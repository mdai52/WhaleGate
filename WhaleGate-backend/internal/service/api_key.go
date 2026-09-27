package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/crypto"
)

// KeyIdentity 是鉴权所需的最小凭证信息，会被序列化进 Redis 与进程内缓存。
type KeyIdentity struct {
	// KeyID 主键，0 表示密钥不存在。
	KeyID uint `json:"kid"`
	// UserID 归属用户。
	UserID uint `json:"uid"`
	// Status 密钥状态：1 正常，2 禁用，0 不存在。
	Status int `json:"st"`
	// UserStatus 用户状态。
	UserStatus int `json:"ust"`
	// UserRole 用户角色。
	UserRole string `json:"role"`
	// QPM 每分钟请求上限，0 表示使用全局默认。
	QPM int `json:"qpm"`
	// Concurrency 并发上限，0 表示使用全局默认。
	Concurrency int `json:"conc"`
	// ExpiresAtUnix 过期时间戳（秒），0 表示不过期。
	ExpiresAtUnix int64 `json:"exp"`
	// Revoked 是否已吊销。
	Revoked bool `json:"rev"`
	// GroupTag 分组标签。
	GroupTag string `json:"grp"`
	// QuotaLimit 密钥级额度上限（点），0=不限。
	QuotaLimit int64 `json:"qlimit"`
	// UsedPoints 累计消耗点数（来自缓存快照，可能略有延迟）。
	UsedPoints int64 `json:"upoints"`
	// AllowedModels 模型白名单，空=不限制。
	AllowedModels []string `json:"models"`
	// IPWhitelist IP 白名单（IP 或 CIDR），空=不限制。
	IPWhitelist []string `json:"ips"`
}

// UsableNow 判断当前是否可用。
func (k *KeyIdentity) UsableNow() bool {
	if k == nil || k.KeyID == 0 || k.Status != constant.StatusEnabled || k.Revoked {
		return false
	}
	if k.UserStatus != constant.StatusEnabled {
		return false
	}
	if k.ExpiresAtUnix > 0 && time.Now().Unix() >= k.ExpiresAtUnix {
		return false
	}
	return true
}

// RejectErrno 返回不可用时对应的业务错误码，可用时返回零值。
func (k *KeyIdentity) RejectErrno() apierr.Errno {
	if k == nil || k.KeyID == 0 {
		return apierr.ErrInvalidCredential
	}
	if k.Revoked || k.Status == constant.StatusDisabled {
		return apierr.ErrKeyRevoked
	}
	if k.ExpiresAtUnix > 0 && time.Now().Unix() >= k.ExpiresAtUnix {
		return apierr.ErrKeyExpired
	}
	if k.UserStatus != constant.StatusEnabled {
		return apierr.ErrUserDisabled
	}
	if k.QuotaExceeded() {
		return apierr.ErrQuotaExceeded
	}
	return apierr.Errno{}
}

// IPAllowed 判断来源 IP 是否在白名单内；白名单为空表示不限制。
// 支持 IPv4/IPv6 精确值与 CIDR 段（如 10.0.0.0/8）。
func (k *KeyIdentity) IPAllowed(ip string) bool {
	list := k.IPWhitelist
	if len(list) == 0 {
		return true
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			if _, cidr, err := net.ParseCIDR(item); err == nil && cidr.Contains(parsed) {
				return true
			}
			continue
		}
		if itemIP := net.ParseIP(item); itemIP != nil && itemIP.Equal(parsed) {
			return true
		}
	}
	return false
}

// ModelAllowed 判断模型是否在白名单内；白名单为空表示不限制。
func (k *KeyIdentity) ModelAllowed(model string) bool {
	if len(k.AllowedModels) == 0 {
		return true
	}
	for _, m := range k.AllowedModels {
		if m == model {
			return true
		}
	}
	return false
}

// QuotaExceeded 判断密钥额度是否已用尽；QuotaLimit 为 0 表示不限制。
func (k *KeyIdentity) QuotaExceeded() bool {
	return k.QuotaLimit > 0 && k.UsedPoints >= k.QuotaLimit
}

// CreateKeyInput 创建密钥的入参。
type CreateKeyInput struct {
	Name string `json:"name"`
	// ExpiresInDays 有效天数，<=0 表示永不过期。
	ExpiresInDays int `json:"expires_in_days"`
	// QPM 每分钟请求上限，<=0 表示继承全局默认。
	QPM int `json:"qpm"`
	// Concurrency 并发上限，<=0 表示继承全局默认。
	Concurrency int `json:"concurrency"`
	// GroupTag 分组标签（选填）。
	GroupTag string `json:"group_tag"`
	// CustomKey 自定义密钥后缀（选填）：完整密钥 = 前缀 + 自定义后缀，需全局唯一。
	CustomKey string `json:"custom_key"`
	// IPWhitelist IP 白名单（IP 或 CIDR），空=不限制。
	IPWhitelist []string `json:"ip_whitelist"`
	// QuotaLimit 密钥级额度上限（点），0=不限。
	QuotaLimit int64 `json:"quota_limit"`
	// AllowedModels 模型白名单，空=不限制。
	AllowedModels []string `json:"allowed_models"`
}

// CreateAPIKey 生成密钥，明文仅在此处返回一次。
func (c *Container) CreateAPIKey(ctx context.Context, userID uint, in CreateKeyInput) (*model.APIKey, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "默认密钥"
	}
	if len(name) > 64 {
		return nil, "", apierr.New(apierr.ErrInvalidParam, "名称长度不能超过 64 字符")
	}

	plain, err := c.newAPIKeyPlain(in.CustomKey)
	if err != nil {
		return nil, "", err
	}

	// 白名单校验：IP 需为合法 IP/CIDR
	for _, item := range in.IPWhitelist {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			if _, _, err := net.ParseCIDR(item); err != nil {
				return nil, "", apierr.New(apierr.ErrInvalidParam, "IP 白名单格式错误: "+item)
			}
			continue
		}
		if net.ParseIP(item) == nil {
			return nil, "", apierr.New(apierr.ErrInvalidParam, "IP 白名单格式错误: "+item)
		}
	}

	key := &model.APIKey{
		UserID:      userID,
		Name:        name,
		KeyHash:     crypto.HashAPIKey(plain),
		Prefix:      c.Config.Security.APIKeyPrefix,
		MaskedKey:   crypto.MaskAPIKey(plain),
		Status:      constant.StatusEnabled,
		QPM:         in.QPM,
		Concurrency: in.Concurrency,
		GroupTag:    strings.TrimSpace(in.GroupTag),
		QuotaLimit:  in.QuotaLimit,
	}
	if err := setJSONList(&key.IPWhitelist, in.IPWhitelist); err != nil {
		return nil, "", apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if err := setJSONList(&key.AllowedModels, in.AllowedModels); err != nil {
		return nil, "", apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if in.ExpiresInDays > 0 {
		exp := time.Now().AddDate(0, 0, in.ExpiresInDays)
		key.ExpiresAt = &exp
	}

	if err := c.DB.WithContext(ctx).Create(key).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, "", apierr.New(apierr.ErrConflict, "密钥重复，请重试")
		}
		return nil, "", apierr.Wrap(apierr.ErrDatabase, err)
	}

	c.warmKeyCache(context.Background(), key)
	return key, plain, nil
}

// ListAPIKeys 分页查询用户的密钥。userID 为 0 时查询全部（管理端）。
// newAPIKeyPlain 生成密钥明文：提供了自定义后缀则拼接前缀使用，否则随机生成。
func (c *Container) newAPIKeyPlain(custom string) (string, error) {
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return crypto.GenerateAPIKey(c.Config.Security.APIKeyPrefix, c.Config.Security.APIKeyRandomBytes)
	}
	if !customKeyPattern.MatchString(custom) {
		return "", apierr.New(apierr.ErrInvalidParam, "自定义密钥仅支持字母、数字、下划线与中划线，长度 8-64")
	}
	plain := c.Config.Security.APIKeyPrefix + custom
	return plain, nil
}

var customKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// setJSONList 序列化字符串列表到 JSON 文本字段；nil/空列表存 "[]"。
func setJSONList(dst *string, list []string) error {
	clean := make([]string, 0, len(list))
	for _, item := range list {
		if item = strings.TrimSpace(item); item != "" {
			clean = append(clean, item)
		}
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	*dst = string(data)
	return nil
}

// UpdateAPIKeyUsage 转发结算时累加密钥使用统计与消耗点数。
func (c *Container) UpdateAPIKeyUsage(ctx context.Context, keyID uint, tokens int, points int64) {
	if keyID == 0 {
		return
	}
	err := c.DB.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", keyID).
		Updates(map[string]interface{}{
			"request_count": gorm.Expr("request_count + 1"),
			"total_tokens":  gorm.Expr("total_tokens + ?", tokens),
			"used_points":   gorm.Expr("used_points + ?", points),
		}).Error
	if err != nil && c.Logger != nil {
		c.Logger.Warn("更新密钥使用统计失败", zap.Uint("key_id", keyID), zap.Error(err))
	}
	// 刷新密钥缓存中的消耗快照，让额度限制尽快生效
	c.RefreshKeyUsage(keyID, tokens, points)
}

func (c *Container) ListAPIKeys(ctx context.Context, userID uint, page, pageSize int) ([]model.APIKey, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.APIKey{})
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var keys []model.APIKey
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&keys).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return keys, total, nil
}

// RevokeAPIKey 吊销密钥（软吊销，保留审计记录）。
func (c *Container) RevokeAPIKey(ctx context.Context, userID uint, id uint) error {
	q := c.DB.WithContext(ctx).Model(&model.APIKey{}).Where("id = ? AND revoked_at IS NULL", id)
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	now := time.Now()
	res := q.Update("revoked_at", now)
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "密钥不存在或已吊销")
	}
	return c.dropKeyCacheByID(ctx, id)
}

// DeleteAPIKey 删除密钥。
func (c *Container) DeleteAPIKey(ctx context.Context, userID uint, id uint) error {
	if err := c.dropKeyCacheByID(ctx, id); err != nil {
		return err
	}
	q := c.DB.WithContext(ctx).Where("id = ?", id)
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	res := q.Delete(&model.APIKey{})
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "密钥不存在")
	}
	return nil
}

// ResolveAPIKey 解析明文密钥：进程内缓存 -> Redis -> 数据库。
func (c *Container) ResolveAPIKey(ctx context.Context, plain string) (*KeyIdentity, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return nil, apierr.New(apierr.ErrUnauthorized, "缺少 API Key")
	}
	hash := crypto.HashAPIKey(plain)

	if c.KeyCache != nil {
		if v, ok := c.KeyCache.Get(hash); ok {
			if idn, ok := v.(*KeyIdentity); ok {
				return idn, nil
			}
		}
	}

	if c.RDB != nil {
		if raw, err := c.RDB.Get(ctx, c.apiKeyCacheKey(hash)).Result(); err == nil && raw != "" {
			idn := &KeyIdentity{}
			if err := json.Unmarshal([]byte(raw), idn); err == nil {
				if c.KeyCache != nil {
					c.KeyCache.SetWithTTL(hash, idn, c.keyCacheTTL(idn))
				}
				return idn, nil
			}
		}
	}

	idn, err := c.loadKeyIdentity(ctx, hash)
	if err != nil {
		return nil, err
	}
	c.warmIdentityCache(ctx, hash, idn)
	return idn, nil
}

type keyRow struct {
	ID            uint
	UserID        uint
	Status        int
	QPM           int
	Concurrency   int
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
	UserStatus    int
	UserRole      string
	GroupTag      string
	IPWhitelist   string
	QuotaLimit    int64
	UsedPoints    int64
	AllowedModels string
}

func (c *Container) loadKeyIdentity(ctx context.Context, hash string) (*KeyIdentity, error) {
	var row keyRow
	err := c.DB.WithContext(ctx).
		Table("api_keys").
		Select("api_keys.id, api_keys.user_id, api_keys.status, api_keys.qpm, api_keys.concurrency, "+
			"api_keys.expires_at, api_keys.revoked_at, api_keys.group_tag, api_keys.ip_whitelist, "+
			"api_keys.quota_limit, api_keys.used_points, api_keys.allowed_models, "+
			"users.status AS user_status, users.role AS user_role").
		Joins("LEFT JOIN users ON users.id = api_keys.user_id").
		Where("api_keys.key_hash = ? AND api_keys.deleted_at IS NULL", hash).
		Scan(&row).Error
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	idn := &KeyIdentity{
		KeyID:       row.ID,
		UserID:      row.UserID,
		Status:      row.Status,
		UserStatus:  row.UserStatus,
		UserRole:    row.UserRole,
		QPM:         row.QPM,
		Concurrency: row.Concurrency,
		Revoked:     row.RevokedAt != nil,
		GroupTag:    row.GroupTag,
		QuotaLimit:  row.QuotaLimit,
		UsedPoints:  row.UsedPoints,
	}
	if row.ExpiresAt != nil {
		idn.ExpiresAtUnix = row.ExpiresAt.Unix()
	}
	_ = json.Unmarshal([]byte(row.IPWhitelist), &idn.IPWhitelist)
	_ = json.Unmarshal([]byte(row.AllowedModels), &idn.AllowedModels)
	return idn, nil
}

// warmIdentityCache 同时写入 Redis 与进程内缓存。
func (c *Container) warmIdentityCache(ctx context.Context, hash string, idn *KeyIdentity) {
	ttl := c.keyCacheTTL(idn)
	if c.RDB != nil {
		raw, err := json.Marshal(idn)
		if err == nil {
			if err := c.RDB.Set(ctx, c.apiKeyCacheKey(hash), raw, ttl).Err(); err != nil && c.Logger != nil {
				c.Logger.Warn("写入 Key 缓存失败", zap.Error(err))
			}
		}
	}
	if c.KeyCache != nil {
		c.KeyCache.SetWithTTL(hash, idn, ttl)
	}
}

// warmKeyCache 创建密钥后预热缓存。
func (c *Container) warmKeyCache(ctx context.Context, key *model.APIKey) {
	idn := &KeyIdentity{
		KeyID:       key.ID,
		UserID:      key.UserID,
		Status:      key.Status,
		UserStatus:  constant.StatusEnabled,
		UserRole:    constant.RoleUser,
		QPM:         key.QPM,
		Concurrency: key.Concurrency,
		Revoked:     key.RevokedAt != nil,
		GroupTag:    key.GroupTag,
		QuotaLimit:  key.QuotaLimit,
		UsedPoints:  key.UsedPoints,
	}
	if key.ExpiresAt != nil {
		idn.ExpiresAtUnix = key.ExpiresAt.Unix()
	}
	_ = json.Unmarshal([]byte(key.IPWhitelist), &idn.IPWhitelist)
	_ = json.Unmarshal([]byte(key.AllowedModels), &idn.AllowedModels)
	c.warmIdentityCache(ctx, key.KeyHash, idn)
}

// RefreshKeyUsage 结算后同步缓存中的消耗快照，让密钥额度限制尽快生效。
func (c *Container) RefreshKeyUsage(keyID uint, tokens int, points int64) {
	if keyID == 0 || (points == 0 && tokens == 0) {
		return
	}
	ctx := context.Background()
	var hash string
	if err := c.DB.WithContext(ctx).Model(&model.APIKey{}).
		Where("id = ?", keyID).Pluck("key_hash", &hash).Error; err != nil || hash == "" {
		return
	}

	if c.KeyCache != nil {
		if v, ok := c.KeyCache.Get(hash); ok {
			if idn, ok := v.(*KeyIdentity); ok && idn != nil {
				idn.UsedPoints += points
			}
		}
	}
	if c.RDB != nil {
		if raw, err := c.RDB.Get(ctx, c.apiKeyCacheKey(hash)).Bytes(); err == nil {
			var idn KeyIdentity
			if json.Unmarshal(raw, &idn) == nil {
				idn.UsedPoints += points
				if data, err := json.Marshal(&idn); err == nil {
					_ = c.RDB.Set(ctx, c.apiKeyCacheKey(hash), data, c.keyCacheTTL(&idn)).Err()
				}
			}
		}
	}
}

// dropKeyCacheByID 按主键清除缓存。
func (c *Container) dropKeyCacheByID(ctx context.Context, id uint) error {
	var hash string
	if err := c.DB.WithContext(ctx).Model(&model.APIKey{}).
		Where("id = ?", id).Pluck("key_hash", &hash).Error; err != nil {
		if err == gorm.ErrRecordNotFound || hash == "" {
			return nil
		}
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	if c.RDB != nil {
		_ = c.RDB.Del(ctx, c.apiKeyCacheKey(hash)).Err()
	}
	if c.KeyCache != nil {
		c.KeyCache.Delete(hash)
	}
	return nil
}

// MarkKeyUsed 记录最近使用时间，按分钟节流避免高频写库。
func (c *Container) MarkKeyUsed(keyID uint) {
	if keyID == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		throttle := fmt.Sprintf("%s:akused:%d", c.prefix(), keyID)
		ok, err := c.RDB.SetNX(ctx, throttle, 1, time.Minute).Result()
		if err != nil || !ok {
			return
		}
		if err := c.DB.WithContext(ctx).Model(&model.APIKey{}).
			Where("id = ?", keyID).Update("last_used_at", time.Now()).Error; err != nil && c.Logger != nil {
			c.Logger.Warn("更新密钥最近使用时间失败", zap.Uint("key_id", keyID), zap.Error(err))
		}
	}()
}

func (c *Container) apiKeyCacheKey(hash string) string {
	return fmt.Sprintf("%s:ak:%s", c.prefix(), hash)
}

// keyCacheTTL 缓存时长：不超过过期时间，且限制在 [1 分钟, 24 小时]。
func (c *Container) keyCacheTTL(idn *KeyIdentity) time.Duration {
	const maxTTL = 24 * time.Hour
	ttl := maxTTL
	if idn != nil && idn.ExpiresAtUnix > 0 {
		if d := time.Until(time.Unix(idn.ExpiresAtUnix, 0)); d < ttl {
			ttl = d
		}
	}
	if ttl < time.Minute {
		ttl = time.Minute
	}
	if idn == nil || idn.KeyID == 0 {
		// 不存在的密钥只做短时负缓存，防止无效 Key 打穿数据库。
		ttl = 30 * time.Second
	}
	return ttl
}
