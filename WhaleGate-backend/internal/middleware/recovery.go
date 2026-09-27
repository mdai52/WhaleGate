package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/response"
)

// Recovery 捕获 panic，记录堆栈并返回统一错误响应，避免连接被直接中断。
func Recovery(lg *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				lg.Error("请求处理发生 panic",
					zap.Any("panic", r),
					zap.String("path", c.Request.URL.Path),
					zap.String("method", c.Request.Method),
					zap.String("stack", string(debug.Stack())),
				)
				if c.Writer.Written() {
					c.Abort()
					return
				}
				c.Writer.Header().Del("Trailer")
				response.FailCode(c, apierr.ErrInternal, "")
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
