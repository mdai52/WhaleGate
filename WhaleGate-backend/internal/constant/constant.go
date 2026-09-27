// Package constant 存放跨包共享的常量，避免循环依赖。
package constant

// Gin context / HTTP header 键。
const (
	// CtxTraceID 链路追踪 ID 在 gin.Context 中的键。
	CtxTraceID = "trace_id"
	// CtxUserID 当前登录用户 ID。
	CtxUserID = "user_id"
	// CtxUsername 当前登录用户名（审计留痕用）。
	CtxUsername = "username"
	// CtxUserRole 当前登录用户角色。
	CtxUserRole = "user_role"
	// CtxMustChangePassword 当前用户是否仍需修改初始密码。
	CtxMustChangePassword = "must_change_password"
	// CtxAPIKeyID 当前请求使用的 API Key ID。
	CtxAPIKeyID = "api_key_id"
	// CtxAPIKey 当前请求使用的 API Key 实体。
	CtxAPIKey = "api_key"
	// CtxRequestBody 缓存的请求体（用于签名/审计）。
	CtxRequestBody = "request_body"
	// CtxErrorDetail 失败响应的内部排查明细（不返回给客户端）。
	CtxErrorDetail = "error_detail"

	// HeaderRequestID 请求 ID 头。
	HeaderRequestID = "X-Request-ID"
	// HeaderAuthorization 鉴权头。
	HeaderAuthorization = "Authorization"
	// HeaderAPIKey 兼容 x-api-key 方式的鉴权头。
	HeaderAPIKey = "X-Api-Key"
	// HeaderTraceID 响应中回传的追踪头。
	HeaderTraceID = "X-Trace-ID"
)

// 用户角色。
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// 通用状态。
const (
	StatusEnabled  = 1
	StatusDisabled = 2
)

// 默认分页参数。
const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 200
)

// 计量单位：1 点 = 0.001 元。所有金额以「点」的整数形式存储，避免浮点误差。
const PointsPerYuan = 1000
