package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// AuthRateLimit 对登录、注册等认证接口按 IP 限流，抵御暴力破解与灌水。
// 缓存故障时限流器返回错误，这里选择放行，避免 Redis 故障导致无法登录。
func AuthRateLimit(svc *service.Container, limit int, window time.Duration) gin.HandlerFunc {
	if limit <= 0 {
		limit = 20
	}
	if window <= 0 {
		window = time.Minute
	}

	return func(c *gin.Context) {
		res, err := svc.Limiter.Allow(c.Request.Context(), "auth:"+c.ClientIP(), limit, window)
		if err != nil {
			c.Next()
			return
		}
		if !res.Allowed {
			c.Header("Retry-After", strconvItoa(int(res.RetryAfter.Seconds())+1))
			response.AbortCode(c, apierr.ErrRateLimited, "操作过于频繁，请稍后再试")
			return
		}
		c.Next()
	}
}

// BodyLimit 限制请求体大小，防止超大 payload 打爆内存。
func BodyLimit(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if n > 0 && c.Request.ContentLength > n {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"code": apierr.CodeBadRequest, "message": "请求体过大"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		c.Next()
	}
}

func strconvItoa(v int) string {
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
