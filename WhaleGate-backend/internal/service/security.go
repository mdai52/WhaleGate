package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

// ---------------------------------------------------------------- 脱敏

// MaskEmail 邮箱脱敏：保留首字符与域名，中间以 *** 代替。
// 例：alice@example.com -> a***@example.com
func MaskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	at := strings.LastIndexByte(email, '@')
	if at <= 0 {
		// 非邮箱格式：仅保留首字符
		if len(email) <= 2 {
			return strings.Repeat("*", len(email))
		}
		return email[:1] + "***"
	}
	local, domain := email[:at], email[at+1:]
	if len(local) <= 1 {
		return local + "***@" + domain
	}
	return local[:1] + "***@" + domain
}

// MaskSecret 通用密钥脱敏：仅保留末 4 位。
func MaskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	const visible = 4
	if len(secret) <= visible {
		return strings.Repeat("*", len(secret))
	}
	return strings.Repeat("*", len(secret)-visible) + secret[len(secret)-visible:]
}

// ---------------------------------------------------------------- 登录防爆破

// loginKey 账号维度的 Redis 键（账号名哈希，避免特殊字符与隐私泄露）。
func (c *Container) loginKey(account string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	return fmt.Sprintf("%s:loginfail:%s", c.prefix(), hex.EncodeToString(sum[:]))
}

// LoginLocked 返回账号是否处于锁定状态以及剩余秒数。
func (c *Container) LoginLocked(ctx context.Context, account string) (bool, int64, error) {
	ttl, err := c.RDB.TTL(ctx, c.loginKey(account)).Result()
	if err != nil {
		if err == redis.Nil {
			return false, 0, nil
		}
		return false, 0, nil // 缓存异常时不阻断登录
	}
	if ttl <= 0 {
		return false, 0, nil
	}
	count, err := c.RDB.Get(ctx, c.loginKey(account)).Int64()
	if err != nil || count == 0 {
		return false, 0, nil
	}

	maxAttempts := c.Config.Security.LoginMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if count < int64(maxAttempts) {
		return false, 0, nil
	}
	return true, int64(ttl.Seconds()), nil
}

// RecordLoginFailure 记录一次登录失败，达到阈值后延长键 TTL 实现临时锁定。
func (c *Container) RecordLoginFailure(ctx context.Context, account string) {
	lockSeconds := c.Config.Security.LoginLockSeconds
	if lockSeconds <= 0 {
		lockSeconds = 900
	}
	key := c.loginKey(account)
	pipe := c.RDB.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, time.Duration(lockSeconds)*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return
	}
	// 已达到阈值：把 TTL 固定为锁定时长，避免滚动过期导致锁定失效
	maxAttempts := c.Config.Security.LoginMaxAttempts
	if maxAttempts > 0 && incr.Val() >= int64(maxAttempts) {
		_ = c.RDB.Expire(ctx, key, time.Duration(lockSeconds)*time.Second).Err()
	}
}

// ClearLoginFailure 登录成功后清除失败计数。
func (c *Container) ClearLoginFailure(ctx context.Context, account string) {
	_ = c.RDB.Del(ctx, c.loginKey(account)).Err()
}

// LoginGuard 登录前置校验：账号被锁定时直接拒绝。
func (c *Container) LoginGuard(ctx context.Context, account string) error {
	locked, seconds, err := c.LoginLocked(ctx, account)
	if err != nil || !locked {
		return nil
	}
	minutes := seconds / 60
	if minutes < 1 {
		minutes = 1
	}
	maxAttempts := c.Config.Security.LoginMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	// 直接给出可读提示（detail 只进日志，客户端只看 message）
	return apierr.New(apierr.Define(apierr.CodeRateLimited,
		fmt.Sprintf("连续登录失败 %d 次，账号已临时锁定，请约 %d 分钟后重试", maxAttempts, minutes)), "")
}
