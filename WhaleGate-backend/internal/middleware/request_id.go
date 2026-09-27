// Package middleware 提供 Gin 通用中间件。
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/whalegate/whalegate/internal/constant"
)

// RequestID 生成或透传链路追踪 ID，并写入响应头与上下文。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(constant.HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(constant.CtxTraceID, id)
		c.Header(constant.HeaderRequestID, id)
		c.Header(constant.HeaderTraceID, id)
		c.Next()
	}
}
