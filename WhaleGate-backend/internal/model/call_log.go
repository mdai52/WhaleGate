package model

import "time"

// 调用状态。
const (
	CallStatusSuccess = 1
	CallStatusFailed  = 2
)

// token 来源置信度。
const (
	// ConfidenceReported 上游明确返回 usage。
	ConfidenceReported = "reported"
	// ConfidenceEstimated 上游未返回 usage，按请求参数估算。
	ConfidenceEstimated = "estimated"
)

// CallLog 每次转发的调用日志，用于计费审计与用量统计。
type CallLog struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint   `gorm:"not null;index" json:"user_id"`
	APIKeyID    uint   `gorm:"not null;index" json:"api_key_id"`
	ChannelID   uint   `gorm:"index" json:"channel_id"`
	ChannelName string `gorm:"size:128" json:"channel_name,omitempty"`
	// Model 对外模型名。
	Model string `gorm:"size:128;index" json:"model"`
	// UpstreamModel 经别名/映射改写后的上游模型名。
	UpstreamModel string `gorm:"size:128" json:"upstream_model,omitempty"`
	// Protocol 客户端 API 协议（openai / gemini）。
	Protocol string `gorm:"size:32" json:"protocol"`
	// RequestScheme 客户端请求协议：http / https。
	RequestScheme string `gorm:"size:8" json:"request_scheme,omitempty"`
	Stream        bool   `json:"stream"`

	// 计量：completion 已包含思维链（reasoning）。
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// Points 实际扣减点数。自用模式下记录"应扣点数"用于成本分析，但用户余额不扣减。
	Points int64 `json:"points"`
	// SelfUse 该次调用发生在自用模式下（免费）。
	SelfUse bool `json:"self_use"`
	// ToolRounds 多轮工具执行轮次，0 表示未使用工具。
	ToolRounds int `json:"tool_rounds"`

	LatencyMS    int64 `json:"latency_ms"`
	FirstTokenMS int64 `json:"first_token_ms"`

	// Status 1 成功 / 2 失败。
	Status       int    `json:"status"`
	HTTPStatus   int    `json:"http_status"`
	ErrorCode    int    `json:"error_code,omitempty"`
	ErrorMessage string `gorm:"size:512" json:"error_message,omitempty"`
	// UsageConfidence reported / estimated。
	UsageConfidence string `gorm:"size:16" json:"usage_confidence"`
	// ParamsApplied / ParamsDropped 自定义参数分配结果（逗号分隔的统一参数名）。
	ParamsApplied string `gorm:"type:text" json:"params_applied,omitempty"`
	ParamsDropped string `gorm:"type:text" json:"params_dropped,omitempty"`

	TraceID   string    `gorm:"size:64;index" json:"trace_id,omitempty"`
	ClientIP  string    `gorm:"size:64" json:"client_ip,omitempty"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// TableName 表名。
func (CallLog) TableName() string { return "call_logs" }
