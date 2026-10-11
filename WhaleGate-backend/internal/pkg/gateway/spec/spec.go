// Package gateway 定义鲸闸内部统一的请求/响应格式（UnifiedRequest / UnifiedResponse）。
//
// 所有外部协议（OpenAI 兼容、Gemini 原生等）都先转换为该内部格式，
// 再交由渠道适配器转换为上游协议，从而实现"一次接入、多协议互通"。
package spec

import "strings"

// 协议标识。
//
// openai / gemini 既可作客户端入口也可作上游渠道；
// anthropic / claude-code 目前只作为上游渠道类型使用，
// 对外仍统一以 OpenAI 或 Gemini 协议输出（见 gateway.WriteClientResponse）。
//
// 判据是「上游是否说 OpenAI 协议」：说的一律用 openai，
// 端点、鉴权与字段命名的差异由 vendor 档案表补齐；
// 只有 Gemini、Anthropic 这类报文结构本身不同的上游才单列协议。
const (
	ProtocolOpenAI = "openai"
	ProtocolGemini = "gemini"
	// ProtocolAnthropic Anthropic Messages API（x-api-key 鉴权）。
	ProtocolAnthropic = "anthropic"
	// ProtocolClaudeCode Claude Code 网关：Anthropic 消息结构 + Bearer 鉴权。
	ProtocolClaudeCode = "claude-code"
)

// IsAnthropicLike 判断是否为 Anthropic 消息结构的协议。
func IsAnthropicLike(protocol string) bool {
	return protocol == ProtocolAnthropic || protocol == ProtocolClaudeCode
}

// IsOpenAILike 判断是否为 OpenAI 兼容协议族。
//
// Azure、千帆、百炼、Ollama 这些厂商说的就是 OpenAI 协议，只是端点、
// 鉴权或字段命名不同，统一按 openai 处理，差异交给 vendor 档案表。
func IsOpenAILike(protocol string) bool {
	switch protocol {
	case ProtocolOpenAI, "":
		return true
	default:
		return false
	}
}

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
	// Reasoning 思维链正文（OpenAI 兼容生态的 message.reasoning_content）。
	//
	// Qwen3 等思考模型在非流式响应里把推理过程放这个字段、content 留空，
	// 丢弃它会让客户端只看到一条空回复。请求方向刻意不回带上文思维链
	//（与 Anthropic 适配器对 thinking 块的处理一致），只在响应方向透传。
	Reasoning string `json:"reasoning_content,omitempty"`
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
	// RevisedPrompt 上游改写后的提示词（图片生成接口返回）。
	RevisedPrompt string `json:"revised_prompt,omitempty"`
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
	// Passthrough 透传开关：开启后能力表之外的参数不再丢弃，
	// 而是原样写入上游请求体，由上游决定是否接受——不支持时上游报错并
	// 原样返回给调用方。三级取值：nil 跟随全局配置，true/false 显式覆盖。
	Passthrough *bool `json:"passthrough,omitempty"`
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

// OutputText 取模型产出的正文，content 为空时回退到思维链。
//
// Qwen3 等思考模型会把全部输出写进 reasoning_content 而 content 留空。
// 对外转发必须如实透传（客户端自己决定要不要展示思考过程），
// 但网关内部「调模型拿文本」的场景（脚本创作等）拿不到正文就等同于调用失败，
// 因此这里显式回退，避免思考模型一律不可用。
func (r *UnifiedResponse) OutputText() string {
	if r == nil {
		return ""
	}
	if text := strings.TrimSpace(r.Text()); text != "" {
		return r.Text()
	}
	for _, choice := range r.Choices {
		if reasoning := strings.TrimSpace(choice.Message.Reasoning); reasoning != "" {
			return reasoning
		}
	}
	return ""
}

// ---------------------------------------------------------------- 图片生成

// ImageRequest 统一的文生图请求，字段对齐 OpenAI /v1/images/generations。
// 该接口是同步一次性返回，没有流式形态，因此不与 UnifiedRequest 合并。
type ImageRequest struct {
	// Model 对外暴露的模型名。
	Model string `json:"model"`
	// Prompt 提示词。
	Prompt string `json:"prompt"`
	// NegativePrompt 负向提示词，部分上游（SD 系、通义万相等）支持。
	NegativePrompt string `json:"negative_prompt,omitempty"`
	// N 生成张数。
	N int `json:"n,omitempty"`
	// Size 尺寸，如 1024x1024。
	Size string `json:"size,omitempty"`
	// Quality 质量档位：standard / hd，或 gpt-image 的 low / medium / high。
	Quality string `json:"quality,omitempty"`
	// Style 风格，仅 DALL·E 3 支持。
	Style string `json:"style,omitempty"`
	// ResponseFormat 返回格式：url 或 b64_json；为空时由上游决定。
	ResponseFormat string `json:"response_format,omitempty"`
	// User 终端用户标识，用于上游风控。
	User string `json:"user,omitempty"`
	// Extra 上游扩展参数（反向提示词、水印开关、seed 等），原样透传。
	Extra map[string]interface{} `json:"-"`
}

// ImageResponse 统一的文生图结果。
type ImageResponse struct {
	// Created 生成时间（Unix 秒）。
	Created int64 `json:"created"`
	// Model 实际使用的模型名。
	Model string `json:"model,omitempty"`
	// Images 生成的图片列表。
	Images []Image `json:"images"`
	// Usage token 用量，仅部分上游上报。
	Usage *Usage `json:"usage,omitempty"`
}

// Delta 流式增量内容。
type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Reasoning 思维链增量（delta.reasoning_content），见 Message.Reasoning。
	Reasoning string `json:"reasoning_content,omitempty"`
	// Images 流式场景下的图片增量（通常出现在最后一个内容块）。
	Images []Image `json:"images,omitempty"`
}

// HasContent 判断增量块是否携带任何有效内容。
func (d Delta) HasContent() bool {
	return d.Role != "" || d.Content != "" || d.Reasoning != "" || len(d.ToolCalls) > 0 || len(d.Images) > 0
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
