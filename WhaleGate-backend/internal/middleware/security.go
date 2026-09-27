package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/config"
)

// 默认 CSP：仅允许同源资源；AntDV 依赖内联样式，因此 style-src 需要 unsafe-inline。
const defaultCSP = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self' ws: wss:; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// SecurityHeaders 统一加固响应头，降低 XSS / 点击劫持 / 嗅探等风险。
func SecurityHeaders(cfg config.SecurityConfig) gin.HandlerFunc {
	csp := cfg.CSP
	if csp == "" {
		csp = defaultCSP
	}
	frameOptions := cfg.FrameOptions
	if frameOptions == "" {
		frameOptions = "DENY"
	}

	return func(c *gin.Context) {
		if !cfg.SecureHeaders {
			c.Next()
			return
		}

		header := c.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", frameOptions)
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("X-Permitted-Cross-Domain-Policies", "none")
		header.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		header.Set("Content-Security-Policy", csp)
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		if cfg.HSTSMaxAge > 0 {
			header.Set("Strict-Transport-Security", "max-age="+itoa(cfg.HSTSMaxAge)+"; includeSubDomains")
		}
		c.Next()
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	buf := make([]byte, 0, 12)
	for v > 0 {
		buf = append([]byte{byte('0' + v%10)}, buf...)
		v /= 10
	}
	return string(buf)
}
