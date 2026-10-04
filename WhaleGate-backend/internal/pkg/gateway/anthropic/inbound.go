package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// 本文件实现入站方向：把 Anthropic Messages API 的请求/响应
// 与内部统一格式互转，使 Claude Code、CCSwitch 这类只认
// /v1/messages 的客户端可以直接接入本网关。
//
// 与 anthropic.go 中的上游方向互为反向，二者共用同一套数据结构与常量。

// ---------------------------------------------------------------- 入站请求

// ParseRequest 解析 Anthropic Messages API 的入站请求。
//
// 与 BuildRequest 互为反向：
//   - 顶层 system 折回 system 消息，放在最前（统一层的角色约定）；
//   - user 消息中的 tool_result 块拆成独立的 role=tool 消息，
//     并保持与同一消息内其余内容块的先后顺序；
//   - assistant 消息中的 tool_use 块还原为统一的 tool_calls。
func ParseRequest(raw []byte) (*spec.UnifiedRequest, error) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 请求失败: %w", err)
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return nil, fmt.Errorf("缺少 model")
	}

	out := &spec.UnifiedRequest{
		Model:          model,
		Stream:         req.Stream,
		Temperature:    req.Temperature,
		TopP:           req.TopP,
		Stop:           req.StopSequences,
		NativeProtocol: spec.ProtocolAnthropic,
		NativeModel:    model,
	}
	if req.MaxTokens > 0 {
		out.MaxTokens = &req.MaxTokens
	}
	if req.Metadata != nil {
		out.User = req.Metadata.UserID
	}
	if req.TopK != nil {
		// 统一层没有 top_k 字段，交给自定义参数通道按渠道能力决定去向
		out.Extra = map[string]interface{}{"top_k": *req.TopK}
	}
	if len(req.Tools) > 0 {
		tools := make([]spec.ToolDefinition, 0, len(req.Tools))
		for _, t := range req.Tools {
			if strings.TrimSpace(t.Name) == "" {
				continue
			}
			tools = append(tools, spec.ToolDefinition{
				Type:        "function",
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			})
		}
		out.Tools = tools
	}

	if text := systemText(req.System); text != "" {
		out.Messages = append(out.Messages, spec.Message{Role: spec.RoleSystem, Content: text})
	}
	for _, m := range req.Messages {
		msgs, err := toUnifiedMessages(m)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, msgs...)
	}
	if len(out.Messages) == 0 {
		return nil, fmt.Errorf("messages 不能为空")
	}
	return out, nil
}

// systemText 抽取顶层 system 文本。Anthropic 允许字符串或文本块数组两种写法。
func systemText(system interface{}) string {
	switch v := system.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	}
	blocks, err := decodeContent(system)
	if err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == BlockText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// toUnifiedMessages 把一条 Anthropic 消息拆成统一层的若干条消息。
//
// 一条 user 消息里可能混着文本、图片与 tool_result，而统一层要求
// tool 结果必须是独立的 role=tool 消息，因此这里按块顺序切分：
// 遇到 tool_result 就先冲掉已累积的 user 内容，再追加一条 tool 消息。
func toUnifiedMessages(m Message) ([]spec.Message, error) {
	role := strings.TrimSpace(m.Role)
	blocks, err := decodeContent(m.Content)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		text := plainText(m.Content)
		if text == "" {
			return nil, nil
		}
		return []spec.Message{{Role: role, Content: text}}, nil
	}

	var (
		out      []spec.Message
		text     strings.Builder
		images   []spec.Image
		toolCall []spec.ToolCall
	)

	flush := func() {
		content := text.String()
		if content == "" && len(images) == 0 {
			return
		}
		out = append(out, spec.Message{Role: role, Content: content, Images: images})
		text.Reset()
		images = nil
	}

	for _, b := range blocks {
		switch b.Type {
		case BlockText:
			text.WriteString(b.Text)
		case BlockThinking:
			// 上游回放的历史思维链不再带回，避免污染上下文
		case BlockImage:
			if img, ok := toImage(b.Source); ok {
				images = append(images, img)
			}
		case BlockToolUse:
			toolCall = append(toolCall, spec.ToolCall{
				ID:        b.ID,
				Type:      "function",
				Name:      b.Name,
				Arguments: encodeJSONObject(b.Input),
			})
		case BlockToolResut:
			flush()
			content, imgs := toolResultParts(b.Content)
			out = append(out, spec.Message{
				Role:       spec.RoleTool,
				ToolCallID: b.ToolUseID,
				Content:    content,
				Images:     imgs,
			})
		}
	}
	flush()

	if len(toolCall) > 0 {
		// tool_calls 归到最后一条同角色消息上，与 OpenAI 语义保持一致
		if len(out) > 0 && out[len(out)-1].Role == role && len(out[len(out)-1].ToolCalls) == 0 {
			out[len(out)-1].ToolCalls = toolCall
		} else {
			out = append(out, spec.Message{Role: role, ToolCalls: toolCall})
		}
	}
	return out, nil
}

// decodeContent 把 content 统一解析为内容块列表；字符串形式视为单个文本块。
func decodeContent(raw interface{}) ([]ContentBlock, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		return []ContentBlock{{Type: BlockText, Text: v}}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		// 不是块数组时按纯文本处理，避免因为格式意外而整请求失败
		var text string
		if json.Unmarshal(data, &text) == nil {
			return []ContentBlock{{Type: BlockText, Text: text}}, nil
		}
		return nil, nil
	}
	return blocks, nil
}

// plainText 从 content 中取纯文本，用于非块结构。
func plainText(raw interface{}) string {
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// toImage 把 Anthropic 图片来源转为统一图片。
func toImage(src *ImageSource) (spec.Image, bool) {
	if src == nil {
		return spec.Image{}, false
	}
	switch src.Type {
	case "base64":
		if strings.TrimSpace(src.Data) == "" {
			return spec.Image{}, false
		}
		return spec.Image{B64Data: src.Data, MimeType: src.MediaType}, true
	case "url":
		if strings.TrimSpace(src.URL) == "" {
			return spec.Image{}, false
		}
		return spec.Image{URL: src.URL, MimeType: src.MediaType}, true
	}
	return spec.Image{}, false
}

// toolResultParts 抽取 tool_result 的文本与图片。
func toolResultParts(content interface{}) (string, []spec.Image) {
	if s, ok := content.(string); ok {
		return s, nil
	}
	blocks, err := decodeContent(content)
	if err != nil || len(blocks) == 0 {
		return plainText(content), nil
	}
	var (
		text   strings.Builder
		images []spec.Image
	)
	for _, b := range blocks {
		switch b.Type {
		case BlockText:
			text.WriteString(b.Text)
		case BlockImage:
			if img, ok := toImage(b.Source); ok {
				images = append(images, img)
			}
		}
	}
	return text.String(), images
}

// ---------------------------------------------------------------- 入站响应

// BuildResponse 把统一响应转换为 Anthropic Messages 响应体。
//
// 与 ParseResponse 互为反向：正文与工具调用还原为内容块，
// finish_reason 映射回 Anthropic 的 stop_reason。
func BuildResponse(resp *spec.UnifiedResponse) map[string]interface{} {
	if resp == nil {
		return ErrorResponse("响应为空", "api_error")
	}

	id := resp.ID
	if strings.TrimSpace(id) == "" {
		id = fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	model := resp.Model

	blocks := make([]map[string]interface{}, 0, 2)
	var (
		text      string
		toolCalls []spec.ToolCall
		stop      string
	)
	for _, choice := range resp.Choices {
		if choice.Message.Content != "" {
			text += choice.Message.Content
		}
		toolCalls = append(toolCalls, choice.Message.ToolCalls...)
		if choice.FinishReason != nil {
			stop = FinishToStopReason(*choice.FinishReason)
		}
	}
	if text != "" {
		blocks = append(blocks, map[string]interface{}{"type": BlockText, "text": text})
	}
	for _, call := range toolCalls {
		blocks = append(blocks, map[string]interface{}{
			"type":  BlockToolUse,
			"id":    call.ID,
			"name":  call.Name,
			"input": decodeJSONObject(call.Arguments),
		})
	}
	if stop == "" {
		// Anthropic 要求 stop_reason 非空；有工具调用时按 tool_use 收尾
		if len(toolCalls) > 0 {
			stop = "tool_use"
		} else {
			stop = "end_turn"
		}
	}

	usage := map[string]interface{}{"input_tokens": 0, "output_tokens": 0}
	if resp.Usage != nil {
		usage["input_tokens"] = resp.Usage.PromptTokens
		usage["output_tokens"] = resp.Usage.CompletionTokens
	}

	return map[string]interface{}{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       blocks,
		"stop_reason":   stop,
		"stop_sequence": nil,
		"usage":         usage,
	}
}

// FinishToStopReason 把统一 finish_reason 映射回 Anthropic 的 stop_reason。
func FinishToStopReason(reason string) string {
	switch reason {
	case spec.FinishLength:
		return "max_tokens"
	case spec.FinishToolCalls:
		return "tool_use"
	case spec.FinishStop, "":
		return "end_turn"
	default:
		return "end_turn"
	}
}

// ---------------------------------------------------------------- 入站流式

// StreamEncoder 有状态的 Anthropic 流式编码器。
//
// Anthropic 的 SSE 是严格有序的事件序列：
//
//	message_start → content_block_start → content_block_delta* →
//	content_block_stop → [下一个内容块…] → message_delta → message_stop
//
// 与 OpenAI 那种逐块独立的编码方式不同，这里必须跨块记住
// 「已经发过 message_start 没有」「当前打开的是第几个内容块」，
// 因此每个请求持有独立实例。
type StreamEncoder struct {
	w         io.Writer
	started   bool
	id        string
	model     string
	blockIdx  int
	blockOpen bool
	blockKind string
	toolIdx   int
	// sentMessageDelta 记录是否已下发结束原因，避免收尾时重复发送。
	sentMessageDelta bool
}

// NewStreamEncoder 创建入站流式编码器。
func NewStreamEncoder(w io.Writer) *StreamEncoder {
	return &StreamEncoder{w: w, blockIdx: -1}
}

// Write 编码一个统一流式块。
func (e *StreamEncoder) Write(chunk *spec.StreamChunk) error {
	if chunk == nil {
		return nil
	}
	if !e.started {
		e.id = chunk.ID
		e.model = chunk.Model
		if strings.TrimSpace(e.id) == "" {
			e.id = fmt.Sprintf("msg_%d", time.Now().UnixNano())
		}
		if err := e.writeMessageStart(); err != nil {
			return err
		}
	}

	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if err := e.ensureBlock(BlockText); err != nil {
				return err
			}
			if err := e.writeEvent(EventContentBlockDelta, map[string]interface{}{
				"type":  EventContentBlockDelta,
				"index": e.blockIdx,
				"delta": map[string]interface{}{"type": DeltaTextDelta, "text": choice.Delta.Content},
			}); err != nil {
				return err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			if err := e.ensureBlock(BlockToolUse); err != nil {
				return err
			}
			if err := e.writeEvent(EventContentBlockDelta, map[string]interface{}{
				"type":  EventContentBlockDelta,
				"index": e.blockIdx,
				"delta": map[string]interface{}{
					"type":         DeltaInputJSONDelta,
					"partial_json": call.Arguments,
				},
			}); err != nil {
				return err
			}
		}
		if choice.FinishReason != nil {
			if err := e.closeBlock(); err != nil {
				return err
			}
			if err := e.writeMessageDelta(FinishToStopReason(*choice.FinishReason), chunk.Usage); err != nil {
				return err
			}
		}
	}
	return nil
}

// Close 输出收尾事件。上游未给出结束原因时也要补齐事件序列，
// 否则客户端会一直等待 message_stop。
func (e *StreamEncoder) Close() error {
	if !e.started {
		// 一个块都没收到：仍要给出完整的空响应，避免客户端解析失败
		if err := e.writeMessageStart(); err != nil {
			return err
		}
	}
	if err := e.closeBlock(); err != nil {
		return err
	}
	if !e.sentMessageDelta {
		if err := e.writeMessageDelta("end_turn", nil); err != nil {
			return err
		}
	}
	return e.writeEvent(EventMessageStop, map[string]interface{}{"type": EventMessageStop})
}

// WriteError 在流已经开始的情况下输出 Anthropic 的错误事件。
func (e *StreamEncoder) WriteError(message string) error {
	payload, err := json.Marshal(map[string]interface{}{
		"type": EventError,
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": message,
		},
	})
	if err != nil {
		return err
	}
	return e.writeRaw(EventError, payload)
}

// ensureBlock 保证当前打开的是指定类型的内容块；
// 类型切换时先关闭旧块再开新块，块序号递增。
func (e *StreamEncoder) ensureBlock(kind string) error {
	if e.blockOpen && e.blockKind == kind {
		return nil
	}
	if err := e.closeBlock(); err != nil {
		return err
	}
	e.blockIdx++
	e.blockOpen = true
	e.blockKind = kind

	block := map[string]interface{}{"type": kind}
	if kind == BlockText {
		block["text"] = ""
	} else {
		block["id"] = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), e.toolIdx)
		block["name"] = ""
		block["input"] = map[string]interface{}{}
		e.toolIdx++
	}
	return e.writeEvent(EventContentBlockStart, map[string]interface{}{
		"type":          EventContentBlockStart,
		"index":         e.blockIdx,
		"content_block": block,
	})
}

func (e *StreamEncoder) closeBlock() error {
	if !e.blockOpen {
		return nil
	}
	e.blockOpen = false
	return e.writeEvent(EventContentBlockStop, map[string]interface{}{
		"type":  EventContentBlockStop,
		"index": e.blockIdx,
	})
}

func (e *StreamEncoder) writeMessageStart() error {
	e.started = true
	usage := map[string]interface{}{"input_tokens": 0, "output_tokens": 0}
	return e.writeEvent(EventMessageStart, map[string]interface{}{
		"type": EventMessageStart,
		"message": map[string]interface{}{
			"id":            e.id,
			"type":          "message",
			"role":          "assistant",
			"model":         e.model,
			"content":       []interface{}{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         usage,
		},
	})
}

func (e *StreamEncoder) writeMessageDelta(stopReason string, u *spec.Usage) error {
	e.sentMessageDelta = true
	usage := map[string]interface{}{"input_tokens": 0, "output_tokens": 0}
	if u != nil {
		usage["input_tokens"] = u.PromptTokens
		usage["output_tokens"] = u.CompletionTokens
	}
	return e.writeEvent(EventMessageDelta, map[string]interface{}{
		"type": EventMessageDelta,
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usage,
	})
}

// writeEvent 按 Anthropic 的 SSE 约定输出「event: + data:」两行。
func (e *StreamEncoder) writeEvent(event string, payload map[string]interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return e.writeRaw(event, data)
}

func (e *StreamEncoder) writeRaw(event string, data []byte) error {
	_, err := fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", event, data)
	return err
}
