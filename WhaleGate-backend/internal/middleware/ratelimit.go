package middleware

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/ratelimit"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// RateLimit 按 API Key 维度做 QPM 滑动窗口限流与并发控制。
// 缓存故障时不阻断请求，仅记录告警日志，保证可用性优先。
func RateLimit(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		idn := currentIdentity(c)
		if idn == nil {
			c.Next()
			return
		}

		qpm := idn.QPM
		if qpm <= 0 {
			qpm = svc.Config.Gateway.DefaultQPM
		}
		if res, err := svc.Limiter.Allow(c.Request.Context(),
			fmt.Sprintf("key:%d", idn.KeyID), qpm, time.Minute); err != nil {
			logLimiterError(svc, err)
		} else if !res.Allowed {
			c.Header("X-RateLimit-Limit", strconv.Itoa(res.Limit))
			c.Header("X-RateLimit-Remaining", strconv.Itoa(maxInt(res.Limit-res.Used, 0)))
			c.Header("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())+1))
			response.AbortCode(c, apierr.ErrRateLimited,
				fmt.Sprintf("qpm=%d used=%d", res.Limit, res.Used))
			return
		}

		conc := idn.Concurrency
		if conc <= 0 {
			conc = svc.Config.Gateway.DefaultConcurrency
		}
		release, err := svc.Limiter.Acquire(c.Request.Context(),
			fmt.Sprintf("key:%d", idn.KeyID), conc, 5*time.Minute)
		if err != nil {
			if err == ratelimit.ErrConcurrencyExceeded {
				response.AbortCode(c, apierr.ErrConcurrencyLimited,
					fmt.Sprintf("concurrency=%d", conc))
				return
			}
			logLimiterError(svc, err)
		} else {
			defer release()
		}

		c.Next()
	}
}

func currentIdentity(c *gin.Context) *service.KeyIdentity {
	v, ok := c.Get(constant.CtxAPIKey)
	if !ok {
		return nil
	}
	idn, ok := v.(*service.KeyIdentity)
	if !ok {
		return nil
	}
	return idn
}

func logLimiterError(svc *service.Container, err error) {
	if svc.Logger != nil {
		svc.Logger.Warn("限流器异常，已放行请求", zap.Error(err))
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
