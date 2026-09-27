package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	whalegatejwt "github.com/whalegate/whalegate/internal/pkg/jwt"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// JWTAuth 校验登录令牌（Bearer Token），成功后注入用户上下文。
func JWTAuth(jm *whalegatejwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			response.AbortCode(c, apierr.ErrUnauthorized, "缺少登录令牌")
			return
		}
		claims, err := jm.Parse(token)
		if err != nil {
			response.Abort(c, err)
			return
		}
		c.Set(constant.CtxUserID, claims.UserID)
		c.Set(constant.CtxUsername, claims.Username)
		c.Set(constant.CtxUserRole, claims.Role)
		c.Set(constant.CtxMustChangePassword, claims.MustChange)
		c.Next()
	}
}

// RequirePasswordChanged 使用初始密码登录时，除改密与登出外一律拒绝，
// 避免初始密码长期可用。
func RequirePasswordChanged() gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get(constant.CtxMustChangePassword)
		if !ok {
			c.Next()
			return
		}
		if must, _ := v.(bool); must {
			response.AbortCode(c, apierr.ErrForbidden, "请先修改初始密码")
			return
		}
		c.Next()
	}
}

// OptionalJWTAuth 有令牌则解析，无令牌继续执行（用于可匿名访问的接口）。
func OptionalJWTAuth(jm *whalegatejwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token := bearerToken(c); token != "" {
			if claims, err := jm.Parse(token); err == nil {
				c.Set(constant.CtxUserID, claims.UserID)
				c.Set(constant.CtxUserRole, claims.Role)
			}
		}
		c.Next()
	}
}

// RequireRole 角色校验，需配合 JWTAuth 使用。
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c *gin.Context) {
		role, _ := c.Get(constant.CtxUserRole)
		current, _ := role.(string)
		if _, ok := allowed[current]; !ok {
			response.AbortCode(c, apierr.ErrForbidden, "需要角色: "+strings.Join(roles, ","))
			return
		}
		c.Next()
	}
}

// APIKeyAuth 网关侧鉴权：解析 sk- 前缀的 API Key 并注入凭证上下文。
func APIKeyAuth(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := extractAPIKey(c)
		if raw == "" {
			response.AbortCode(c, apierr.ErrUnauthorized,
				"缺少 API Key，请通过 Authorization: Bearer sk-xxx 或 X-Api-Key 传入")
			return
		}

		idn, err := svc.ResolveAPIKey(c.Request.Context(), raw)
		if err != nil {
			response.Abort(c, err)
			return
		}
		if eno := idn.RejectErrno(); eno.Code != apierr.CodeOK {
			response.AbortCode(c, eno, "")
			return
		}
		// 密钥 IP 白名单：启用后仅允许名单内的来源调用
		if !idn.IPAllowed(c.ClientIP()) {
			response.AbortCode(c, apierr.ErrForbidden, "来源 IP 不在该 API Key 的白名单内")
			return
		}

		c.Set(constant.CtxAPIKeyID, idn.KeyID)
		c.Set(constant.CtxAPIKey, idn)
		c.Set(constant.CtxUserID, idn.UserID)
		c.Set(constant.CtxUserRole, idn.UserRole)
		svc.MarkKeyUsed(idn.KeyID)
		c.Next()
	}
}

func bearerToken(c *gin.Context) string {
	header := c.GetHeader(constant.HeaderAuthorization)
	if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

// extractAPIKey 支持 Authorization: Bearer sk-xxx、X-Api-Key 与查询参数三种方式。
func extractAPIKey(c *gin.Context) string {
	if v := c.GetHeader(constant.HeaderAPIKey); v != "" {
		return strings.TrimSpace(v)
	}
	if token := bearerToken(c); token != "" {
		return token
	}
	if v := c.Query("api_key"); v != "" {
		return strings.TrimSpace(v)
	}
	return ""
}
