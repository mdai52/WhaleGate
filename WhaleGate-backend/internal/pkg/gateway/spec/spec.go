// Package gateway 定义鲸闸内部统一的请求/响应格式（UnifiedRequest / UnifiedResponse）。
//
// 所有外部协议（OpenAI 兼容、Gemini 原生等）都先转换为该内部格式，
// 再交由渠道适配器转换为上游协议，从而实现"一次接入、多协议互通"。
package spec

import "strings"

// 协议标识。
const (
	ProtocolOpenAI = "openai"
	ProtocolGemini = "gemini"
)

// 消息角色。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// 结束原因。
const (
	FinishStop      = "stop"
	FinishLength    = "length"
	FinishToolCalls = "tool_calls"
)

// Message 统一消息体。
type Message struct {
	// Role 角色：system / user / assistant / tool。
	Role string `json:"role"`
	// Content 文本内容。
	Content string `json:"content,omitempty"`
	// Name 可选，用于区分多参与者或工具名。
	Name string `json:"name,omitempty"`
	// ToolCallID 工具调用结果对应的调用 ID。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolCalls 助手消息中的工具调用列表。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Images 非标扩展归一化后的图片结果（生图等场景）。
	// 上游可能放在 message.images[]（data URI）或 content 的 image_url 片段中。
	Images []Image `json:"images,omitempty"`
}

// Image 统一图片内容。
type Image struct {
	// Type 取值：image_url（远程或 data URI）、inline_data（原始 base64）。
	Type string `json:"type,omitempty"`
	// MimeType 例如 image/png。
	MimeType string `json:"mime_type,omitempty"`
	// B64Data 纯 base64 载荷，不含 "data:image/png;base64," 前缀。
	B64Data string `json:"b64_data,omitempty"`
	// URL 远程地址，与 B64Data 二选一。
	URL string `json:"url,omitempty"`
}

// DataURI 生成 data URI；无 base64 载荷时返回 URL 本身。
func (img Image) DataURI() string {
	if img.B64Data == "" {
		return img.URL
	}
	mime := img.MimeType
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + img.B64Data
}

// IsEmpty 是否为空图片。
func (img Image) IsEmpty() bool { return img.B64Data == "" && img.URL == "" }

// ParseDataURI 解析 "data:image/png;base64,xxx" 形式的 URI。
func ParseDataURI(uri string) (Image, bool) {
	const prefix = "data:"
	if len(uri) <= len(prefix) || uri[:len(prefix)] != prefix {
		return Image{}, false
	}
	rest := uri[len(prefix):]
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return Image{}, false
	}
	meta := rest[:comma]
	payload := rest[comma+1:]

	img := Image{Type: "image_url"}
	if idx := strings.IndexByte(meta, ';'); idx >= 0 {
		img.MimeType = meta[:idx]
		if strings.HasSuffix(meta[idx+1:], "base64") {
			img.B64Data = payload
			return img, true
		}
	}
	// 非 base64（可能是 URL 编码文本），退化为 URL 透传。
	img.MimeType = meta
	img.URL = uri
	return img, true
}

// ToolCall 统一工具调用描述。
type ToolCall struct {
	// ID 调用标识。
	ID string `json:"id"`
	// Type 固定为 function。
	Type string `json:"type"`
	// Name 函数名。
	Name string `json:"name,omitempty"`
	// Arguments 参数 JSON 字符串（上游协议要求的原始字符串）。
	Arguments string `json:"arguments,omitempty"`
}

// ToolDefinition 统一工具定义（OpenAI tools / Gemini functionDeclarations）。
type ToolDefinition struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// Description 描述。
	Description string `json:"description,omitempty"`
	// Parameters JSON Schema。
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// ParamSchema 渠道/上游模型的参数能力声明，用于自定义参数的自动识别与分配。
type ParamSchema struct {
	// Supported 该上游支持的统一参数名；为空时使用协议默认支持集。
	Supported []string `json:"supported,omitempty"`
	// Exclude 明确不支持的参数，优先级高于 Supported。
	Exclude []string `json:"exclude,omitempty"`
	// Defaults 用户未传参时使用的默认值（统一参数名 -> 值）。
	Defaults map[string]interface{} `json:"defaults,omitempty"`
}

// Usage token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// ReasoningTokens 思维链（CoT）消耗，按 OpenAI 约定已计入 CompletionTokens。
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
	TotalTokens     int `json:"total_tokens"`
}

// Add 累加用量。
func (u *Usage) Add(other *Usage) {
	if u == nil || other == nil {
		return
	}
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	u.ReasoningTokens += other.ReasoningTokens
	u.TotalTokens += other.TotalTokens
}

// Normalize 补齐 TotalTokens：上游缺失时按 prompt + completion 推导。
func (u *Usage) Normalize() {
	if u == nil {
		return
	}
	if u.TotalTokens <= 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	if u.ReasoningTokens > u.CompletionTokens {
		// 上游把思维链单独计数时，补全 completion 以免计量偏小。
		u.CompletionTokens = u.ReasoningTokens
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
}

// BillableTokens 计费口径：prompt + completion（含 reasoning）。
func (u *Usage) BillableTokens() (prompt, completion int) {
	if u == nil {
		return 0, 0
	}
	return u.PromptTokens, u.CompletionTokens
}

// UnifiedRequest 统一请求。
type UnifiedRequest struct {
	// Model 对外暴露的模型名。
	Model string `json:"model"`
	// Messages 对话消息。
	Messages []Message `json:"messages"`
	// Stream 是否流式。
	Stream bool `json:"stream"`
	// Temperature 采样温度，nil 表示使用上游默认值。
	Temperature *float64 `json:"temperature,omitempty"`
	// TopP 核采样。
	TopP *float64 `json:"top_p,omitempty"`
	// MaxTokens 最大生成 token 数。
	MaxTokens *int `json:"max_tokens,omitempty"`
	// Stop 停止词。
	Stop []string `json:"stop,omitempty"`
	// Tools 工具定义。
	Tools []ToolDefinition `json:"tools,omitempty"`
	// User 终端用户标识，用于上游风控。
	User string `json:"user,omitempty"`
	// Extra 用户自定义参数（协议未定义或本网关未标准化的字段）。
	// 系统会根据渠道声明的能力自动识别并分配到上游对应字段。
	Extra map[string]interface{} `json:"-"`
	// NativeProtocol 原始协议，便于渠道适配器做差异化处理。
	NativeProtocol string `json:"-"`
	// NativeModel 上游模型名（渠道映射后），为空时等于 Model。
	NativeModel string `json:"-"`
	// Raw 原始请求体，供需要原样透传的渠道使用。
	Raw map[string]interface{} `json:"-"`
}

// Choice 非流式响应中的一个候选。
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

// UnifiedResponse 统一非流式响应。
type UnifiedResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object,omitempty"`
	Model   string   `json:"model"`
	Created int64    `json:"created"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Text 便捷取首个候选的文本。
func (r *UnifiedResponse) Text() string {
	if r == nil || len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.Content
}

// Delta 流式增量内容。
type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Images 流式场景下的图片增量（通常出现在最后一个内容块）。
	Images []Image `json:"images,omitempty"`
}

// HasContent 判断增量块是否携带任何有效内容。
func (d Delta) HasContent() bool {
	return d.Role != "" || d.Content != "" || len(d.ToolCalls) > 0 || len(d.Images) > 0
}

// StreamChoice 流式响应中的一个候选。
type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

// StreamChunk 统一流式响应块。
type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object,omitempty"`
	Model   string         `json:"model"`
	Created int64          `json:"created"`
	Choices []StreamChoice `json:"choices"`
	// Usage 仅出现在最后一个块（协议允许时）。
	Usage *Usage `json:"usage,omitempty"`
}

// IsEmpty 是否为无内容块（仅用于过滤心跳）。
func (c *StreamChunk) IsEmpty() bool {
	if c == nil || len(c.Choices) == 0 {
		return true
	}
	for _, ch := range c.Choices {
		if ch.Delta.HasContent() || ch.FinishReason != nil {
			return false
		}
	}
	return true
}

// FinishReasonPtr 便捷构造结束原因指针。
func FinishReasonPtr(reason string) *string { return &reason }
