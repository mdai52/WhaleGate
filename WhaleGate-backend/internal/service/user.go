package service

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/crypto"
	whalegatejwt "github.com/whalegate/whalegate/internal/pkg/jwt"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,64}$`)

// ErrRegistrationDisabled 关闭开放注册时返回。
var ErrRegistrationDisabled = apierr.New(apierr.ErrForbidden, "当前未开放注册，请联系管理员开通账号")

// RegisterInput 注册入参。
type RegisterInput struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
	// Role 仅管理员可指定，普通注册强制为 user。
	Role string `json:"role"`
}

// LoginInput 登录入参。
type LoginInput struct {
	// Account 用户名或邮箱。
	Account  string `json:"account" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResult 登录结果。
type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresIn int64       `json:"expires_in"`
	User      *model.User `json:"user"`
}

// Register 创建用户。allowAdmin 为 true 时允许指定角色（管理端创建）。
func (c *Container) Register(ctx context.Context, in RegisterInput, allowAdmin bool) (*model.User, error) {
	return c.registerOn(ctx, c.DB, in, allowAdmin)
}

// registerOn 在指定连接（可以是事务）上创建用户。
//
// 首次安装需要在同一个事务里先 COUNT 再 INSERT，否则并发请求会各自看到
// "还没有用户"并分别创建一个管理员，因此把落库动作拆出来供事务复用。
func (c *Container) registerOn(
	ctx context.Context,
	db *gorm.DB,
	in RegisterInput,
	allowAdmin bool,
) (*model.User, error) {
	username := strings.TrimSpace(in.Username)
	if !usernamePattern.MatchString(username) {
		return nil, apierr.New(apierr.ErrInvalidParam, "用户名需为 3-64 位字母、数字、下划线、点或连字符")
	}
	if err := crypto.ValidatePassword(in.Password); err != nil {
		return nil, apierr.New(apierr.ErrInvalidParam, err.Error())
	}

	role := constant.RoleUser
	if allowAdmin && in.Role != "" {
		if in.Role != constant.RoleAdmin && in.Role != constant.RoleUser {
			return nil, apierr.New(apierr.ErrInvalidParam, "角色取值非法")
		}
		role = in.Role
	}
	// 普通注册永远不会获得管理员角色：管理员由启动时的初始化流程创建，
	// 其随机密码只打印到日志一次，避免抢注提权。
	_ = allowAdmin

	hash, err := bcryptHash(in.Password)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	user := &model.User{
		Username:     username,
		Email:        strings.TrimSpace(in.Email),
		Nickname:     strings.TrimSpace(in.Nickname),
		PasswordHash: string(hash),
		Role:         role,
		Status:       constant.StatusEnabled,
	}

	err = db.WithContext(ctx).Create(user).Error
	if err != nil {
		if isUniqueViolation(err) {
			// 直接给出可读提示（detail 只进日志，客户端只看到 message）
			return nil, apierr.New(apierr.Define(apierr.CodeConflict, "用户名或邮箱已被占用"), "")
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return user, nil
}

// Login 校验凭证并签发令牌；连续失败达到阈值会临时锁定账号（防爆破）。
func (c *Container) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	account := strings.TrimSpace(in.Account)
	if account == "" || in.Password == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "账号与密码不能为空")
	}

	// 登录前置：账号是否被临时锁定
	if err := c.LoginGuard(ctx, account); err != nil {
		return nil, err
	}

	var user model.User
	err := c.DB.WithContext(ctx).
		Where("username = ? OR (email <> '' AND email = ?)", account, account).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.RecordLoginFailure(ctx, account)
			return nil, apierr.New(apierr.ErrInvalidCredential, "账号或密码错误")
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	if err := comparePassword(user.PasswordHash, in.Password); err != nil {
		c.RecordLoginFailure(ctx, account)
		return nil, apierr.New(apierr.ErrInvalidCredential, "账号或密码错误")
	}
	if !user.IsEnabled() {
		return nil, apierr.New(apierr.ErrUserDisabled, "账号已被禁用，请联系管理员")
	}
	// 登录成功：清除失败计数
	c.ClearLoginFailure(ctx, account)

	token, err := c.JWT.Generate(jwtClaimsFor(&user))
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	now := time.Now()
	go func(id uint) {
		updateCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := c.DB.WithContext(updateCtx).Model(&model.User{}).
			Where("id = ?", id).
			Updates(map[string]interface{}{"last_login_at": now}).Error; err != nil && c.Logger != nil {
			c.Logger.Warn("更新最近登录时间失败", zap.Uint("user_id", id), zap.Error(err))
		}
	}(user.ID)

	return &LoginResult{
		Token:     token,
		ExpiresIn: int64(c.JWT.TTL().Seconds()),
		User:      &user,
	}, nil
}

// userStateCacheTTL 用户状态缓存时长。
const userStateCacheTTL = 30 * time.Second

// userSessionState 用于鉴权的用户快照。
type userSessionState struct {
	enabled bool
	role    string
}

func userStateKey(userID uint) string {
	return "user:" + strconv.FormatUint(uint64(userID), 10)
}

// UserSessionState 回查用户当前是否启用与当前角色。
//
// 令牌里带的是签发时刻的快照：账号被禁用或角色被调整后，旧令牌在过期前仍然有效。
// 因此每个请求都要以数据库为准核对一次，用短缓存把库查询摊薄。
func (c *Container) UserSessionState(ctx context.Context, userID uint) (enabled bool, role string, ok bool) {
	if userID == 0 {
		return false, "", false
	}
	key := userStateKey(userID)
	if c.UserCache != nil {
		if v, hit := c.UserCache.Get(key); hit {
			if st, is := v.(userSessionState); is {
				return st.enabled, st.role, true
			}
		}
	}
	var u model.User
	if err := c.DB.WithContext(ctx).Select("status", "role").First(&u, userID).Error; err != nil {
		return false, "", false
	}
	st := userSessionState{enabled: u.Status == constant.StatusEnabled, role: u.Role}
	if c.UserCache != nil {
		c.UserCache.SetWithTTL(key, st, userStateCacheTTL)
	}
	return st.enabled, st.role, true
}

// InvalidateUserCache 让用户状态缓存立即失效（启停账号、改角色后调用）。
func (c *Container) InvalidateUserCache(userID uint) {
	if c.UserCache != nil {
		c.UserCache.Delete(userStateKey(userID))
	}
}

// RevokeUserTokens 让用户当前持有的全部令牌立即失效。
//
// JWT 本身无状态，无法撤回已签发的令牌，因此记录一个"失效时刻"：
// 签发时间早于该时刻的令牌一律拒绝（见 middleware.JWTAuth）。
// 登出、改密、禁用账号时调用，使被盗的旧令牌不再可用。
func (c *Container) RevokeUserTokens(ctx context.Context, userID uint) {
	if userID == 0 || c.RDB == nil {
		return
	}
	ttl := c.JWT.TTL()
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	// 令牌的 IssuedAt 只精确到秒，这里同样截断到秒：
	// 否则改密后立刻签发的新令牌会因"签发早于失效时刻"而把自己也判失效。
	revokedAt := time.Now().Truncate(time.Second)
	if err := c.RDB.Set(ctx, c.userRevokeKey(userID), revokedAt.UnixNano(), ttl).Err(); err != nil {
		if c.Logger != nil {
			c.Logger.Warn("记录令牌失效时刻失败", zap.Uint("user_id", userID), zap.Error(err))
		}
	}
	c.InvalidateUserCache(userID)
}

// TokensRevokedAt 返回令牌失效时刻；未失效时返回零值与 false。
func (c *Container) TokensRevokedAt(ctx context.Context, userID uint) (time.Time, bool) {
	if userID == 0 || c.RDB == nil {
		return time.Time{}, false
	}
	raw, err := c.RDB.Get(ctx, c.userRevokeKey(userID)).Result()
	if err != nil || raw == "" {
		return time.Time{}, false
	}
	nanos, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, nanos), true
}

func (c *Container) userRevokeKey(userID uint) string {
	return c.prefix() + ":user:revoke:" + strconv.FormatUint(uint64(userID), 10)
}

// GetUserByID 按主键查询用户。
func (c *Container) GetUserByID(ctx context.Context, id uint) (*model.User, error) {
	var user model.User
	if err := c.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	return &user, nil
}

// UpdateProfileInput 用户自助修改资料入参。
type UpdateProfileInput struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
}

// UpdateProfile 当前用户修改自己的资料。
func (c *Container) UpdateProfile(ctx context.Context, id uint, in UpdateProfileInput) (*model.User, error) {
	username := strings.TrimSpace(in.Username)
	if !usernamePattern.MatchString(username) {
		return nil, apierr.New(apierr.ErrInvalidParam, "用户名需为 3-64 位字母、数字、下划线、点或连字符")
	}
	email := strings.TrimSpace(in.Email)
	nickname := strings.TrimSpace(in.Nickname)

	var user model.User
	if err := c.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}

	updates := map[string]interface{}{
		"username": username,
		"email":    email,
		"nickname": nickname,
	}
	if err := c.DB.WithContext(ctx).Model(&user).Updates(updates).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, apierr.New(apierr.Define(apierr.CodeConflict, "用户名或邮箱已被占用"), "")
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	user.Username = username
	user.Email = email
	user.Nickname = nickname
	return &user, nil
}

// ListUsers 分页查询用户。
func (c *Container) ListUsers(ctx context.Context, keyword string, page, pageSize int) ([]model.User, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.User{})
	if keyword != "" {
		like := "%" + strings.TrimSpace(keyword) + "%"
		q = q.Where("username ILIKE ? OR email ILIKE ? OR nickname ILIKE ?", like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var users []model.User
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return users, total, nil
}

// SetUserStatus 启用/禁用用户，禁用后同步失效其全部 Key 缓存。
func (c *Container) SetUserStatus(ctx context.Context, id uint, status int) error {
	if status != constant.StatusEnabled && status != constant.StatusDisabled {
		return apierr.New(apierr.ErrInvalidParam, "状态取值非法")
	}
	res := c.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "用户不存在")
	}
	// 令牌里带的是签发时刻的快照：不立即失效的话，
	// 被停用的账号（尤其是管理员）在令牌过期前仍能继续操作。
	c.InvalidateUserCache(id)
	if status == constant.StatusDisabled {
		c.RevokeUserTokens(ctx, id)
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.invalidateUserKeys(bg, id); err != nil && c.Logger != nil {
			c.Logger.Warn("失效用户 Key 缓存失败", zap.Uint("user_id", id), zap.Error(err))
		}
	}()
	return nil
}

// invalidateUserKeys 删除某用户在 Redis 与进程内的 Key 缓存。
func (c *Container) invalidateUserKeys(ctx context.Context, userID uint) error {
	var hashes []string
	if err := c.DB.WithContext(ctx).Model(&model.APIKey{}).
		Where("user_id = ?", userID).Pluck("key_hash", &hashes).Error; err != nil {
		return err
	}
	for _, h := range hashes {
		_ = c.RDB.Del(ctx, c.apiKeyCacheKey(h)).Err()
		if c.KeyCache != nil {
			c.KeyCache.Delete(h)
		}
	}
	return nil
}

func jwtClaimsFor(u *model.User) whalegatejwt.Claims {
	return whalegatejwt.Claims{
		UserID:     u.ID,
		Username:   u.Username,
		Role:       u.Role,
		MustChange: u.MustChangePassword,
	}
}

// bcryptHash 生成密码摘要；明文密码永不落库、永不写日志。
func bcryptHash(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

// comparePassword 校验密码。
func comparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// gormLock 行级排他锁，用于额度变更等并发敏感操作。
func gormLock() clause.Locking {
	return clause.Locking{Strength: "UPDATE"}
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = constant.DefaultPage
	}
	if pageSize < 1 {
		pageSize = constant.DefaultPageSize
	}
	if pageSize > constant.MaxPageSize {
		pageSize = constant.MaxPageSize
	}
	return page, pageSize
}

// isUniqueViolation 判断是否为唯一键冲突。
// 注意：GORM 开启了 TranslateError，PostgreSQL 的 23505 会被翻译成
// gorm.ErrDuplicatedKey（"duplicated key not allowed"），不能只匹配原始文案。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	msg := strings.ToLower(err.Error())
	for _, hint := range []string{
		"duplicate key",
		"duplicated key",
		"23505",
		"unique constraint",
		"unique violation",
	} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}
