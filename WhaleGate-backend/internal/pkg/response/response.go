// Package response 定义鲸闸的统一 HTTP 响应格式与 gin 写入助手。
package response

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

// Body 是所有 JSON 接口的统一信封。
type Body struct {
	// Code 业务错误码，0 表示成功。
	Code int `json:"code"`
	// Message 人类可读提示（成功为 ok）。
	Message string `json:"message"`
	// Data 业务数据，失败时为空。
	Data interface{} `json:"data,omitempty"`
	// TraceID 链路追踪 ID，便于排查问题。
	TraceID string `json:"trace_id,omitempty"`
	// Timestamp 响应生成时间（Unix 秒）。
	Timestamp int64 `json:"timestamp"`
}

// PagedData 统一分页结构。
type PagedData[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// OK 返回 200 成功响应。
func OK(c *gin.Context, data interface{}) {
	write(c, http.StatusOK, apierr.ErrOK, data)
}

// Created 返回 201 成功响应。
func Created(c *gin.Context, data interface{}) {
	write(c, http.StatusCreated, apierr.ErrOK, data)
}

// NoContent 返回 204。
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Fail 根据错误自适应业务码与 HTTP 状态码。
func Fail(c *gin.Context, err error) {
	if err == nil {
		FailCode(c, apierr.ErrInternal, "")
		return
	}
	be := apierr.From(err)
	write(c, be.HTTP, be.Errno, nil)
}

// FailCode 以指定业务码失败；detail 仅写入上下文用于日志排查，不返回给客户端。
func FailCode(c *gin.Context, eno apierr.Errno, detail string) {
	if detail != "" {
		c.Set(constant.CtxErrorDetail, detail)
	}
	write(c, eno.HTTP, eno, nil)
}

// AbortCode 以指定业务码中断请求。
func AbortCode(c *gin.Context, eno apierr.Errno, detail string) {
	FailCode(c, eno, detail)
	c.Abort()
}

// Abort 中断请求并返回错误响应（用于中间件）。
func Abort(c *gin.Context, err error) {
	Fail(c, err)
	c.Abort()
}

// Page 返回分页响应。
func Page[T any](c *gin.Context, items []T, total int64, page, pageSize int) {
	OK(c, PagedData[T]{Items: items, Total: total, Page: page, PageSize: pageSize})
}

func write(c *gin.Context, httpStatus int, eno apierr.Errno, data interface{}) {
	body := Body{
		Code:      eno.Code,
		Message:   eno.Message,
		Data:      data,
		TraceID:   traceID(c),
		Timestamp: time.Now().Unix(),
	}
	if httpStatus == 0 {
		httpStatus = http.StatusInternalServerError
	}
	c.JSON(httpStatus, body)
}

func traceID(c *gin.Context) string {
	if v, ok := c.Get(constant.CtxTraceID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
