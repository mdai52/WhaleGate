package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/constant"
)

// AccessLog 记录每次 HTTP 访问的结构化日志。
func AccessLog(lg *zap.Logger, skipPaths ...string) gin.HandlerFunc {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}

	return func(c *gin.Context) {
		if _, ok := skip[c.FullPath()]; ok {
			c.Next()
			return
		}

		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		fields := []zap.Field{
			zap.String("trace_id", traceID(c)),
			zap.Int("status", c.Writer.Status()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			// 查询串中的凭证一律脱敏，避免日志泄露密钥
			zap.String("query", sanitizeQuery(query)),
			zap.String("client_ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Duration("latency", time.Since(start)),
			zap.Int("body_size", c.Writer.Size()),
		}
		if uid, ok := c.Get(constant.CtxUserID); ok {
			fields = append(fields, zap.Any("user_id", uid))
		}
		if kid, ok := c.Get(constant.CtxAPIKeyID); ok {
			fields = append(fields, zap.Any("api_key_id", kid))
		}

		detail := ""
		if v, ok := c.Get(constant.CtxErrorDetail); ok {
			if s, ok := v.(string); ok {
				detail = s
			}
		}
		if len(c.Errors) > 0 {
			detail = appendErrors(detail, c.Errors.Errors())
		}
		if detail != "" {
			fields = append(fields, zap.String("detail", detail))
		}

		msg := "HTTP " + c.Request.Method + " " + path
		switch {
		case c.Writer.Status() >= 500:
			lg.Error(msg, fields...)
		case c.Writer.Status() >= 400:
			lg.Warn(msg, fields...)
		default:
			lg.Info(msg, fields...)
		}
	}
}

// sensitiveQueryKeys 需要在访问日志中脱敏的查询参数。
var sensitiveQueryKeys = map[string]struct{}{
	"api_key": {}, "apikey": {}, "key": {}, "token": {},
	"access_token": {}, "refresh_token": {}, "password": {}, "secret": {},
}

// sanitizeQuery 把查询串中的敏感参数值替换为 ***。
func sanitizeQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	changed := false
	for i, part := range parts {
		name := part
		if idx := strings.IndexByte(part, '='); idx >= 0 {
			name = part[:idx]
		}
		if _, ok := sensitiveQueryKeys[strings.ToLower(name)]; ok {
			parts[i] = name + "=***"
			changed = true
		}
	}
	if !changed {
		return raw
	}
	return strings.Join(parts, "&")
}

func traceID(c *gin.Context) string {
	if v, ok := c.Get(constant.CtxTraceID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func appendErrors(detail string, errs []string) string {
	for _, e := range errs {
		if detail == "" {
			detail = e
			continue
		}
		detail += "; " + e
	}
	return detail
}
