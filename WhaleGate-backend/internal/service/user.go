package service

import (
	"context"
	"errors"
	"regexp"
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

	err = c.DB.WithContext(ctx).Create(user).Error
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

// GetUserByID 按主键查询用户。
func (c *Container) GetUserByID(ctx context.Context, id uint) (*model.User, error) {
	var user model.User
	if err := c.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
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
