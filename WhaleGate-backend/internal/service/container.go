// Package service 承载业务逻辑，隔离 HTTP 层与数据访问层。
package service

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/redis/go-redis/v9"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/model"
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
	// tlsProvider 当前 TLS 证书与启用状态的持有者，支持运行时热替换证书。
	tlsProvider *tlsCertProvider
	// restartCh 当监听模式（HTTP<->HTTPS）需要变更时触发进程自重启。
	restartCh chan struct{}
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
	c.WebAuthn = c.buildWebAuthn()
	c.tlsProvider = newTLSCertProvider()
	c.restartCh = make(chan struct{}, 1)
	return c
}

// passkeyConfig 返回当前生效的通行密钥配置：数据库设置优先，未设置则回退到配置文件。
func (c *Container) passkeyConfig() (enabled bool, rpID, rpDisplayName string, origins []string) {
	cfg := c.Config
	if cfg == nil {
		return false, "", "鲸闸 WhaleGate", nil
	}
	enabled = cfg.Auth.WebAuthn.Enabled
	rpID = cfg.Auth.WebAuthn.RPID
	rpDisplayName = cfg.Auth.WebAuthn.RPDisplayName
	origins = cfg.Auth.WebAuthn.RPOrigins
	if len(origins) == 0 {
		origins = []string{cfg.Auth.FrontendBase}
	}

	// 数据库设置覆盖配置文件；若数据库未设置则保留配置文件值。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	settings, _ := c.GetSettings(ctx)
	if settings == nil {
		return
	}
	if _, ok := c.getRawSetting(ctx, model.SettingPasskeyEnabled); ok {
		enabled = settings.PasskeyEnabled
	}
	if settings.PasskeyRPID != "" {
		rpID = settings.PasskeyRPID
	}
	if settings.PasskeyRPOrigins != "" {
		origins = parseOrigins(settings.PasskeyRPOrigins, cfg.Auth.FrontendBase)
	}
	return
}

func parseOrigins(raw, fallback string) []string {
	if raw == "" {
		if fallback == "" {
			return nil
		}
		return []string{fallback}
	}
	var out []string
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			out = append(out, o)
		}
	}
	if len(out) == 0 && fallback != "" {
		out = append(out, fallback)
	}
	return out
}

// buildWebAuthn 按当前生效配置构造 WebAuthn 实例，失败时仅告警（不影响其它功能）。
func (c *Container) buildWebAuthn() *webauthn.WebAuthn {
	enabled, rpID, rpDisplayName, origins := c.passkeyConfig()
	if !enabled {
		return nil
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: rpDisplayName,
		RPOrigins:     origins,
	})
	if err != nil {
		if c.Logger != nil {
			c.Logger.Warn("通行密钥初始化失败", zap.Error(err))
		}
		return nil
	}
	return wa
}

// rebuildWebAuthn 在设置变更后重新构造 WebAuthn 实例。
func (c *Container) rebuildWebAuthn(ctx context.Context) {
	wa := c.buildWebAuthn()
	c.WebAuthn = wa
	if c.Logger != nil {
		if wa == nil {
			c.Logger.Info("通行密钥已禁用或初始化失败")
		} else {
			c.Logger.Info("通行密钥配置已更新并生效")
		}
	}
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
