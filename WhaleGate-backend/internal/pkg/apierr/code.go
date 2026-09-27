// Package apierr 定义鲸闸的业务错误码与统一错误类型。
//
// 约定：业务码 code 的百位以上即 HTTP 状态码，例如 40401 -> HTTP 404。
package apierr

import "net/http"

// 业务错误码。
const (
	CodeOK = 0

	// 4xxxx 客户端错误
	CodeBadRequest         = 40000 // 请求格式错误
	CodeInvalidParam       = 40001 // 参数校验失败
	CodeInvalidJSON        = 40002 // JSON 解析失败
	CodeUnauthorized       = 40100 // 未认证
	CodeInvalidCredential  = 40101 // 凭证无效
	CodeTokenExpired       = 40102 // 令牌过期
	CodeKeyRevoked         = 40103 // API Key 已吊销
	CodeKeyExpired         = 40104 // API Key 已过期
	CodeForbidden          = 40300 // 无权限
	CodeUserDisabled       = 40301 // 账号已禁用
	CodeNotFound           = 40400 // 资源不存在
	CodeModelNotFound      = 40401 // 模型不存在
	CodeChannelNotFound    = 40402 // 渠道不存在
	CodeConflict           = 40900 // 资源冲突（如用户名已存在）
	CodeQuotaExceeded      = 40201 // 余额不足
	CodeRateLimited        = 42900 // 触发限流
	CodeConcurrencyLimited = 42901 // 并发超限

	// 5xxxx 服务端错误
	CodeInternal        = 50000
	CodeDatabase        = 50001
	CodeCache           = 50002
	CodeCrypto          = 50003
	CodeUpstream        = 50200 // 上游调用失败
	CodeUpstreamInvalid = 50201 // 上游返回无法解析
	CodeNoChannel       = 50301 // 无可用渠道
	CodeUpstreamTimeout = 50400 // 上游超时
)

// Errno 是不可变的错误码定义。
type Errno struct {
	// Code 业务错误码。
	Code int `json:"code"`
	// HTTP 对应的 HTTP 状态码。
	HTTP int `json:"-"`
	// Message 面向用户的中文提示。
	Message string `json:"message"`
}

// Define 由业务码派生标准错误码（HTTP = code / 100）。
func Define(code int, message string) Errno {
	httpStatus := code / 100
	if httpStatus < 100 || httpStatus > 599 {
		httpStatus = http.StatusInternalServerError
	}
	return Errno{Code: code, HTTP: httpStatus, Message: message}
}

var (
	ErrOK = Define(CodeOK, "ok")

	ErrBadRequest         = Define(CodeBadRequest, "请求格式错误")
	ErrInvalidParam       = Define(CodeInvalidParam, "参数校验失败")
	ErrInvalidJSON        = Define(CodeInvalidJSON, "请求体 JSON 解析失败")
	ErrUnauthorized       = Define(CodeUnauthorized, "未认证，请先登录")
	ErrInvalidCredential  = Define(CodeInvalidCredential, "凭证无效")
	ErrTokenExpired       = Define(CodeTokenExpired, "登录已过期，请重新登录")
	ErrKeyRevoked         = Define(CodeKeyRevoked, "API Key 已吊销")
	ErrKeyExpired         = Define(CodeKeyExpired, "API Key 已过期")
	ErrForbidden          = Define(CodeForbidden, "无权访问该资源")
	ErrUserDisabled       = Define(CodeUserDisabled, "账号已被禁用")
	ErrNotFound           = Define(CodeNotFound, "资源不存在")
	ErrModelNotFound      = Define(CodeModelNotFound, "模型不存在")
	ErrChannelNotFound    = Define(CodeChannelNotFound, "渠道不存在")
	ErrConflict           = Define(CodeConflict, "资源冲突")
	ErrQuotaExceeded      = Define(CodeQuotaExceeded, "余额不足，请充值后再试")
	ErrRateLimited        = Define(CodeRateLimited, "请求过于频繁，请稍后再试")
	ErrConcurrencyLimited = Define(CodeConcurrencyLimited, "并发请求数超出限制")

	ErrInternal        = Define(CodeInternal, "服务内部错误")
	ErrDatabase        = Define(CodeDatabase, "数据库异常")
	ErrCache           = Define(CodeCache, "缓存服务异常")
	ErrCrypto          = Define(CodeCrypto, "加解密失败")
	ErrUpstream        = Define(CodeUpstream, "上游模型服务调用失败")
	ErrUpstreamInvalid = Define(CodeUpstreamInvalid, "上游返回内容无法解析")
	ErrNoChannel       = Define(CodeNoChannel, "暂无可用渠道，请联系管理员")
	ErrUpstreamTimeout = Define(CodeUpstreamTimeout, "上游模型服务响应超时")
)
