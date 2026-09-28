package util

import (
	"net"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequestScheme 判断客户端实际使用的请求协议，优先读取 X-Forwarded-Proto，
// 其次根据当前连接是否 TLS 判定。
func RequestScheme(c *gin.Context) string {
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		return strings.ToLower(proto)
	}
	if c.Request.TLS != nil {
		return "https"
	}
	return "http"
}

// RealClientIP 返回真实客户端 IP。
// 若显式指定了 realIPHeader（如 X-Real-IP），优先读取该头；否则使用 gin 的
// ClientIP，其会按 TrustedProxies 配置解析 X-Forwarded-For。
func RealClientIP(c *gin.Context, realIPHeader string) string {
	if realIPHeader != "" {
		ip := c.GetHeader(realIPHeader)
		if ip != "" {
			if idx := strings.Index(ip, ","); idx >= 0 {
				ip = ip[:idx]
			}
			ip = strings.TrimSpace(ip)
			if host, _, err := net.SplitHostPort(ip); err == nil {
				ip = host
			}
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	return c.ClientIP()
}
