// Package service 承载业务逻辑，隔离 HTTP 层与数据访问层。
package service

import (
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/redis/go-redis/v9"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/pkg/cache"
	"github.com/whalegate/whalegate/internal/pkg/jwt"
	"github.com/whalegate/whalegate/internal/pkg/oauth"
	"github.com/whalegate/whalegate/internal/pkg/ratelimit"
)

// scripts 预编译的 Redis Lua 脚本。
type scripts struct {
	reserve *redis.Script
	settle  *redis.Script
}

// Container 聚合所有服务共享的依赖。
type Container struct {
	DB       *gorm.DB
	RDB      *redis.Client
	Config   *config.Config
	Logger   *zap.Logger
	JWT      *jwt.Manager
	Limiter  *ratelimit.Limiter
	KeyCache *cache.TTLCache
	Logs     *CallLogWriter
	Audits   *AuditWriter
	OAuth    *oauth.Sessions
	// WebAuthn 通行密钥（未启用时为 nil）。
	WebAuthn *webauthn.WebAuthn
	scripts  *scripts
}

// New 创建服务容器。
func New(cfg *config.Config, db *gorm.DB, rdb *redis.Client, lg *zap.Logger) *Container {
	c := &Container{
		DB:       db,
		RDB:      rdb,
		Config:   cfg,
		Logger:   lg,
		JWT:      jwt.New(cfg.Security.JWTSecret, cfg.Security.JWTIssuer, cfg.Security.JWTTTL),
		Limiter:  ratelimit.New(rdb, cfg.Redis.KeyPrefix),
		KeyCache: cache.NewTTLCache(cfg.Security.JWTTTL, 10000),
		OAuth:    oauth.NewSessions(nil),
		scripts: &scripts{
			reserve: redis.NewScript(reserveScript),
			settle:  redis.NewScript(settleScript),
		},
	}
	c.Logs = NewCallLogWriter(db, lg, 4096)
	c.Audits = NewAuditWriter(db, lg, 2048)
	c.WebAuthn = buildWebAuthn(cfg, lg)
	return c
}

// buildWebAuthn 按配置构造 WebAuthn 实例，失败时仅告警（不影响其它功能）。
func buildWebAuthn(cfg *config.Config, lg *zap.Logger) *webauthn.WebAuthn {
	if cfg == nil || !cfg.Auth.WebAuthn.Enabled {
		return nil
	}
	origins := cfg.Auth.WebAuthn.RPOrigins
	if len(origins) == 0 {
		origins = []string{cfg.Auth.FrontendBase}
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.Auth.WebAuthn.RPID,
		RPDisplayName: cfg.Auth.WebAuthn.RPDisplayName,
		RPOrigins:     origins,
	})
	if err != nil {
		if lg != nil {
			lg.Warn("通行密钥初始化失败", zap.Error(err))
		}
		return nil
	}
	return wa
}

// Close 释放容器持有的后台资源（日志与审计异步写入器等）。
func (c *Container) Close() error {
	if c.Logs != nil {
		c.Logs.Close()
	}
	if c.Audits != nil {
		c.Audits.Close()
	}
	return nil
}

// prefix 返回 Redis 键前缀。
func (c *Container) prefix() string {
	if c.Config == nil || c.Config.Redis.KeyPrefix == "" {
		return "wg"
	}
	return c.Config.Redis.KeyPrefix
}
