// Package config 负责加载鲸闸运行所需的全部配置。
//
// 加载优先级：命令行指定文件 > ./configs/config.yaml > 环境变量 WG_* > 内置默认值。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 是配置的根节点。
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Log      LogConfig      `mapstructure:"log"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Security SecurityConfig `mapstructure:"security"`
	Gateway  GatewayConfig  `mapstructure:"gateway"`
	CORS     CORSConfig     `mapstructure:"cors"`
	Web      WebConfig      `mapstructure:"web"`
	OAuth    OAuthConfig    `mapstructure:"oauth"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Skills   SkillsConfig   `mapstructure:"skills"`
}

// SkillsConfig 技能目录配置。
type SkillsConfig struct {
	// Dir SKILL.md 扫描根目录，为空表示不启用目录扫描。
	Dir string `mapstructure:"dir"`
}

// GitHubAuthConfig GitHub 第三方登录/绑定配置。
type GitHubAuthConfig struct {
	// Enabled 是否启用。
	Enabled bool `mapstructure:"enabled"`
	// ClientID OAuth App 的 Client ID。
	ClientID string `mapstructure:"client_id"`
	// ClientSecret OAuth App 的 Client Secret。
	ClientSecret string `mapstructure:"client_secret"`
	// Scopes 授权范围，默认 read:user user:email。
	Scopes []string `mapstructure:"scopes"`
}

// WebAuthnConfig 通行密钥（WebAuthn）配置。
type WebAuthnConfig struct {
	// Enabled 是否启用。
	Enabled bool `mapstructure:"enabled"`
	// RPID 依赖方 ID，通常是域名（不含协议与端口），如 localhost。
	RPID string `mapstructure:"rp_id"`
	// RPDisplayName 展示名。
	RPDisplayName string `mapstructure:"rp_display_name"`
	// RPOrigins 允许的来源，需完整带协议与端口，如 http://localhost:5173。
	RPOrigins []string `mapstructure:"rp_origins"`
}

// AuthConfig 登录与账号绑定相关配置。
type AuthConfig struct {
	// FrontendBase 前端地址，用于第三方登录回调跳转。
	FrontendBase string           `mapstructure:"frontend_base"`
	GitHub       GitHubAuthConfig `mapstructure:"github"`
	WebAuthn     WebAuthnConfig   `mapstructure:"webauthn"`
}

// OAuthProviderConfig 配置文件中声明的 OAuth 供应商。
type OAuthProviderConfig struct {
	// ID 唯一标识，如 kimi-cn / muse / anthropic。
	ID string `mapstructure:"id"`
	// Name 展示名。
	Name string `mapstructure:"name"`
	// Description 描述。
	Description string `mapstructure:"description"`
	// Flow 授权流程：authorization_code（默认）或 device_code。
	Flow string `mapstructure:"flow"`
	// AuthURL 授权页地址。
	AuthURL string `mapstructure:"auth_url"`
	// TokenURL 令牌端点。
	TokenURL string `mapstructure:"token_url"`
	// DeviceAuthURL 设备码申请端点。
	DeviceAuthURL string `mapstructure:"device_auth_url"`
	// ClientID 客户端 ID。
	ClientID string `mapstructure:"client_id"`
	// Scopes 授权范围。
	Scopes []string `mapstructure:"scopes"`
	// TokenBodyFormat 令牌端点请求体格式：form（默认）或 json。
	TokenBodyFormat string `mapstructure:"token_body_format"`
}

// OAuthConfig OAuth 凭证相关配置。
type OAuthConfig struct {
	// Providers 自定义供应商（可覆盖内置定义）。
	Providers []OAuthProviderConfig `mapstructure:"providers"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Mode            string        `mapstructure:"mode"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	BodyLimit       string        `mapstructure:"body_limit"`
}

// LogFileConfig 日志文件滚动配置。
type LogFileConfig struct {
	Path       string `mapstructure:"path"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAge     int    `mapstructure:"max_age"`
	Compress   bool   `mapstructure:"compress"`
}

// LogConfig 日志配置。
type LogConfig struct {
	Level      string        `mapstructure:"level"`
	Format     string        `mapstructure:"format"`
	Output     string        `mapstructure:"output"`
	Caller     bool          `mapstructure:"caller"`
	Stacktrace string        `mapstructure:"stacktrace"`
	File       LogFileConfig `mapstructure:"file"`
}

// DatabaseConfig PostgreSQL 配置。
type DatabaseConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	DBName          string        `mapstructure:"dbname"`
	SSLMode         string        `mapstructure:"sslmode"`
	TimeZone        string        `mapstructure:"timezone"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	SlowThreshold   time.Duration `mapstructure:"slow_threshold"`
	AutoMigrate     bool          `mapstructure:"auto_migrate"`
	MigrationDir    string        `mapstructure:"migration_dir"`
}

// RedisConfig Redis 配置。
type RedisConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	KeyPrefix    string        `mapstructure:"key_prefix"`
}

// SecurityConfig 安全相关配置。
type SecurityConfig struct {
	JWTSecret         string        `mapstructure:"jwt_secret"`
	JWTTTL            time.Duration `mapstructure:"jwt_ttl"`
	JWTIssuer         string        `mapstructure:"jwt_issuer"`
	APIKeyPrefix      string        `mapstructure:"api_key_prefix"`
	APIKeyRandomBytes int           `mapstructure:"api_key_random_bytes"`
	EncryptionKey     string        `mapstructure:"encryption_key"`
	AllowRegistration bool          `mapstructure:"allow_registration"`
	// SecureHeaders 是否下发安全响应头（CSP、X-Frame-Options 等）。
	SecureHeaders bool `mapstructure:"secure_headers"`
	// CSP 自定义 Content-Security-Policy，为空使用内置策略。
	CSP string `mapstructure:"csp"`
	// FrameOptions X-Frame-Options 取值，默认 DENY。
	FrameOptions string `mapstructure:"frame_options"`
	// HSTSMaxAge Strict-Transport-Security 的 max-age 秒数，0 表示不下发。
	HSTSMaxAge int `mapstructure:"hsts_max_age"`
	// LoginMaxAttempts 登录失败达到该次数后临时锁定账号。
	LoginMaxAttempts int `mapstructure:"login_max_attempts"`
	// LoginLockSeconds 登录锁定时长（秒）。
	LoginLockSeconds int `mapstructure:"login_lock_seconds"`
	// AuthRateLimit 认证接口按 IP 的每分钟请求上限。
	AuthRateLimit int `mapstructure:"auth_rate_limit"`
	// BootstrapAdmin 首次安装（users 表为空）时自动创建管理员账号。
	BootstrapAdmin bool `mapstructure:"bootstrap_admin"`
	// BootstrapPasswordLength 初始化管理员随机密码长度。
	BootstrapPasswordLength int `mapstructure:"bootstrap_password_length"`
}

// GatewayConfig 网关转发与计费默认配置。
type GatewayConfig struct {
	UpstreamTimeout      time.Duration `mapstructure:"upstream_timeout"`
	MaxRetries           int           `mapstructure:"max_retries"`
	RetryBackoff         time.Duration `mapstructure:"retry_backoff"`
	AutoDisableThreshold int           `mapstructure:"auto_disable_threshold"`
	AutoDisableWindow    time.Duration `mapstructure:"auto_disable_window"`
	DefaultQPM           int           `mapstructure:"default_qpm"`
	DefaultConcurrency   int           `mapstructure:"default_concurrency"`
	// DefaultPromptRatio 兜底倍率：点 / 1K prompt tokens。
	DefaultPromptRatio float64 `mapstructure:"default_prompt_ratio"`
	// DefaultCompletionRatio 兜底倍率：点 / 1K completion tokens（含思维链）。
	DefaultCompletionRatio float64 `mapstructure:"default_completion_ratio"`
	// EstimateCompletionTokens 上游未返回 usage 时，单次请求按该值估算输出 token。
	EstimateCompletionTokens int `mapstructure:"estimate_completion_tokens"`
	// QuotaFlushInterval 额度增量落库周期。
	QuotaFlushInterval time.Duration `mapstructure:"quota_flush_interval"`
	MinBalance         int64         `mapstructure:"min_balance"`
}

// CORSConfig 跨域配置。
type CORSConfig struct {
	Enable       bool          `mapstructure:"enable"`
	AllowOrigins []string      `mapstructure:"allow_origins"`
	AllowMethods []string      `mapstructure:"allow_methods"`
	AllowHeaders []string      `mapstructure:"allow_headers"`
	MaxAge       time.Duration `mapstructure:"max_age"`
}

// WebConfig 前端静态资源托管配置。
type WebConfig struct {
	Enable  bool   `mapstructure:"enable"`
	DistDir string `mapstructure:"dist_dir"`
}

// Load 读取配置。path 为空时按默认搜索路径查找。
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath("./configs")
		v.AddConfigPath("../configs")
		v.AddConfigPath("/etc/whalegate")
	}

	v.SetEnvPrefix("WG")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if path != "" || !errors.As(err, &notFound) {
			return nil, fmt.Errorf("读取配置失败: %w", err)
		}
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "600s")
	v.SetDefault("server.idle_timeout", "120s")
	v.SetDefault("server.shutdown_timeout", "15s")
	v.SetDefault("server.body_limit", "32MB")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.output", "stdout")
	v.SetDefault("log.caller", true)
	v.SetDefault("log.stacktrace", "error")
	v.SetDefault("log.file.path", "./logs/whalegate.log")
	v.SetDefault("log.file.max_size", 100)
	v.SetDefault("log.file.max_backups", 7)
	v.SetDefault("log.file.max_age", 30)
	v.SetDefault("log.file.compress", true)

	v.SetDefault("database.host", "127.0.0.1")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.sslmode", "disable")
	v.SetDefault("database.timezone", "Asia/Shanghai")
	v.SetDefault("database.max_open_conns", 50)
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.conn_max_lifetime", "1h")
	v.SetDefault("database.slow_threshold", "500ms")
	v.SetDefault("database.auto_migrate", true)
	v.SetDefault("database.migration_dir", "./migrations")

	v.SetDefault("redis.host", "127.0.0.1")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.pool_size", 100)
	v.SetDefault("redis.min_idle_conns", 10)
	v.SetDefault("redis.dial_timeout", "5s")
	v.SetDefault("redis.read_timeout", "3s")
	v.SetDefault("redis.write_timeout", "3s")
	v.SetDefault("redis.key_prefix", "wg")

	v.SetDefault("security.jwt_ttl", "24h")
	v.SetDefault("security.jwt_issuer", "whalegate")
	v.SetDefault("security.api_key_prefix", "sk-")
	v.SetDefault("security.api_key_random_bytes", 32)
	v.SetDefault("skills.dir", "skills")
	v.SetDefault("security.secure_headers", true)
	v.SetDefault("security.csp", "")
	v.SetDefault("security.frame_options", "DENY")
	v.SetDefault("security.hsts_max_age", 0)
	v.SetDefault("security.login_max_attempts", 5)
	v.SetDefault("security.login_lock_seconds", 900)
	v.SetDefault("security.auth_rate_limit", 20)
	v.SetDefault("security.bootstrap_admin", true)
	v.SetDefault("security.bootstrap_password_length", 24)

	v.SetDefault("gateway.upstream_timeout", "300s")
	v.SetDefault("gateway.max_retries", 2)
	v.SetDefault("gateway.retry_backoff", "200ms")
	v.SetDefault("gateway.auto_disable_threshold", 5)
	v.SetDefault("gateway.auto_disable_window", "5m")
	v.SetDefault("gateway.default_qpm", 60)
	v.SetDefault("gateway.default_concurrency", 8)
	// 兜底倍率：15 点/1K 输入、60 点/1K 输出（即 ¥0.015 / ¥0.06 每千 token）
	v.SetDefault("gateway.default_prompt_ratio", 15.0)
	v.SetDefault("gateway.default_completion_ratio", 60.0)
	v.SetDefault("gateway.estimate_completion_tokens", 1024)
	v.SetDefault("gateway.quota_flush_interval", "5s")

	v.SetDefault("cors.enable", true)
	v.SetDefault("cors.allow_origins", []string{"*"})
	v.SetDefault("cors.allow_methods", []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"})
	v.SetDefault("cors.allow_headers", []string{"Authorization", "Content-Type", "X-Request-ID"})
	v.SetDefault("cors.max_age", "12h")

	v.SetDefault("web.enable", true)
	v.SetDefault("web.dist_dir", "./web/dist")
	v.SetDefault("oauth.providers", []OAuthProviderConfig{})

	v.SetDefault("auth.frontend_base", "http://localhost:5173")
	v.SetDefault("auth.github.enabled", false)
	v.SetDefault("auth.github.client_id", "")
	v.SetDefault("auth.github.client_secret", "")
	v.SetDefault("auth.github.scopes", []string{"read:user", "user:email"})
	v.SetDefault("auth.webauthn.enabled", true)
	v.SetDefault("auth.webauthn.rp_id", "localhost")
	v.SetDefault("auth.webauthn.rp_display_name", "鲸闸 WhaleGate")
	v.SetDefault("auth.webauthn.rp_origins", []string{"http://localhost:5173"})
}

// Validate 校验必填项与取值合法性。
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 非法: %d", c.Server.Port)
	}
	switch c.Server.Mode {
	case "debug", "release", "test":
	default:
		return fmt.Errorf("server.mode 非法: %s", c.Server.Mode)
	}
	if c.Database.User == "" || c.Database.DBName == "" {
		return errors.New("database.user 与 database.dbname 不能为空")
	}
	if len(c.Security.EncryptionKey) != 32 {
		return fmt.Errorf("security.encryption_key 必须为 32 字节，当前 %d", len(c.Security.EncryptionKey))
	}
	return nil
}

// Addr 返回 HTTP 监听地址。
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// DSN 返回 GORM (pgx) 使用的 key=value 连接串。
func (c DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode, c.TimeZone,
	)
}

// MigrationDSN 返回 golang-migrate 使用的 URL 形式连接串。
func (c DatabaseConfig) MigrationDSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:   "/" + c.DBName,
	}
	q := u.Query()
	q.Set("sslmode", c.SSLMode)
	if c.TimeZone != "" {
		q.Set("TimeZone", c.TimeZone)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
