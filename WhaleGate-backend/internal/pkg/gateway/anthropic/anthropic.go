// Package anthropic 实现 Anthropic Messages API 与内部统一格式之间的双向转换。
//
// 覆盖范围：
//   - 上游侧：渠道类型 anthropic（x-api-key 鉴权）与 claude-code（Bearer 鉴权）；
//   - 请求侧：system 拆分、多模态 content 块、tool_use / tool_result 映射；
//   - 流式侧：Anthropic 的增量事件（content_block_delta / input_json_delta）需要跨包累积，
//     因此提供有状态的 StreamDecoder 供转发层按请求持有；
//   - 思维链：thinking / redacted_thinking 块不进入正文，避免污染客户端文本流。
//
// 该包不依赖网络与数据库，可独立单测。
package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// 协议交互常量。
const (
	// APIVersion 请求头 anthropic-version 的取值。
	APIVersion = "2023-06-01"
	// APIKeyHeader 原生鉴权头名。
	APIKeyHeader = "x-api-key"
	// BearerHeader Claude Code 网关常用的鉴权头名。
	BearerHeader = "Authorization"
	// MessagesPath 相对于 /v1 的资源路径。
	MessagesPath = "/messages"
	// DefaultMaxTokens Anthropic 要求必填 max_tokens，缺失时的兜底值。
	DefaultMaxTokens = 4096
)

// 内容块类型。
const (
	BlockText      = "text"
	BlockImage     = "image"
	BlockToolUse   = "tool_use"
	BlockToolResut = "tool_result"
	BlockThinking  = "thinking"
)

// 流式增量类型。
const (
	DeltaTextDelta      = "text_delta"
	DeltaThinkingDelta  = "thinking_delta"
	DeltaInputJSONDelta = "input_json_delta"
	DeltaSignatureDelta = "signature_delta"
)

// 流式事件类型。
const (
	EventMessageStart      = "message_start"
	EventMessageDelta      = "message_delta"
	EventMessageStop       = "message_stop"
	EventContentBlockStart = "content_block_start"
	EventContentBlockDelta = "content_block_delta"
	EventContentBlockStop  = "content_block_stop"
	EventPing              = "ping"
	EventError             = "error"
)

// MessagesURL 按 Anthropic 约定拼装 messages 端点。
// 用户可能填写带或不带 /v1 后缀的地址（探测阶段两种都能识别），此处统一补全。
func MessagesURL(baseURL string) string {
	return ResourceURL(baseURL, MessagesPath)
}

// ModelsURL 拼装模型列表端点，用于连通性测试。
func ModelsURL(baseURL string) string {
	return ResourceURL(baseURL, "/models")
}

// ResourceURL 把资源路径拼到 base 上：base 自带 /v1 前缀时直接追加，否则先补 /v1。
func ResourceURL(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return path
	}
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		return base + path
	}
	return base + "/v1" + path
}

// ---------------------------------------------------------------- 数据结构

// Request Anthropic 请求体。
type Request struct {
	Model         string      `json:"model"`
	Messages      []Message   `json:"messages"`
	System        interface{} `json:"system,omitempty"`
	MaxTokens     int         `json:"max_tokens"`
	Temperature   *float64    `json:"temperature,omitempty"`
	TopP          *float64    `json:"top_p,omitempty"`
	TopK          *int        `json:"top_k,omitempty"`
	StopSequences []string    `json:"stop_sequences,omitempty"`
	Stream        bool        `json:"stream,omitempty"`
	Tools         []Tool      `json:"tools,omitempty"`
	Metadata      *Metadata   `json:"metadata,omitempty"`
	Thinking      interface{} `json:"thinking,omitempty"`
}

// Metadata 上游风控用的终端用户标识。
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// Message Anthropic 消息。
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// ContentBlock Anthropic 内容块。
type ContentBlock struct {
	Type      string       `json:"type"`
	Text      string       `json:"text,omitempty"`
	Source    *ImageSource `json:"source,omitempty"`
	ID        string       `json:"id,omitempty"`
	Name      string       `json:"name,omitempty"`
	Input     interface{}  `json:"input,omitempty"`
	ToolUseID string       `json:"tool_use_id,omitempty"`
	Content   interface{}  `json:"content,omitempty"`
	IsError   bool         `json:"is_error,omitempty"`
	Thinking  string       `json:"thinking,omitempty"`
	Signature string       `json:"signature,omitempty"`
}

// ImageSource Anthropic 图片来源。
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// Tool 工具定义（Anthropic 无嵌套 function 层）。
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

// Usage token 用量。
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ErrorPayload Anthropic 错误体。
type ErrorPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Response 非流式响应体。
type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        *Usage         `json:"usage"`
	Error        *ErrorPayload  `json:"error"`
}

// ---------------------------------------------------------------- 请求方向

// BuildRequest 把统一请求转换为发往 Anthropic 上游的请求体。
func BuildRequest(u *spec.UnifiedRequest) (map[string]interface{}, error) {
	if u == nil {
		return nil, fmt.Errorf("请求为空")
	}

	var systemBlocks []string
	messages := make([]Message, 0, len(u.Messages))
	pendingToolResults := make([]ContentBlock, 0, 4)

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		blocks := make([]ContentBlock, len(pendingToolResults))
		copy(blocks, pendingToolResults)
		messages = append(messages, Message{Role: spec.RoleUser, Content: blocks})
		pendingToolResults = pendingToolResults[:0]
	}

	for _, m := range u.Messages {
		switch m.Role {
		case spec.RoleSystem:
			// Anthropic 把 system 提到顶层，且不参与 messages 交替规则。
			if text := strings.TrimSpace(m.Content); text != "" {
				systemBlocks = append(systemBlocks, text)
			}
			continue
		case spec.RoleTool:
			// OpenAI 的 role=tool 消息在 Anthropic 中是 tool_result 块，
			// 必须归属到一个 user 消息里，且允许连续多条。
			block := ContentBlock{
				Type:      BlockToolResut,
				ToolUseID: m.ToolCallID,
				Content:   toolResultContent(m),
			}
			if block.ToolUseID == "" {
				// 缺少调用 ID 时上游必然报错，降级为普通文本内容更稳妥。
				block.Type = BlockText
				block.Text = m.Content
			}
			pendingToolResults = append(pendingToolResults, block)
			continue
		}

		flushToolResults()
		role := m.Role
		if role == "developer" {
			// Anthropic 无 developer 角色，收敛到 system 语义。
			if text := strings.TrimSpace(m.Content); text != "" {
				systemBlocks = append(systemBlocks, text)
			}
			continue
		}
		if role == "" {
			role = spec.RoleUser
		}

		blocks := make([]ContentBlock, 0, 2)
		if text := strings.TrimSpace(m.Content); text != "" {
			blocks = append(blocks, ContentBlock{Type: BlockText, Text: m.Content})
		}
		blocks = append(blocks, imageBlocks(m.Images)...)
		for _, call := range m.ToolCalls {
			block := ContentBlock{
				Type:  BlockToolUse,
				ID:    call.ID,
				Name:  call.Name,
				Input: decodeJSONObject(call.Arguments),
			}
			blocks = append(blocks, block)
		}
		if len(blocks) == 0 {
			// Anthropic 拒绝空 content 数组，补一个空文本块保证结构合法。
			blocks = append(blocks, ContentBlock{Type: BlockText, Text: ""})
		}
		messages = append(messages, Message{Role: role, Content: blocks})
	}
	flushToolResults()

	if len(messages) == 0 {
		return nil, fmt.Errorf("messages 不能为空")
	}

	maxTokens := DefaultMaxTokens
	if u.MaxTokens != nil && *u.MaxTokens > 0 {
		maxTokens = *u.MaxTokens
	}

	body := map[string]interface{}{
		"model":      u.Model,
		"messages":   messages,
		"max_tokens": maxTokens,
	}
	if len(systemBlocks) == 1 {
		body["system"] = systemBlocks[0]
	} else if len(systemBlocks) > 1 {
		body["system"] = systemBlocks
	}
	if u.Temperature != nil {
		body["temperature"] = *u.Temperature
	}
	if u.TopP != nil {
		body["top_p"] = *u.TopP
	}
	if len(u.Stop) > 0 {
		body["stop_sequences"] = u.Stop
	}
	if u.Stream {
		body["stream"] = true
	}
	if u.User != "" {
		body["metadata"] = &Metadata{UserID: u.User}
	}
	if len(u.Tools) > 0 {
		tools := make([]Tool, 0, len(u.Tools))
		for _, t := range u.Tools {
			tools = append(tools, Tool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Parameters,
			})
		}
		body["tools"] = tools
	}
	return body, nil
}

// toolResultContent 构造 tool_result 的内容：优先文本，失败时给出可读错误。
func toolResultContent(m spec.Message) interface{} {
	if strings.TrimSpace(m.Content) != "" {
		return m.Content
	}
	if len(m.Images) > 0 {
		blocks := imageBlocks(m.Images)
		if len(blocks) > 0 {
			return blocks
		}
	}
	return "(empty)"
}

// imageBlocks 把统一图片转换为 Anthropic 图片块。
func imageBlocks(images []spec.Image) []ContentBlock {
	if len(images) == 0 {
		return nil
	}
	out := make([]ContentBlock, 0, len(images))
	for _, img := range images {
		if img.IsEmpty() {
			continue
		}
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		block := ContentBlock{Type: BlockImage}
		if img.B64Data != "" {
			block.Source = &ImageSource{Type: "base64", MediaType: mime, Data: img.B64Data}
		} else {
			block.Source = &ImageSource{Type: "url", URL: img.URL}
		}
		out = append(out, block)
	}
	return out
}

// decodeJSONObject 把工具参数字符串解析为对象，失败时返回空对象避免上游报错。
func decodeJSONObject(raw string) map[string]interface{} {
	if strings.TrimSpace(raw) == "" {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	return map[string]interface{}{}
}

// ---------------------------------------------------------------- 响应方向

// ParseResponse 解析 Anthropic 非流式响应。
func ParseResponse(raw []byte) (*spec.UnifiedResponse, error) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 响应失败: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("上游返回错误: %s", resp.Error.Message)
	}

	out := &spec.UnifiedResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Model:   resp.Model,
		Created: time.Now().Unix(),
	}
	if out.ID == "" {
		out.ID = "msg_" + resp.Model
	}

	var text strings.Builder
	var toolCalls []spec.ToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case BlockText:
			text.WriteString(block.Text)
		case BlockToolUse:
			toolCalls = append(toolCalls, spec.ToolCall{
				ID:        block.ID,
				Type:      "function",
				Name:      block.Name,
				Arguments: encodeJSONObject(block.Input),
			})
		case BlockThinking:
			// 思维链不进入正文
		}
	}

	finish := StopReasonToFinish(resp.StopReason)
	if len(toolCalls) > 0 && finish == spec.FinishStop {
		finish = spec.FinishToolCalls
	}
	out.Choices = append(out.Choices, spec.Choice{
		Index: 0,
		Message: spec.Message{
			Role:      spec.RoleAssistant,
			Content:   text.String(),
			ToolCalls: toolCalls,
		},
		FinishReason: &finish,
	})
	if resp.Usage != nil {
		out.Usage = convertUsage(resp.Usage)
	}
	return out, nil
}

// encodeJSONObject 把工具输入对象序列化回 JSON 字符串（统一格式约定为字符串）。
func encodeJSONObject(v interface{}) string {
	if v == nil {
		return "{}"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// convertUsage 归一 usage；缓存读写 token 计入 prompt 侧。
func convertUsage(u *Usage) *spec.Usage {
	if u == nil {
		return nil
	}
	out := &spec.Usage{
		PromptTokens:     u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		CompletionTokens: u.OutputTokens,
	}
	out.Normalize()
	return out
}

// StopReasonToFinish 把 Anthropic 停止原因映射到统一的 finish_reason。
func StopReasonToFinish(reason string) string {
	switch reason {
	case "max_tokens", "model_context_window_exceeded":
		return spec.FinishLength
	case "tool_use":
		return spec.FinishToolCalls
	case "end_turn", "stop_sequence", "pause_turn", "refusal":
		return spec.FinishStop
	default:
		return spec.FinishStop
	}
}

// ---------------------------------------------------------------- 流式方向

// blockState 记录一个内容块的累积状态。
//
// startInput 来自 content_block_start 的完整入参（部分网关一次性给全）；
// partialJSON 来自逐片下发（input_json_delta）的入参增量。
// 二者互斥，取 input_json_delta 优先；若上游同时给了两者，以增量覆盖完整值，
// 避免拼接出 "{}{...}" 这类非法 JSON。
type blockState struct {
	kind        string
	id          string
	name        string
	input       interface{}
	partialJSON strings.Builder
}

// StreamDecoder 有状态的 Anthropic 流式解码器。
//
// Anthropic 的 tool_use 参数是分片下发（input_json_delta）的，必须跨事件累积，
// 因此每个请求需要持有独立实例。
type StreamDecoder struct {
	id     string
	model  string
	blocks map[int]*blockState
}

// NewStreamDecoder 创建流式解码器。
func NewStreamDecoder() *StreamDecoder {
	return &StreamDecoder{blocks: make(map[int]*blockState, 4)}
}

// Decode 解码一个 SSE data 载荷，返回 nil 表示该事件无需下发客户端。
func (d *StreamDecoder) Decode(payload []byte) (*spec.StreamChunk, error) {
	payload = trimSpace(payload)
	if len(payload) == 0 || string(payload) == "[DONE]" {
		return nil, nil
	}

	var event struct {
		Type         string          `json:"type"`
		Index        *int            `json:"index"`
		Message      *Response       `json:"message"`
		ContentBlock *ContentBlock   `json:"content_block"`
		Delta        json.RawMessage `json:"delta"`
		Usage        *Usage          `json:"usage"`
		Error        *ErrorPayload   `json:"error"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 流式块失败: %w", err)
	}
	if event.Error != nil && event.Error.Message != "" {
		return nil, fmt.Errorf("上游流式返回错误: %s", event.Error.Message)
	}

	switch event.Type {
	case EventMessageStart:
		return d.decodeMessageStart(event.Message)
	case EventContentBlockStart:
		return d.decodeBlockStart(event.Index, event.ContentBlock)
	case EventContentBlockDelta:
		return d.decodeBlockDelta(event.Index, event.Delta)
	case EventContentBlockStop:
		return d.decodeBlockStop(event.Index)
	case EventMessageDelta:
		return d.decodeMessageDelta(event.Delta, event.Usage)
	case EventPing, EventMessageStop:
		return nil, nil
	default:
		return nil, nil
	}
}

func (d *StreamDecoder) decodeMessageStart(msg *Response) (*spec.StreamChunk, error) {
	if msg != nil {
		d.id = msg.ID
		d.model = msg.Model
	}
	chunk := &spec.StreamChunk{
		ID:      d.id,
		Object:  "chat.completion.chunk",
		Model:   d.model,
		Created: time.Now().Unix(),
		Choices: []spec.StreamChoice{{
			Index: 0,
			Delta: spec.Delta{Role: spec.RoleAssistant},
		}},
	}
	// input_tokens 只在 message_start 出现一次，在此上报避免重复累加。
	if msg != nil && msg.Usage != nil {
		chunk.Usage = convertUsage(msg.Usage)
	}
	return chunk, nil
}

func (d *StreamDecoder) decodeBlockStart(index *int, block *ContentBlock) (*spec.StreamChunk, error) {
	if index == nil || block == nil {
		return nil, nil
	}
	state := &blockState{kind: block.Type, id: block.ID, name: block.Name}
	if block.Type == BlockToolUse {
		state.input = block.Input
	}
	d.blocks[*index] = state
	return nil, nil
}

func (d *StreamDecoder) decodeBlockDelta(index *int, raw json.RawMessage) (*spec.StreamChunk, error) {
	if len(raw) == 0 || index == nil {
		return nil, nil
	}
	var delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
	}
	if err := json.Unmarshal(raw, &delta); err != nil {
		return nil, nil
	}

	switch delta.Type {
	case DeltaTextDelta:
		if delta.Text == "" {
			return nil, nil
		}
		return d.chunk(spec.Delta{Content: delta.Text}), nil
	case DeltaInputJSONDelta:
		// 工具参数是分片 JSON，累积到 content_block_stop 再整体下发。
		state, ok := d.blocks[*index]
		if !ok {
			state = &blockState{kind: BlockToolUse}
			d.blocks[*index] = state
		}
		state.partialJSON.WriteString(delta.PartialJSON)
		return nil, nil
	case DeltaThinkingDelta, DeltaSignatureDelta:
		// 思维链分片不下发到客户端正文
		return nil, nil
	default:
		if delta.Text != "" {
			return d.chunk(spec.Delta{Content: delta.Text}), nil
		}
		return nil, nil
	}
}

func (d *StreamDecoder) decodeBlockStop(index *int) (*spec.StreamChunk, error) {
	if index == nil {
		return nil, nil
	}
	state, ok := d.blocks[*index]
	if !ok {
		return nil, nil
	}
	delete(d.blocks, *index)

	if state.kind != BlockToolUse {
		return nil, nil
	}
	// input_json_delta 优先于 content_block_start 里的一次性入参。
	args := strings.TrimSpace(state.partialJSON.String())
	if args == "" {
		args = encodeJSONObject(state.input)
	}
	if args == "" || args == "null" {
		args = "{}"
	}
	return d.chunk(spec.Delta{ToolCalls: []spec.ToolCall{{
		ID:        state.id,
		Type:      "function",
		Name:      state.name,
		Arguments: args,
	}}}), nil
}

func (d *StreamDecoder) decodeMessageDelta(raw json.RawMessage, usage *Usage) (*spec.StreamChunk, error) {
	var stopReason string
	if len(raw) > 0 {
		var delta struct {
			StopReason   string  `json:"stop_reason"`
			StopSequence *string `json:"stop_sequence"`
		}
		if err := json.Unmarshal(raw, &delta); err == nil {
			stopReason = delta.StopReason
		}
	}
	finish := StopReasonToFinish(stopReason)

	chunk := &spec.StreamChunk{
		ID:      d.id,
		Object:  "chat.completion.chunk",
		Model:   d.model,
		Created: time.Now().Unix(),
		Choices: []spec.StreamChoice{{
			Index:        0,
			FinishReason: &finish,
		}},
	}
	// output_tokens 在 message_delta 中以累计值出现一次，此处作为 completion 上报。
	if usage != nil {
		chunk.Usage = &spec.Usage{
			PromptTokens:     usage.CacheCreationInputTokens + usage.CacheReadInputTokens,
			CompletionTokens: usage.OutputTokens,
		}
		chunk.Usage.Normalize()
	}
	return chunk, nil
}

func (d *StreamDecoder) chunk(delta spec.Delta) *spec.StreamChunk {
	return &spec.StreamChunk{
		ID:      d.id,
		Object:  "chat.completion.chunk",
		Model:   d.model,
		Created: time.Now().Unix(),
		Choices: []spec.StreamChoice{{Index: 0, Delta: delta}},
	}
}

// ParseStreamEvent 解析单个流式事件。
// 注意：Anthropic 的工具调用增量跨多个事件，需要按请求持有 StreamDecoder；
// 本函数仅供一次性调用场景使用，工具参数可能不完整。
func ParseStreamEvent(payload []byte) (*spec.StreamChunk, error) {
	return NewStreamDecoder().Decode(payload)
}

// ---------------------------------------------------------------- 错误

// ExtractError 从上游响应体中提取错误信息。
func ExtractError(raw []byte) string {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err == nil && resp.Error != nil {
		return resp.Error.Message
	}
	var single struct {
		Type  string        `json:"type"`
		Error *ErrorPayload `json:"error"`
	}
	if err := json.Unmarshal(raw, &single); err == nil && single.Error != nil {
		return single.Error.Message
	}
	return ""
}

// ErrorResponse 构造 Anthropic 风格错误体。
func ErrorResponse(message, errType string) map[string]interface{} {
	return map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	}
}

// ---------------------------------------------------------------- 工具

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}
