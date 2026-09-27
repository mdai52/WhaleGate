// Package openai 实现 OpenAI chat/completions 协议与内部统一格式之间的双向转换。
//
// 该包不依赖任何网络或数据库，可独立单测。
package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// ObjectChatCompletion 非流式响应对象名。
const ObjectChatCompletion = "chat.completion"

// ObjectChatCompletionChunk 流式响应对象名。
const ObjectChatCompletionChunk = "chat.completion.chunk"

// DoneSentinel 流式结束标记。
const DoneSentinel = "[DONE]"

// Request OpenAI 风格请求体。
type Request struct {
	Model               string         `json:"model"`
	Messages            []Message      `json:"messages"`
	Stream              bool           `json:"stream"`
	StreamOptions       *StreamOptions `json:"stream_options,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty"`
	MaxTokens           *int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int           `json:"max_completion_tokens,omitempty"`
	Stop                interface{}    `json:"stop,omitempty"`
	Tools               []Tool         `json:"tools,omitempty"`
	User                string         `json:"user,omitempty"`
}

// StreamOptions 流式选项。
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// Message OpenAI 风格消息。
type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content,omitempty"`
	Name       string      `json:"name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	// Images OpenRouter 风格扩展：生图结果放在 message.images[]。
	Images []ImagePart `json:"images,omitempty"`
}

// ImagePart OpenRouter 风格的图片片段。
type ImagePart struct {
	Type     string        `json:"type,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

// ImageURLPart 图片地址片段，url 常见为 data URI。
type ImageURLPart struct {
	URL string `json:"url"`
}

// ToolCall OpenAI 风格工具调用。
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Index    *int         `json:"index,omitempty"`
	Function *FunctionDef `json:"function,omitempty"`
}

// FunctionDef 函数调用内容。
type FunctionDef struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// Tool OpenAI 风格工具定义。
type Tool struct {
	Type     string        `json:"type"`
	Function *FunctionDef2 `json:"function"`
}

// FunctionDef2 工具函数定义。
type FunctionDef2 struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// Response OpenAI 风格响应体。
type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
	Error   *Error   `json:"error,omitempty"`
}

// Choice 响应候选。
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason *string `json:"finish_reason"`
}

// Usage token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// CompletionTokensDetails OpenAI 扩展：思维链 token 明细。
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// CompletionTokensDetails 完成 token 明细，reasoning_tokens 为思维链消耗。
type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
	// AudioTokens 音频消耗，暂不参与计费。
	AudioTokens int `json:"audio_tokens,omitempty"`
}

// Error OpenAI 风格错误体。
type Error struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Chunk 流式块。
type Chunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
	Error   *Error        `json:"error,omitempty"`
}

// ChunkChoice 流式候选。
type ChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Message `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// ---------------------------------------------------------------- 请求方向

// ParseRequest 把 OpenAI 请求体转换为统一请求。
func ParseRequest(raw []byte) (*spec.UnifiedRequest, error) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 请求失败: %w", err)
	}
	if req.Model == "" {
		return nil, fmt.Errorf("缺少 model 字段")
	}

	out := &spec.UnifiedRequest{
		Model:          req.Model,
		Stream:         req.Stream,
		Temperature:    req.Temperature,
		TopP:           req.TopP,
		Stop:           normalizeStop(req.Stop),
		User:           req.User,
		NativeProtocol: spec.ProtocolOpenAI,
	}
	out.MaxTokens = req.MaxTokens
	if out.MaxTokens == nil {
		out.MaxTokens = req.MaxCompletionTokens
	}
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, spec.Message{
			Role:       m.Role,
			Content:    contentToString(m.Content),
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
			ToolCalls:  convertToolCalls(m.ToolCalls),
		})
	}
	for _, t := range req.Tools {
		if t.Function == nil {
			continue
		}
		out.Tools = append(out.Tools, spec.ToolDefinition{
			Type:        t.Type,
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		})
	}
	if out.MaxTokens != nil && *out.MaxTokens <= 0 {
		out.MaxTokens = nil
	}
	out.Extra = extractExtraParams(raw, openAIKnownFields)
	// extra_body 是 OpenAI SDK 的显式自定义参数命名空间，合并进 Extra 后不再原样下发。
	var wrapper struct {
		ExtraBody map[string]interface{} `json:"extra_body"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil {
		for k, v := range wrapper.ExtraBody {
			if v == nil {
				continue
			}
			if out.Extra == nil {
				out.Extra = map[string]interface{}{}
			}
			out.Extra[k] = v
		}
	}
	if len(out.Extra) == 0 {
		out.Extra = nil
	}
	return out, nil
}

// openAIKnownFields OpenAI 协议已定义且本网关单独处理的顶层字段，其余视为用户自定义参数。
// 注意：seed / n / frequency_penalty / presence_penalty / response_format 故意不在此列，
// 它们需要走统一的参数能力协商（不同上游支持情况不同）。
var openAIKnownFields = map[string]struct{}{
	"model": {}, "messages": {}, "stream": {}, "stream_options": {},
	"temperature": {}, "top_p": {}, "max_tokens": {}, "max_completion_tokens": {},
	"stop": {}, "tools": {}, "tool_choice": {}, "user": {},
	"logprobs": {}, "top_logprobs": {}, "logit_bias": {}, "parallel_tool_calls": {},
	"service_tier": {}, "modalities": {}, "prediction": {}, "audio": {}, "reasoning_effort": {},
	"extra_body": {},
}

// extractExtraParams 收集请求体中协议未定义的顶层字段。
func extractExtraParams(raw []byte, known map[string]struct{}) map[string]interface{} {
	var generic map[string]interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil
	}
	extra := make(map[string]interface{})
	for k, v := range generic {
		if _, ok := known[k]; ok {
			continue
		}
		if v == nil {
			continue
		}
		extra[k] = v
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// BuildRequest 把统一请求转换为发往 OpenAI 上游的请求体。
func BuildRequest(u *spec.UnifiedRequest) (map[string]interface{}, error) {
	if u == nil {
		return nil, fmt.Errorf("请求为空")
	}
	body := map[string]interface{}{
		"model":    u.Model,
		"messages": buildMessages(u.Messages),
		"stream":   u.Stream,
	}
	if len(body["messages"].([]map[string]interface{})) == 0 {
		return nil, fmt.Errorf("messages 不能为空")
	}
	if u.Temperature != nil {
		body["temperature"] = *u.Temperature
	}
	if u.TopP != nil {
		body["top_p"] = *u.TopP
	}
	if u.MaxTokens != nil {
		body["max_tokens"] = *u.MaxTokens
	}
	if len(u.Stop) > 0 {
		body["stop"] = u.Stop
	}
	if u.User != "" {
		body["user"] = u.User
	}
	if len(u.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(u.Tools))
		for _, t := range u.Tools {
			fn := map[string]interface{}{"name": t.Name}
			if t.Description != "" {
				fn["description"] = t.Description
			}
			if len(t.Parameters) > 0 {
				fn["parameters"] = t.Parameters
			}
			tp := t.Type
			if tp == "" {
				tp = "function"
			}
			tools = append(tools, map[string]interface{}{"type": tp, "function": fn})
		}
		body["tools"] = tools
	}
	if u.Stream {
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}
	return body, nil
}

func buildMessages(messages []spec.Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		item := map[string]interface{}{"role": m.Role, "content": m.Content}
		if m.Name != "" {
			item["name"] = m.Name
		}
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]interface{}, 0, len(m.ToolCalls))
			for _, c := range m.ToolCalls {
				call := map[string]interface{}{
					"id":   c.ID,
					"type": c.Type,
					"function": map[string]interface{}{
						"name":      c.Name,
						"arguments": c.Arguments,
					},
				}
				if call["type"] == "" {
					call["type"] = "function"
				}
				calls = append(calls, call)
			}
			item["tool_calls"] = calls
		}
		if len(m.Images) > 0 {
			parts := make([]map[string]interface{}, 0, len(m.Images))
			for _, img := range m.Images {
				if img.IsEmpty() {
					continue
				}
				parts = append(parts, map[string]interface{}{
					"type":      "image_url",
					"image_url": map[string]interface{}{"url": img.DataURI()},
				})
			}
			if len(parts) > 0 {
				item["content"] = parts
			}
		}
		out = append(out, item)
	}
	return out
}

// ---------------------------------------------------------------- 响应方向

// ParseResponse 解析 OpenAI 非流式响应。
func ParseResponse(raw []byte) (*spec.UnifiedResponse, error) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 响应失败: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("上游返回错误: %s", resp.Error.Message)
	}

	out := &spec.UnifiedResponse{
		ID:      resp.ID,
		Object:  ObjectChatCompletion,
		Model:   resp.Model,
		Created: resp.Created,
	}
	for _, c := range resp.Choices {
		out.Choices = append(out.Choices, spec.Choice{
			Index:        c.Index,
			Message:      toUnifiedMessage(c.Message),
			FinishReason: c.FinishReason,
		})
	}
	if resp.Usage != nil {
		out.Usage = convertUsage(resp.Usage)
	}
	if len(out.Choices) == 0 && out.Usage == nil {
		return nil, fmt.Errorf("上游响应缺少 choices 与 usage")
	}
	return out, nil
}

// convertUsage 归一 usage，并抽取 completion_tokens_details.reasoning_tokens。
func convertUsage(u *Usage) *spec.Usage {
	if u == nil {
		return nil
	}
	out := &spec.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
	if u.CompletionTokensDetails != nil {
		out.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	out.Normalize()
	return out
}

func toUnifiedMessage(m Message) spec.Message {
	images := convertImageParts(m.Images)
	images = append(images, contentToImages(m.Content)...)
	if len(images) == 0 {
		images = nil
	}
	return spec.Message{
		Role:       m.Role,
		Content:    contentToString(m.Content),
		Name:       m.Name,
		ToolCallID: m.ToolCallID,
		ToolCalls:  convertToolCalls(m.ToolCalls),
		Images:     images,
	}
}

// convertImageParts 把 OpenRouter 扩展的 message.images[] 归一为统一图片结构。
func convertImageParts(parts []ImagePart) []spec.Image {
	if len(parts) == 0 {
		return nil
	}
	out := make([]spec.Image, 0, len(parts))
	for _, p := range parts {
		if p.ImageURL == nil || p.ImageURL.URL == "" {
			continue
		}
		if img, ok := spec.ParseDataURI(p.ImageURL.URL); ok {
			out = append(out, img)
			continue
		}
		out = append(out, spec.Image{Type: "image_url", URL: p.ImageURL.URL})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// contentToImages 从多模态 content 片段中提取图片（输入侧归一）。
func contentToImages(v interface{}) []spec.Image {
	parts, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []spec.Image
	for _, part := range parts {
		m, ok := part.(map[string]interface{})
		if !ok {
			continue
		}
		inner, ok := m["image_url"].(map[string]interface{})
		if !ok {
			continue
		}
		url, _ := inner["url"].(string)
		if url == "" {
			continue
		}
		if img, parsed := spec.ParseDataURI(url); parsed {
			out = append(out, img)
			continue
		}
		out = append(out, spec.Image{Type: "image_url", URL: url})
	}
	return out
}

// buildImageParts 把统一图片还原为 OpenRouter 风格的 message.images[]。
func buildImageParts(images []spec.Image) []ImagePart {
	if len(images) == 0 {
		return nil
	}
	out := make([]ImagePart, 0, len(images))
	for _, img := range images {
		if img.IsEmpty() {
			continue
		}
		part := ImagePart{Type: "image_url", ImageURL: &ImageURLPart{URL: img.DataURI()}}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildResponse 把统一响应序列化为 OpenAI 响应体。
func BuildResponse(u *spec.UnifiedResponse) map[string]interface{} {
	if u == nil {
		return ErrorResponse("响应为空", "internal_error")
	}
	choices := make([]map[string]interface{}, 0, len(u.Choices))
	for _, c := range u.Choices {
		msg := map[string]interface{}{"role": c.Message.Role, "content": c.Message.Content}
		if c.Message.Name != "" {
			msg["name"] = c.Message.Name
		}
		if len(c.Message.ToolCalls) > 0 {
			calls := make([]map[string]interface{}, 0, len(c.Message.ToolCalls))
			for _, call := range c.Message.ToolCalls {
				calls = append(calls, map[string]interface{}{
					"id":   call.ID,
					"type": call.Type,
					"function": map[string]interface{}{
						"name":      call.Name,
						"arguments": call.Arguments,
					},
				})
			}
			msg["tool_calls"] = calls
		}
		// OpenAI 兼容客户端习惯从 message.images[] 读取生图结果，原样透传扩展字段。
		if parts := buildImageParts(c.Message.Images); len(parts) > 0 {
			msg["images"] = parts
		}
		choices = append(choices, map[string]interface{}{
			"index":         c.Index,
			"message":       msg,
			"finish_reason": c.FinishReason,
		})
	}
	body := map[string]interface{}{
		"id":      u.ID,
		"object":  ObjectChatCompletion,
		"created": u.Created,
		"model":   u.Model,
		"choices": choices,
	}
	if u.Usage != nil {
		body["usage"] = buildUsage(u.Usage)
	}
	return body
}

// buildUsage 构造 OpenAI usage 体，思维链计入 completion_tokens_details.reasoning_tokens。
func buildUsage(u *spec.Usage) map[string]interface{} {
	if u == nil {
		return nil
	}
	out := map[string]interface{}{
		"prompt_tokens":     u.PromptTokens,
		"completion_tokens": u.CompletionTokens,
		"total_tokens":      u.TotalTokens,
	}
	if u.ReasoningTokens > 0 {
		out["completion_tokens_details"] = map[string]interface{}{
			"reasoning_tokens": u.ReasoningTokens,
		}
	}
	return out
}

// ---------------------------------------------------------------- 流式方向

// ParseStreamEvent 解析单个 SSE data 载荷（不含 data: 前缀）。
func ParseStreamEvent(payload []byte) (*spec.StreamChunk, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || bytes.Equal(payload, []byte(DoneSentinel)) {
		return nil, nil
	}

	var chunk Chunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return nil, fmt.Errorf("解析流式块失败: %w", err)
	}
	if chunk.Error != nil && chunk.Error.Message != "" {
		return nil, fmt.Errorf("上游流式返回错误: %s", chunk.Error.Message)
	}

	out := &spec.StreamChunk{
		ID:      chunk.ID,
		Object:  ObjectChatCompletionChunk,
		Model:   chunk.Model,
		Created: chunk.Created,
	}
	for i, c := range chunk.Choices {
		index := c.Index
		out.Choices = append(out.Choices, spec.StreamChoice{
			Index: index,
			Delta: spec.Delta{
				Role:      c.Delta.Role,
				Content:   contentToString(c.Delta.Content),
				ToolCalls: convertToolCalls(c.Delta.ToolCalls),
				Images:    convertImageParts(c.Delta.Images),
			},
			FinishReason: c.FinishReason,
		})
		_ = i
	}
	if chunk.Usage != nil {
		out.Usage = convertUsage(chunk.Usage)
	}
	return out, nil
}

// BuildStreamEvent 把统一流式块编码为一行 SSE（含 data: 前缀与空行）。
func BuildStreamEvent(u *spec.StreamChunk) ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("流式块为空")
	}
	choices := make([]map[string]interface{}, 0, len(u.Choices))
	for _, c := range u.Choices {
		delta := map[string]interface{}{}
		if c.Delta.Role != "" {
			delta["role"] = c.Delta.Role
		}
		if c.Delta.Content != "" {
			delta["content"] = c.Delta.Content
		}
		if len(c.Delta.ToolCalls) > 0 {
			calls := make([]map[string]interface{}, 0, len(c.Delta.ToolCalls))
			for _, call := range c.Delta.ToolCalls {
				calls = append(calls, map[string]interface{}{
					"index": len(calls),
					"id":    call.ID,
					"type":  call.Type,
					"function": map[string]interface{}{
						"name":      call.Name,
						"arguments": call.Arguments,
					},
				})
			}
			delta["tool_calls"] = calls
		}
		if parts := buildImageParts(c.Delta.Images); len(parts) > 0 {
			delta["images"] = parts
		}
		choices = append(choices, map[string]interface{}{
			"index":         c.Index,
			"delta":         delta,
			"finish_reason": c.FinishReason,
		})
	}

	payload := map[string]interface{}{
		"id":      u.ID,
		"object":  ObjectChatCompletionChunk,
		"created": u.Created,
		"model":   u.Model,
		"choices": choices,
	}
	if u.Usage != nil {
		payload["usage"] = buildUsage(u.Usage)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化流式块失败: %w", err)
	}
	return append(append([]byte("data: "), data...), '\n', '\n'), nil
}

// DoneEvent 返回流式结束标记行。
func DoneEvent() []byte {
	return []byte("data: [DONE]\n\n")
}

// ---------------------------------------------------------------- 错误

// ErrorResponse 构造 OpenAI 风格错误体。
func ErrorResponse(message, errType string) map[string]interface{} {
	return map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errType,
			"code":    errType,
		},
	}
}

// ExtractError 从上游响应体中提取错误信息。
func ExtractError(raw []byte) string {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err == nil && resp.Error != nil {
		return resp.Error.Message
	}
	var single struct {
		Error *Error `json:"error"`
	}
	if err := json.Unmarshal(raw, &single); err == nil && single.Error != nil {
		return single.Error.Message
	}
	return ""
}

// ---------------------------------------------------------------- 工具函数

func contentToString(v interface{}) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []interface{}:
		var sb strings.Builder
		for _, part := range typed {
			m, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := m["text"].(string); ok {
				sb.WriteString(text)
			}
		}
		return sb.String()
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

func convertToolCalls(calls []ToolCall) []spec.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]spec.ToolCall, 0, len(calls))
	for _, c := range calls {
		item := spec.ToolCall{ID: c.ID, Type: c.Type}
		if item.Type == "" {
			item.Type = "function"
		}
		if c.Function != nil {
			item.Name = c.Function.Name
			item.Arguments = c.Function.Arguments
		}
		out = append(out, item)
	}
	return out
}

func normalizeStop(v interface{}) []string {
	switch typed := v.(type) {
	case nil:
		return nil
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, s := range typed {
			if str, ok := s.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
