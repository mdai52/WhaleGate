// Package gemini 实现 Google Gemini 原生协议（generateContent / streamGenerateContent）
// 与鲸闸内部统一格式之间的双向转换。
//
// 该包不依赖任何网络或数据库，可独立单测。
package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// 动作名。
const (
	ActionGenerateContent       = "generateContent"
	ActionStreamGenerateContent = "streamGenerateContent"
)

// 角色常量。
const (
	RoleUser  = "user"
	RoleModel = "model"
)

// 结束原因映射。
const (
	FinishStop      = "STOP"
	FinishMaxTokens = "MAX_TOKENS"
	FinishSafety    = "SAFETY"
	FinishOther     = "OTHER"
)

// Request Gemini generateContent 请求体。
type Request struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	Tools             []Tool            `json:"tools,omitempty"`
	SafetySettings    []SafetySetting   `json:"safetySettings,omitempty"`
}

// Content 一段对话内容。
type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

// Part 内容片段。
type Part struct {
	Text         string                 `json:"text,omitempty"`
	FunctionCall *FunctionCall          `json:"functionCall,omitempty"`
	FunctionResp map[string]interface{} `json:"functionResponse,omitempty"`
	// InlineData Gemini 内联二进制（生图结果存放处）。
	InlineData *InlineDataPart `json:"inlineData,omitempty"`
}

// InlineDataPart Gemini inlineData 片段。
type InlineDataPart struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// FunctionCall 函数调用。
type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// GenerationConfig 生成参数。
type GenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
	CandidateCount  *int     `json:"candidateCount,omitempty"`
}

// Tool 工具声明集合。
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// FunctionDeclaration 函数声明。
type FunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// SafetySetting 安全设置。
type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// Response Gemini 响应体。
type Response struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
	Error         *Error         `json:"error,omitempty"`
}

// Candidate 响应候选。
type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
	Index        int     `json:"index"`
}

// UsageMetadata token 用量。
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	// ThoughtsTokenCount 思维链消耗，等价 OpenAI 的 reasoning_tokens。
	ThoughtsTokenCount int `json:"thoughtsTokenCount,omitempty"`
}

// Error Gemini 错误体。
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ---------------------------------------------------------------- 请求方向

// ParseRequest 把 Gemini 请求体转换为统一请求，model 来自 URL 路径。
func ParseRequest(raw []byte, model string) (*spec.UnifiedRequest, error) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("解析 Gemini 请求失败: %w", err)
	}
	if model == "" {
		return nil, fmt.Errorf("缺少模型名")
	}

	out := &spec.UnifiedRequest{
		Model:          model,
		NativeProtocol: spec.ProtocolGemini,
	}
	if req.SystemInstruction != nil {
		out.Messages = append(out.Messages, spec.Message{
			Role:    spec.RoleSystem,
			Content: partsToText(req.SystemInstruction.Parts),
		})
	}
	for _, c := range req.Contents {
		role := spec.RoleUser
		if c.Role == RoleModel {
			role = spec.RoleAssistant
		}
		out.Messages = append(out.Messages, spec.Message{
			Role:      role,
			Content:   partsToText(c.Parts),
			ToolCalls: partsToToolCalls(c.Parts),
			Images:    partsToImages(c.Parts),
		})
	}
	if len(out.Messages) == 0 {
		return nil, fmt.Errorf("contents 不能为空")
	}

	if cfg := req.GenerationConfig; cfg != nil {
		out.Temperature = cfg.Temperature
		out.TopP = cfg.TopP
		out.MaxTokens = cfg.MaxOutputTokens
		out.Stop = cfg.StopSequences
	}
	for _, t := range req.Tools {
		for _, fn := range t.FunctionDeclarations {
			out.Tools = append(out.Tools, spec.ToolDefinition{
				Type:        "function",
				Name:        fn.Name,
				Description: fn.Description,
				Parameters:  fn.Parameters,
			})
		}
	}
	out.Extra = extractGeminiExtra(raw)
	return out, nil
}

// geminiKnownConfigFields generationConfig 中由本网关单独处理的字段。
// 其余（topK / seed / candidateCount / responseMimeType 等）交给统一的参数能力协商，
// 以便跨协议转发时按需映射到目标协议。
var geminiKnownConfigFields = map[string]struct{}{
	"responseSchema": {}, "thinkingConfig": {},
}

// extractGeminiExtra 收集 generationConfig 中未标准化的字段作为自定义参数。
func extractGeminiExtra(raw []byte) map[string]interface{} {
	var generic struct {
		GenerationConfig map[string]interface{} `json:"generationConfig"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil
	}
	extra := make(map[string]interface{})
	for k, v := range generic.GenerationConfig {
		if _, ok := geminiKnownConfigFields[k]; ok {
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

// BuildRequest 把统一请求转换为发往 Gemini 上游的请求体。
func BuildRequest(u *spec.UnifiedRequest) (map[string]interface{}, error) {
	if u == nil {
		return nil, fmt.Errorf("请求为空")
	}
	contents := make([]map[string]interface{}, 0, len(u.Messages))
	body := map[string]interface{}{}

	for _, m := range u.Messages {
		switch m.Role {
		case spec.RoleSystem:
			body["systemInstruction"] = map[string]interface{}{
				"parts": []map[string]interface{}{{"text": m.Content}},
			}
		default:
			role := RoleUser
			if m.Role == spec.RoleAssistant || m.Role == spec.RoleTool {
				role = RoleModel
			}
			parts := []map[string]interface{}{{"text": m.Content}}
			for _, call := range m.ToolCalls {
				part := map[string]interface{}{
					"functionCall": map[string]interface{}{
						"name": call.Name,
						"args": decodeArgs(call.Arguments),
					},
				}
				parts = append(parts, part)
			}
			parts = append(parts, buildInlineParts(m.Images)...)
			contents = append(contents, map[string]interface{}{"role": role, "parts": parts})
		}
	}
	if len(contents) == 0 {
		return nil, fmt.Errorf("contents 不能为空")
	}
	body["contents"] = contents

	genCfg := map[string]interface{}{}
	if u.Temperature != nil {
		genCfg["temperature"] = *u.Temperature
	}
	if u.TopP != nil {
		genCfg["topP"] = *u.TopP
	}
	if u.MaxTokens != nil {
		genCfg["maxOutputTokens"] = *u.MaxTokens
	}
	if len(u.Stop) > 0 {
		genCfg["stopSequences"] = u.Stop
	}
	if len(genCfg) > 0 {
		body["generationConfig"] = genCfg
	}

	if len(u.Tools) > 0 {
		decls := make([]map[string]interface{}, 0, len(u.Tools))
		for _, t := range u.Tools {
			decl := map[string]interface{}{"name": t.Name}
			if t.Description != "" {
				decl["description"] = t.Description
			}
			if len(t.Parameters) > 0 {
				decl["parameters"] = t.Parameters
			}
			decls = append(decls, decl)
		}
		body["tools"] = []map[string]interface{}{{"functionDeclarations": decls}}
	}
	return body, nil
}

// ---------------------------------------------------------------- 响应方向

// ParseResponse 解析 Gemini 非流式响应。
func ParseResponse(raw []byte) (*spec.UnifiedResponse, error) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("解析 Gemini 响应失败: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("上游返回错误: %s", resp.Error.Message)
	}

	out := &spec.UnifiedResponse{
		ID:     "gemini-" + resp.ModelVersion,
		Object: ActionGenerateContent,
		Model:  resp.ModelVersion,
	}
	for _, c := range resp.Candidates {
		out.Choices = append(out.Choices, spec.Choice{
			Index: c.Index,
			Message: spec.Message{
				Role:      spec.RoleAssistant,
				Content:   partsToText(c.Content.Parts),
				ToolCalls: partsToToolCalls(c.Content.Parts),
				Images:    partsToImages(c.Content.Parts),
			},
			FinishReason: spec.FinishReasonPtr(MapFinishReasonToUnified(c.FinishReason)),
		})
	}
	if resp.UsageMetadata != nil {
		out.Usage = convertUsage(resp.UsageMetadata)
	}
	if len(out.Choices) == 0 && out.Usage == nil {
		return nil, fmt.Errorf("上游响应缺少 candidates 与 usageMetadata")
	}
	return out, nil
}

// convertUsage 归一 usage，思维链取自 thoughtsTokenCount。
func convertUsage(u *UsageMetadata) *spec.Usage {
	if u == nil {
		return nil
	}
	out := &spec.Usage{
		PromptTokens:     u.PromptTokenCount,
		CompletionTokens: u.CandidatesTokenCount,
		ReasoningTokens:  u.ThoughtsTokenCount,
		TotalTokens:      u.TotalTokenCount,
	}
	out.Normalize()
	return out
}

// BuildResponse 把统一响应序列化为 Gemini 响应体。
func BuildResponse(u *spec.UnifiedResponse) map[string]interface{} {
	if u == nil {
		return ErrorResponse("响应为空", "INTERNAL")
	}
	candidates := make([]map[string]interface{}, 0, len(u.Choices))
	for i, c := range u.Choices {
		parts := []map[string]interface{}{{"text": c.Message.Content}}
		for _, call := range c.Message.ToolCalls {
			parts = append(parts, map[string]interface{}{
				"functionCall": map[string]interface{}{"name": call.Name, "args": decodeArgs(call.Arguments)},
			})
		}
		// Gemini 侧没有 images 扩展字段，统一还原为 inlineData parts。
		parts = append(parts, buildInlineParts(c.Message.Images)...)
		reason := FinishStop
		if c.FinishReason != nil {
			reason = MapFinishReasonToGemini(*c.FinishReason)
		}
		candidates = append(candidates, map[string]interface{}{
			"content":      map[string]interface{}{"role": RoleModel, "parts": parts},
			"finishReason": reason,
			"index":        c.Index,
		})
		_ = i
	}
	body := map[string]interface{}{
		"candidates":   candidates,
		"modelVersion": u.Model,
	}
	if u.Usage != nil {
		body["usageMetadata"] = map[string]interface{}{
			"promptTokenCount":     u.Usage.PromptTokens,
			"candidatesTokenCount": u.Usage.CompletionTokens,
			"totalTokenCount":      u.Usage.TotalTokens,
		}
	}
	return body
}

// ---------------------------------------------------------------- 流式方向

// ParseStreamEvent 解析 Gemini SSE（alt=sse）中的单个 data 载荷。
func ParseStreamEvent(payload []byte) (*spec.StreamChunk, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return nil, nil
	}
	var resp Response
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("解析 Gemini 流式块失败: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("上游流式返回错误: %s", resp.Error.Message)
	}

	out := &spec.StreamChunk{ID: "gemini", Model: resp.ModelVersion}
	for _, c := range resp.Candidates {
		text := partsToText(c.Content.Parts)
		delta := spec.Delta{Role: spec.RoleAssistant}
		if text != "" {
			delta.Content = text
		}
		if calls := partsToToolCalls(c.Content.Parts); len(calls) > 0 {
			delta.ToolCalls = calls
		}
		if imgs := partsToImages(c.Content.Parts); len(imgs) > 0 {
			delta.Images = imgs
		}
		choice := spec.StreamChoice{Index: c.Index, Delta: delta}
		if c.FinishReason != "" {
			choice.FinishReason = spec.FinishReasonPtr(MapFinishReasonToUnified(c.FinishReason))
		}
		out.Choices = append(out.Choices, choice)
	}
	if resp.UsageMetadata != nil {
		out.Usage = convertUsage(resp.UsageMetadata)
	}
	return out, nil
}

// BuildStreamEvent 把统一流式块编码为 Gemini SSE 一行（data: {...}\r\n\r\n）。
func BuildStreamEvent(u *spec.StreamChunk) ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("流式块为空")
	}
	candidates := make([]map[string]interface{}, 0, len(u.Choices))
	for _, c := range u.Choices {
		parts := []map[string]interface{}{}
		if c.Delta.Content != "" {
			parts = append(parts, map[string]interface{}{"text": c.Delta.Content})
		}
		for _, call := range c.Delta.ToolCalls {
			parts = append(parts, map[string]interface{}{
				"functionCall": map[string]interface{}{"name": call.Name, "args": decodeArgs(call.Arguments)},
			})
		}
		parts = append(parts, buildInlineParts(c.Delta.Images)...)
		if len(parts) == 0 {
			parts = append(parts, map[string]interface{}{"text": ""})
		}
		candidate := map[string]interface{}{
			"content": map[string]interface{}{"role": RoleModel, "parts": parts},
			"index":   c.Index,
		}
		if c.FinishReason != nil {
			candidate["finishReason"] = MapFinishReasonToGemini(*c.FinishReason)
		}
		candidates = append(candidates, candidate)
	}

	payload := map[string]interface{}{"candidates": candidates, "modelVersion": u.Model}
	if u.Usage != nil {
		payload["usageMetadata"] = map[string]interface{}{
			"promptTokenCount":     u.Usage.PromptTokens,
			"candidatesTokenCount": u.Usage.CompletionTokens,
			"totalTokenCount":      u.Usage.TotalTokens,
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 Gemini 流式块失败: %w", err)
	}
	return append(append([]byte("data: "), data...), '\r', '\n', '\r', '\n'), nil
}

// ---------------------------------------------------------------- 错误与映射

// ErrorResponse 构造 Gemini 风格错误体。
func ErrorResponse(message, status string) map[string]interface{} {
	return map[string]interface{}{
		"error": map[string]interface{}{
			"code":    400,
			"message": message,
			"status":  status,
		},
	}
}

// ExtractError 从上游响应体提取错误信息。
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

// MapFinishReasonToUnified Gemini -> 统一格式。
func MapFinishReasonToUnified(reason string) string {
	switch strings.ToUpper(reason) {
	case FinishStop:
		return spec.FinishStop
	case FinishMaxTokens, "LENGTH":
		return spec.FinishLength
	case "TOOL_CALLS", "FUNCTION_CALL":
		return spec.FinishToolCalls
	case "":
		return spec.FinishStop
	default:
		return strings.ToLower(reason)
	}
}

// MapFinishReasonToGemini 统一格式 -> Gemini。
func MapFinishReasonToGemini(reason string) string {
	switch strings.ToLower(reason) {
	case spec.FinishStop:
		return FinishStop
	case spec.FinishLength:
		return FinishMaxTokens
	case spec.FinishToolCalls:
		return FinishStop
	case "":
		return FinishStop
	default:
		return strings.ToUpper(reason)
	}
}

// ---------------------------------------------------------------- 工具函数

func partsToText(parts []Part) string {
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.Text)
	}
	return sb.String()
}

// partsToImages 从 inlineData 片段提取图片结果。
func partsToImages(parts []Part) []spec.Image {
	var out []spec.Image
	for _, p := range parts {
		if p.InlineData == nil {
			continue
		}
		out = append(out, spec.Image{
			Type:     "inline_data",
			MimeType: p.InlineData.MimeType,
			B64Data:  p.InlineData.Data,
		})
	}
	return out
}

// buildInlineParts 把统一图片还原为 Gemini inlineData 片段。
func buildInlineParts(images []spec.Image) []map[string]interface{} {
	var out []map[string]interface{}
	for _, img := range images {
		if img.IsEmpty() {
			continue
		}
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		out = append(out, map[string]interface{}{
			"inlineData": map[string]interface{}{"mimeType": mime, "data": img.B64Data},
		})
	}
	return out
}

func partsToToolCalls(parts []Part) []spec.ToolCall {
	var out []spec.ToolCall
	for _, p := range parts {
		if p.FunctionCall == nil {
			continue
		}
		args := ""
		if len(p.FunctionCall.Args) > 0 {
			if data, err := json.Marshal(p.FunctionCall.Args); err == nil {
				args = string(data)
			}
		}
		out = append(out, spec.ToolCall{ID: p.FunctionCall.Name, Type: "function", Name: p.FunctionCall.Name, Arguments: args})
	}
	return out
}

// decodeArgs 把 JSON 字符串还原为对象；非法时返回空对象。
func decodeArgs(args string) map[string]interface{} {
	if args == "" {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(args), &out); err != nil || out == nil {
		return map[string]interface{}{}
	}
	return out
}
