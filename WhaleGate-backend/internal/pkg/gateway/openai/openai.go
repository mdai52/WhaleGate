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
	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
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
	// ReasoningContent 思维链正文：Qwen3 等思考模型把推理过程放这里、content 留空。
	// 深度求索 / 通义 / 智谱 / 硅基流动等国内厂商与 vLLM 都用它。
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// Reasoning OpenRouter / Groq 使用的同义字段。
	Reasoning string `json:"reasoning,omitempty"`
	// Thinking Ollama 与部分本地推理栈使用的同义字段。
	Thinking string      `json:"thinking,omitempty"`
	Images   []ImagePart `json:"images,omitempty"`
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
	// InputTokens / OutputTokens 通义百炼、部分聚合平台的同义字段。
	// 与 prompt/completion 二选一出现，解析时按「谁非零取谁」归一。
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
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
	return BuildRequestFor(u, nil)
}

// BuildRequestFor 按厂商档案构造请求体。
//
// 两处细节各家并不一致：
//   - 输出上限字段名：OpenAI 的 o 系列与 gpt-5 只认 max_completion_tokens；
//   - stream_options：部分平台不认这个字段，下发即 400，只能不下发、改用估算计量。
func BuildRequestFor(u *spec.UnifiedRequest, v *vendor.Vendor) (map[string]interface{}, error) {
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
	if u.MaxTokens != nil && *u.MaxTokens > 0 {
		body[v.MaxTokensField()] = *u.MaxTokens
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
	// 上游不认 stream_options 时不能下发：宁可少一份精确 usage，也不要一次 400。
	if u.Stream && v.SupportsStreamUsage() {
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
	return ParseResponseFor(raw, nil)
}

// ParseResponseFor 按厂商档案解析非流式响应：
// 思维链字段名与 usage 口径各家不同，统一在这里归一。
//
// 千帆这类厂商同一个渠道可能同时挂着新版（choices[]）与老版（result）模型，
// 因此标准解析拿不到候选时要按档案回退到老版字段。
func ParseResponseFor(raw []byte, v *vendor.Vendor) (*spec.UnifiedResponse, error) {
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
			Message:      toUnifiedMessageFor(c.Message, v),
			FinishReason: c.FinishReason,
		})
	}
	if resp.Usage != nil {
		out.Usage = convertUsage(resp.Usage)
	}
	// 老版响应同样带 usage，只检查「有没有 choices」不够，
	// 否则会返回一个带用量却没有正文的空响应。
	if len(out.Choices) == 0 {
		if v != nil && v.ResponseStyle() == vendor.ResponseLegacy {
			if legacy := parseLegacyResponse(raw); legacy != nil {
				return legacy, nil
			}
		}
		if out.Usage == nil {
			return nil, fmt.Errorf("上游响应缺少 choices 与 usage")
		}
		return nil, fmt.Errorf("上游响应缺少 choices")
	}
	return out, nil
}

// legacyResponse 老版响应结构：正文在 result，错误在 error_code/error_msg。
//
// 百度千帆的 wenxinworkshop 系列端点就是这种形态，
// 与 v2 的 OpenAI 兼容端点共用同一个密钥，因此同一个渠道可能两种都遇到。
type legacyResponse struct {
	ID               string `json:"id"`
	Object           string `json:"object"`
	Created          int64  `json:"created"`
	Result           string `json:"result"`
	IsTruncated      bool   `json:"is_truncated"`
	FinishReason     string `json:"finish_reason"`
	NeedClearHistory bool   `json:"need_clear_history"`
	Usage            *Usage `json:"usage"`
	ErrorCode        int    `json:"error_code"`
	ErrorMsg         string `json:"error_msg"`
}

// parseLegacyResponse 解析老版响应，解析不出内容时返回 nil 交由调用方报错。
func parseLegacyResponse(raw []byte) *spec.UnifiedResponse {
	var resp legacyResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	if resp.ErrorCode != 0 || resp.ErrorMsg != "" {
		return nil
	}
	if strings.TrimSpace(resp.Result) == "" {
		return nil
	}
	out := &spec.UnifiedResponse{
		ID:      resp.ID,
		Object:  firstNonEmpty(resp.Object, ObjectChatCompletion),
		Created: resp.Created,
		Choices: []spec.Choice{{
			Index:        0,
			Message:      spec.Message{Role: spec.RoleAssistant, Content: resp.Result},
			FinishReason: legacyFinishReason(resp.FinishReason, resp.IsTruncated),
		}},
	}
	if resp.Usage != nil {
		out.Usage = convertUsage(resp.Usage)
	}
	return out
}

// legacyFinishReason 老版的 normal / stop 都对应 stop；
// 被截断时上游常常不给 finish_reason，只置 is_truncated。
func legacyFinishReason(reason string, truncated bool) *string {
	if truncated {
		return spec.FinishReasonPtr(spec.FinishLength)
	}
	switch strings.TrimSpace(reason) {
	case "", "normal", "stop":
		return spec.FinishReasonPtr(spec.FinishStop)
	case "length":
		return spec.FinishReasonPtr(spec.FinishLength)
	case "function_call", "tool_calls":
		return spec.FinishReasonPtr(spec.FinishToolCalls)
	default:
		return spec.FinishReasonPtr(reason)
	}
}

// convertUsage 归一 usage，并抽取 completion_tokens_details.reasoning_tokens。
//
// input_tokens / output_tokens 是百炼与部分聚合平台的命名，
// 与 prompt/completion 语义等价，按「谁非零取谁」合并。
func convertUsage(u *Usage) *spec.Usage {
	if u == nil {
		return nil
	}
	out := &spec.Usage{
		PromptTokens:     firstNonZero(u.PromptTokens, u.InputTokens),
		CompletionTokens: firstNonZero(u.CompletionTokens, u.OutputTokens),
		TotalTokens:      u.TotalTokens,
	}
	if u.CompletionTokensDetails != nil {
		out.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	out.Normalize()
	return out
}

func toUnifiedMessage(m Message) spec.Message {
	return toUnifiedMessageFor(m, nil)
}

// toUnifiedMessageFor 按厂商档案取思维链正文。
//
// 同一个「思考过程」在生态里有三种字段名，取错就只剩一条空回复；
// 按厂商命名字段优先、其余作为兜底，可以在厂商换字段名时仍然可用。
func toUnifiedMessageFor(m Message, v *vendor.Vendor) spec.Message {
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
		Reasoning:  reasoningOf(v, m),
		Images:     images,
	}
}

// reasoningOf 按厂商偏好的字段名取思维链，其余命名作为兜底。
func reasoningOf(v *vendor.Vendor, m Message) string {
	switch v.ReasoningField() {
	case vendor.ReasoningPlain:
		return firstNonEmpty(m.Reasoning, m.ReasoningContent, m.Thinking)
	case vendor.ReasoningThinking:
		return firstNonEmpty(m.Thinking, m.ReasoningContent, m.Reasoning)
	default:
		return firstNonEmpty(m.ReasoningContent, m.Reasoning, m.Thinking)
	}
}

// firstNonEmpty 返回第一个非空字符串；思考模型的思维链字段有多种命名，
// 上游用哪个取决于其推理解析器版本。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstNonZero 返回第一个非零值，用于合并等价但命名不同的 usage 字段。
func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
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
		// 思维链透传：Qwen3 等思考模型的正文在 reasoning_content 里，
		// content 可能为空，丢掉它客户端只能看到一条空回复。
		if c.Message.Reasoning != "" {
			msg["reasoning_content"] = c.Message.Reasoning
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
	return ParseStreamEventFor(payload, nil)
}

// ParseStreamEventFor 按厂商档案解析流式事件（思维链字段名随厂商变化）。
func ParseStreamEventFor(payload []byte, v *vendor.Vendor) (*spec.StreamChunk, error) {
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
	for _, c := range chunk.Choices {
		out.Choices = append(out.Choices, spec.StreamChoice{
			Index: c.Index,
			Delta: spec.Delta{
				Role:      c.Delta.Role,
				Content:   contentToString(c.Delta.Content),
				ToolCalls: convertToolCalls(c.Delta.ToolCalls),
				Reasoning: reasoningOf(v, c.Delta),
				Images:    convertImageParts(c.Delta.Images),
			},
			FinishReason: c.FinishReason,
		})
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
		if c.Delta.Reasoning != "" {
			delta["reasoning_content"] = c.Delta.Reasoning
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
	return ExtractErrorFor(raw, nil)
}

// ExtractErrorFor 按厂商风格提取错误信息。
//
// 错误文案是排障的唯一线索，取不到时用户只能看到「上游返回 400」。
// 兼容生态至少有四种外壳，这里按厂商声明的顺序逐个试探。
func ExtractErrorFor(raw []byte, v *vendor.Vendor) string {
	style := v.ErrorStyle()
	// OpenAI 标准外壳：{"error":{"message":..}}。绝大多数厂商沿用，先试它。
	var resp Response
	if err := json.Unmarshal(raw, &resp); err == nil && resp.Error != nil && resp.Error.Message != "" {
		return resp.Error.Message
	}
	var single struct {
		Error *Error `json:"error"`
	}
	if err := json.Unmarshal(raw, &single); err == nil && single.Error != nil && single.Error.Message != "" {
		return single.Error.Message
	}
	switch style {
	case vendor.ErrorBaidu:
		// 文心：{"error_code":336000,"error_msg":"..."}
		var baidu struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		}
		if err := json.Unmarshal(raw, &baidu); err == nil && baidu.ErrorMsg != "" {
			if baidu.ErrorCode != 0 {
				return fmt.Sprintf("%d: %s", baidu.ErrorCode, baidu.ErrorMsg)
			}
			return baidu.ErrorMsg
		}
	case vendor.ErrorDashScope, vendor.ErrorXfyun:
		// 百炼 / 讯飞：顶层 {"code":..,"message":..}
		var coded struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(raw, &coded); err == nil && coded.Message != "" {
			if coded.Code != "" {
				return coded.Code + ": " + coded.Message
			}
			return coded.Message
		}
	}
	// 最后兜底：顶层 message / msg 字段（部分聚合平台）。
	var flat struct {
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &flat); err == nil {
		if flat.Message != "" {
			return flat.Message
		}
		if flat.Msg != "" {
			return flat.Msg
		}
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
