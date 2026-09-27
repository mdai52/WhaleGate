// Package router 注册 HTTP 路由与中间件链。
package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/redis/go-redis/v9"

	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/handler"
	"github.com/whalegate/whalegate/internal/middleware"
	"github.com/whalegate/whalegate/internal/service"
)

// Deps 路由装配所需的依赖。
type Deps struct {
	Config  *config.Config
	DB      *gorm.DB
	RDB     *redis.Client
	Logger  *zap.Logger
	Svc     *service.Container
	Version string
}

// NewEngine 构建 gin 引擎并注册全部路由。
func NewEngine(d Deps) *gin.Engine {
	gin.SetMode(d.Config.Server.Mode)
	engine := gin.New()

	engine.Use(middleware.RequestID())
	engine.Use(middleware.Recovery(d.Logger))
	engine.Use(middleware.SecurityHeaders(d.Config.Security))
	engine.Use(middleware.AccessLog(d.Logger, "/healthz", "/readyz"))
	engine.Use(middleware.CORS(d.Config.CORS))
	// 非流式接口请求体上限 8MB（证书上传等场景由该值兜底）
	engine.Use(middleware.BodyLimit(8 << 20))
	engine.MaxMultipartMemory = 8 << 20

	registerSystem(engine, d)
	registerAPI(engine, d)
	registerGateway(engine, d)
	registerSPA(engine, d)

	return engine
}

func registerSystem(e *gin.Engine, d Deps) {
	sys := handler.NewSystemHandler(d.DB, d.RDB, d.Logger, d.Version, d.Svc)
	e.GET("/healthz", sys.Health)
	e.GET("/readyz", sys.Ready)

	v1 := e.Group("/api/v1")
	v1.GET("/system/info", sys.Info)
	// 公开：首次安装向导依赖这两个接口
	v1.GET("/system/status", sys.Status)
	v1.POST("/system/install", sys.Install)
}

func registerAPI(e *gin.Engine, d Deps) {
	auth := handler.NewAuthHandler(d.Svc)
	keys := handler.NewAPIKeyHandler(d.Svc)
	admin := handler.NewAdminHandler(d.Svc)
	channel := handler.NewChannelHandler(d.Svc)
	settings := handler.NewSettingsHandler(d.Svc)
	skill := handler.NewSkillHandler(d.Svc)
	mcpHandler := handler.NewMCPHandler(d.Svc)
	usage := handler.NewUsageHandler(d.Svc)
	oauthHandler := handler.NewOAuthHandler(d.Svc)
	credentialHandler := handler.NewCredentialHandler(d.Svc)
	account := handler.NewAccountHandler(d.Svc)

	pub := e.Group("/api/v1/auth")
	// 认证接口按 IP 限流 + 登录失败锁定（见 service.Login）
	pub.Use(middleware.AuthRateLimit(d.Svc, d.Config.Security.AuthRateLimit, time.Minute))
	{
		pub.POST("/register", middleware.OptionalJWTAuth(d.Svc.JWT), auth.Register)
		pub.POST("/login", auth.Login)
		// 第三方登录：GitHub 回调与一次性票据换取令牌
		pub.GET("/github/callback", account.GitHubCallback)
		pub.POST("/ticket", account.ExchangeTicket)
		// 通行密钥登录（无用户名）
		pub.POST("/passkey/login/begin", account.PasskeyLoginBegin)
		pub.POST("/passkey/login/finish", account.PasskeyLoginFinish)
	}

	// 改密与登出在初始密码状态下依然可用
	authed := e.Group("/api/v1")
	authed.Use(middleware.JWTAuth(d.Svc.JWT))
	{
		authed.POST("/auth/logout", auth.Logout)
		authed.POST("/auth/change-password", auth.ChangePassword)
		// /me 需要返回 must_change_password，不能拦截
		authed.GET("/me", auth.Me)
	}

	// 其余已登录接口在"未修改初始密码"时被拦截
	authed = e.Group("/api/v1")
	authed.Use(middleware.JWTAuth(d.Svc.JWT), middleware.RequirePasswordChanged())
	{
		keyGroup := authed.Group("/keys")
		{
			keyGroup.POST("", keys.Create)
			keyGroup.GET("", keys.List)
			keyGroup.POST("/:id/revoke", keys.Revoke)
			keyGroup.DELETE("/:id", keys.Delete)
		}

		usageGroup := authed.Group("/usage")
		{
			usageGroup.GET("/logs", usage.MyLogs)
			usageGroup.GET("/summary", usage.Summary)
		}

		accountGroup := authed.Group("/account")
		{
			accountGroup.GET("/bindings", account.Bindings)
			accountGroup.DELETE("/bindings/identity/:id", account.UnbindIdentity)
			accountGroup.DELETE("/bindings/passkey/:id", account.DeletePasskey)
			accountGroup.POST("/passkey/register/begin", account.PasskeyRegisterBegin)
			accountGroup.POST("/passkey/register/finish", account.PasskeyRegisterFinish)
			accountGroup.GET("/github/authorize", account.GitHubAuthorize)
			accountGroup.POST("/github/callback", account.GitHubCallbackJSON)
		}
	}

	adminGroup := e.Group("/api/v1/admin")
	adminGroup.Use(middleware.JWTAuth(d.Svc.JWT), middleware.RequireRole(constant.RoleAdmin))
	{
		adminGroup.GET("/users", admin.ListUsers)
		adminGroup.POST("/users", admin.CreateUser)
		adminGroup.PATCH("/users/:id/status", admin.SetUserStatus)
		adminGroup.POST("/users/:id/quota", admin.ChangeQuota)

		channels := adminGroup.Group("/channels")
		{
			channels.POST("", channel.Create)
			channels.POST("/detect", channel.Detect)
			channels.GET("/model-catalog", channel.Catalog)
			channels.GET("", channel.List)
			channels.GET("/:id", channel.Get)
			channels.PUT("/:id", channel.Update)
			channels.DELETE("/:id", channel.Delete)
			channels.POST("/:id/test", channel.Test)

			// 全局设置：自用模式、Skills 与 MCP 开关（仅管理员）
			adminGroup.GET("/settings", settings.Get)
			adminGroup.PUT("/settings", settings.Update)

			// 技能管理
			skills := adminGroup.Group("/skills")
			{
				skills.GET("", skill.List)
				skills.POST("/scan", skill.Scan)
				skills.PATCH("/:id/toggle", skill.Toggle)
				skills.DELETE("/:id", skill.Delete)
			}

			// MCP 服务与工具
			mcpGroup := adminGroup.Group("/mcp")
			{
				mcpGroup.GET("", mcpHandler.List)
				mcpGroup.POST("", mcpHandler.Create)
				mcpGroup.PUT("/:id", mcpHandler.Update)
				mcpGroup.DELETE("/:id", mcpHandler.Delete)
				mcpGroup.POST("/:id/connect", mcpHandler.Connect)
				mcpGroup.GET("/tools", mcpHandler.Tools)
				mcpGroup.POST("/tools/call", mcpHandler.Call)
			}
		}

		ratios := adminGroup.Group("/ratios")
		{
			ratios.GET("", admin.ListRatios)
			ratios.POST("", admin.UpsertRatio)
			ratios.DELETE("/:id", admin.DeleteRatio)
		}

		adminGroup.GET("/logs", admin.ListLogs)
		adminGroup.GET("/logs/export", admin.ExportLogs)
		adminGroup.GET("/audit-logs", admin.ListAuditLogs)

		oauthGroup := adminGroup.Group("/oauth")
		{
			oauthGroup.GET("/providers", oauthHandler.Providers)
			oauthGroup.POST("/:provider/start", oauthHandler.Start)
			oauthGroup.POST("/:provider/complete", oauthHandler.Complete)
			oauthGroup.POST("/:provider/poll", oauthHandler.Poll)
		}

		credentials := adminGroup.Group("/credentials")
		{
			credentials.GET("", credentialHandler.List)
			credentials.POST("", credentialHandler.Import)
			credentials.PATCH("/:id/status", credentialHandler.SetStatus)
			credentials.PATCH("/:id/name", credentialHandler.Rename)
			credentials.GET("/:id/export", credentialHandler.Export)
			credentials.POST("/:id/refresh", credentialHandler.Refresh)
			credentials.DELETE("/:id", credentialHandler.Delete)
		}
	}
}

// registerGateway 注册 OpenAI 兼容入口与 Gemini 原生入口。
func registerGateway(e *gin.Engine, d Deps) {
	relay := handler.NewRelayHandler(d.Svc)

	openaiGroup := e.Group("/v1")
	openaiGroup.Use(middleware.APIKeyAuth(d.Svc), middleware.RateLimit(d.Svc))
	{
		openaiGroup.GET("/models", relay.ListModels)
		openaiGroup.POST("/chat/completions", relay.ChatCompletions)
	}

	// Gemini 原生：/v1beta/models/{model}:generateContent 与 :streamGenerateContent
	geminiGroup := e.Group("", middleware.APIKeyAuth(d.Svc), middleware.RateLimit(d.Svc))
	{
		geminiGroup.POST("/v1beta/models/*fullpath", relay.GeminiNative)
		geminiGroup.POST("/gemini/v1beta/models/*fullpath", relay.GeminiNative)
	}
}

// registerSPA 托管前端静态资源，未匹配的非 API 请求回退到 index.html。
func registerSPA(e *gin.Engine, d Deps) {
	if !d.Config.Web.Enable {
		return
	}
	dist := d.Config.Web.DistDir
	index := filepath.Join(dist, "index.html")
	if _, err := os.Stat(index); err != nil {
		if d.Logger != nil {
			d.Logger.Info("未找到前端构建产物，跳过静态托管", zap.String("dir", dist))
		}
		return
	}

	e.Static("/assets", filepath.Join(dist, "assets"))
	e.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if isAPIPath(path) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		// 命中 dist 内实际存在的静态文件（favicon、logo 等），否则回退 SPA 入口。
		full := filepath.Join(dist, filepath.Clean("/"+path))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			c.File(full)
			return
		}
		c.File(index)
	})
}

// isAPIPath 判断是否为后端接口路径，这类路径不做 SPA 回退。
func isAPIPath(path string) bool {
	return strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/v1/") ||
		strings.HasPrefix(path, "/v1beta/") ||
		strings.HasPrefix(path, "/gemini/") ||
		path == "/healthz" || path == "/readyz"
}
