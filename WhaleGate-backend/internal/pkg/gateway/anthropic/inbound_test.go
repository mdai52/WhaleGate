package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func TestParseRequestBasics(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 1024,
		"system": "你是助手",
		"temperature": 0.5,
		"stop_sequences": ["STOP"],
		"stream": true,
		"messages": [{"role": "user", "content": "你好"}]
	}`)

	u, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if u.Model != "claude-sonnet-4-6" {
		t.Fatalf("模型未解析: %s", u.Model)
	}
	if !u.Stream {
		t.Fatal("stream 未透传")
	}
	if u.MaxTokens == nil || *u.MaxTokens != 1024 {
		t.Fatalf("max_tokens 未解析: %v", u.MaxTokens)
	}
	if u.Temperature == nil || *u.Temperature != 0.5 {
		t.Fatalf("temperature 未解析: %v", u.Temperature)
	}
	if len(u.Stop) != 1 || u.Stop[0] != "STOP" {
		t.Fatalf("stop_sequences 未解析: %v", u.Stop)
	}
	if len(u.Messages) != 2 {
		t.Fatalf("期望 system + user 两条消息，实际 %d", len(u.Messages))
	}
	if u.Messages[0].Role != spec.RoleSystem || u.Messages[0].Content != "你是助手" {
		t.Fatalf("system 未折回消息: %+v", u.Messages[0])
	}
	if u.Messages[1].Role != spec.RoleUser || u.Messages[1].Content != "你好" {
		t.Fatalf("user 消息不正确: %+v", u.Messages[1])
	}
}

// system 用文本块数组书写时同样要能抽取出来。
func TestParseRequestSystemBlocks(t *testing.T) {
	raw := []byte(`{
		"model": "m", "max_tokens": 8,
		"system": [{"type": "text", "text": "第一段"}, {"type": "text", "text": "第二段"}],
		"messages": [{"role": "user", "content": "hi"}]
	}`)
	u, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if u.Messages[0].Role != spec.RoleSystem || u.Messages[0].Content != "第一段\n第二段" {
		t.Fatalf("system 块未正确合并: %+v", u.Messages[0])
	}
}

func TestParseRequestToolUseAndResult(t *testing.T) {
	raw := []byte(`{
		"model": "m", "max_tokens": 64,
		"messages": [
			{"role": "assistant", "content": [
				{"type": "text", "text": "我来查一下"},
				{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "北京"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "晴 25 度"}
			]},
			{"role": "user", "content": "继续"}
		]
	}`)

	u, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(u.Messages) != 3 {
		t.Fatalf("期望 3 条消息，实际 %d: %+v", len(u.Messages), u.Messages)
	}
	assistant := u.Messages[0]
	if assistant.Role != spec.RoleAssistant || assistant.Content != "我来查一下" {
		t.Fatalf("assistant 文本未解析: %+v", assistant)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("tool_use 未还原为 tool_calls: %+v", assistant)
	}
	call := assistant.ToolCalls[0]
	if call.ID != "toolu_1" || call.Name != "get_weather" || call.Arguments != `{"city":"北京"}` {
		t.Fatalf("tool_call 内容不正确: %+v", call)
	}

	result := u.Messages[1]
	if result.Role != spec.RoleTool || result.ToolCallID != "toolu_1" || result.Content != "晴 25 度" {
		t.Fatalf("tool_result 未拆成 role=tool 消息: %+v", result)
	}
}

// 同一条 user 消息里文本与 tool_result 混排时，顺序必须保持。
func TestParseRequestMixedUserContent(t *testing.T) {
	raw := []byte(`{
		"model": "m", "max_tokens": 64,
		"messages": [{"role": "user", "content": [
			{"type": "text", "text": "结果如下"},
			{"type": "tool_result", "tool_use_id": "t1", "content": "42"},
			{"type": "text", "text": "请继续"}
		]}]
	}`)
	u, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(u.Messages) != 3 {
		t.Fatalf("期望 3 条消息（user / tool / user），实际 %d: %+v", len(u.Messages), u.Messages)
	}
	if u.Messages[0].Role != spec.RoleUser || u.Messages[0].Content != "结果如下" {
		t.Fatalf("第一段文本位置不对: %+v", u.Messages[0])
	}
	if u.Messages[1].Role != spec.RoleTool || u.Messages[1].Content != "42" {
		t.Fatalf("tool 结果位置不对: %+v", u.Messages[1])
	}
	if u.Messages[2].Role != spec.RoleUser || u.Messages[2].Content != "请继续" {
		t.Fatalf("tool 之后的文本应单独成条: %+v", u.Messages[2])
	}
}

func TestParseRequestImage(t *testing.T) {
	raw := []byte(`{
		"model": "m", "max_tokens": 16,
		"messages": [{"role": "user", "content": [
			{"type": "text", "text": "这是什么"},
			{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AAAA"}}
		]}]
	}`)
	u, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(u.Messages) != 1 || len(u.Messages[0].Images) != 1 {
		t.Fatalf("图片未解析: %+v", u.Messages)
	}
	if u.Messages[0].Images[0].B64Data != "AAAA" || u.Messages[0].Images[0].MimeType != "image/png" {
		t.Fatalf("图片内容不正确: %+v", u.Messages[0].Images[0])
	}
}

func TestParseRequestTools(t *testing.T) {
	raw := []byte(`{
		"model": "m", "max_tokens": 16,
		"tools": [{"name": "get_weather", "description": "查天气", "input_schema": {"type": "object"}}],
		"messages": [{"role": "user", "content": "hi"}]
	}`)
	u, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(u.Tools) != 1 {
		t.Fatalf("工具未解析: %+v", u.Tools)
	}
	if u.Tools[0].Type != "function" || u.Tools[0].Name != "get_weather" || u.Tools[0].Parameters == nil {
		t.Fatalf("工具字段不正确: %+v", u.Tools[0])
	}
}

func TestParseRequestRejectsMissingModelOrMessages(t *testing.T) {
	if _, err := ParseRequest([]byte(`{"max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)); err == nil {
		t.Fatal("缺少 model 应报错")
	}
	if _, err := ParseRequest([]byte(`{"model":"m","max_tokens":8,"messages":[]}`)); err == nil {
		t.Fatal("messages 为空应报错")
	}
}

// ---------------------------------------------------------------- 入站响应

func TestBuildResponseText(t *testing.T) {
	finish := spec.FinishStop
	got := BuildResponse(&spec.UnifiedResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-4o",
		Choices: []spec.Choice{{
			Message:      spec.Message{Role: spec.RoleAssistant, Content: "你好"},
			FinishReason: &finish,
		}},
		Usage: &spec.Usage{PromptTokens: 10, CompletionTokens: 5},
	})

	if got["type"] != "message" || got["role"] != "assistant" {
		t.Fatalf("信封字段不正确: %+v", got)
	}
	if got["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason 映射错误: %v", got["stop_reason"])
	}
	blocks, _ := got["content"].([]map[string]interface{})
	if len(blocks) != 1 || blocks[0]["type"] != BlockText || blocks[0]["text"] != "你好" {
		t.Fatalf("正文块不正确: %+v", blocks)
	}
	usage, _ := got["usage"].(map[string]interface{})
	if usage["input_tokens"] != 10 || usage["output_tokens"] != 5 {
		t.Fatalf("usage 不正确: %+v", usage)
	}
}

func TestBuildResponseToolCalls(t *testing.T) {
	finish := spec.FinishToolCalls
	got := BuildResponse(&spec.UnifiedResponse{
		ID:    "chatcmpl-2",
		Model: "gpt-4o",
		Choices: []spec.Choice{{
			Message: spec.Message{
				Role:    spec.RoleAssistant,
				Content: "我来查",
				ToolCalls: []spec.ToolCall{{
					ID: "call_1", Type: "function", Name: "get_weather", Arguments: `{"city":"北京"}`,
				}},
			},
			FinishReason: &finish,
		}},
	})

	if got["stop_reason"] != "tool_use" {
		t.Fatalf("tool_use 的 stop_reason 应为 tool_use，实际 %v", got["stop_reason"])
	}
	blocks, _ := got["content"].([]map[string]interface{})
	if len(blocks) != 2 {
		t.Fatalf("期望文本块 + tool_use 块，实际 %+v", blocks)
	}
	tool, _ := blocks[1]["input"].(map[string]interface{})
	if blocks[1]["type"] != BlockToolUse || blocks[1]["name"] != "get_weather" || tool["city"] != "北京" {
		t.Fatalf("tool_use 块不正确: %+v", blocks[1])
	}
}

func TestFinishToStopReason(t *testing.T) {
	cases := map[string]string{
		spec.FinishStop:      "end_turn",
		spec.FinishLength:    "max_tokens",
		spec.FinishToolCalls: "tool_use",
	}
	for in, want := range cases {
		if got := FinishToStopReason(in); got != want {
			t.Fatalf("%s 应映射为 %s，实际 %s", in, want, got)
		}
	}
}

// ---------------------------------------------------------------- 入站流式

// 事件顺序必须严格符合 Anthropic 约定，否则客户端会解析失败。
func TestStreamEncoderEventOrder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)

	mustWrite(t, enc.Write(&spec.StreamChunk{
		ID: "msg_1", Model: "gpt-4o",
		Choices: []spec.StreamChoice{{Delta: spec.Delta{Content: "你"}}},
	}))
	mustWrite(t, enc.Write(&spec.StreamChunk{
		Choices: []spec.StreamChoice{{Delta: spec.Delta{Content: "好"}}},
	}))
	finish := spec.FinishStop
	mustWrite(t, enc.Write(&spec.StreamChunk{
		Choices: []spec.StreamChoice{{FinishReason: &finish}},
		Usage:   &spec.Usage{PromptTokens: 3, CompletionTokens: 2},
	}))
	mustWrite(t, enc.Close())

	want := []string{
		EventMessageStart,
		EventContentBlockStart,
		EventContentBlockDelta,
		EventContentBlockDelta,
		EventContentBlockStop,
		EventMessageDelta,
		EventMessageStop,
	}
	if got := eventNames(buf.String()); !equalStrings(got, want) {
		t.Fatalf("事件序列不正确\n期望: %v\n实际: %v\n原文:\n%s", want, got, buf.String())
	}

	// message_start 必须携带 id 与 model
	if !strings.Contains(buf.String(), `"id":"msg_1"`) || !strings.Contains(buf.String(), `"model":"gpt-4o"`) {
		t.Fatalf("message_start 缺少 id/model:\n%s", buf.String())
	}
}

// 工具调用要走 input_json_delta，且文本块与工具块之间要正确闭合。
func TestStreamEncoderToolUse(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)

	mustWrite(t, enc.Write(&spec.StreamChunk{
		ID: "msg_2", Model: "m",
		Choices: []spec.StreamChoice{{Delta: spec.Delta{Content: "调用中"}}},
	}))
	mustWrite(t, enc.Write(&spec.StreamChunk{
		Choices: []spec.StreamChoice{{Delta: spec.Delta{ToolCalls: []spec.ToolCall{
			{ID: "call_1", Name: "get_weather", Arguments: `{"city":"BJ"}`},
		}}}},
	}))
	finish := spec.FinishToolCalls
	mustWrite(t, enc.Write(&spec.StreamChunk{
		Choices: []spec.StreamChoice{{FinishReason: &finish}},
	}))
	mustWrite(t, enc.Close())

	out := buf.String()
	want := []string{
		EventMessageStart,
		EventContentBlockStart, // index 0 文本
		EventContentBlockDelta,
		EventContentBlockStop,  // 切换块前先闭合
		EventContentBlockStart, // index 1 tool_use
		EventContentBlockDelta,
		EventContentBlockStop,
		EventMessageDelta,
		EventMessageStop,
	}
	if got := eventNames(out); !equalStrings(got, want) {
		t.Fatalf("事件序列不正确\n期望: %v\n实际: %v\n原文:\n%s", want, got, out)
	}
	if !strings.Contains(out, `"type":"input_json_delta"`) {
		t.Fatalf("工具参数应以 input_json_delta 下发:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("结束原因应为 tool_use:\n%s", out)
	}
}

// 上游一个块都没给时也要输出完整序列，否则客户端会一直等 message_stop。
func TestStreamEncoderCloseWithoutChunks(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	mustWrite(t, enc.Close())

	want := []string{EventMessageStart, EventMessageDelta, EventMessageStop}
	if got := eventNames(buf.String()); !equalStrings(got, want) {
		t.Fatalf("空流的收尾序列不正确: %v", got)
	}
}

func TestStreamEncoderWriteError(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	mustWrite(t, enc.WriteError("上游中断"))

	out := buf.String()
	if !strings.Contains(out, "event: "+EventError) || !strings.Contains(out, `"type":"api_error"`) {
		t.Fatalf("错误事件不正确:\n%s", out)
	}
}

// ---------------------------------------------------------------- 辅助

func mustWrite(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
}

// eventNames 按出现顺序抽取 SSE 中的 event 名。
func eventNames(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "event: "); ok {
			out = append(out, name)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 每个 data 行都必须是合法 JSON，避免手工拼串引入语法错误。
func TestStreamEncoderPayloadIsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	enc := NewStreamEncoder(&buf)
	mustWrite(t, enc.Write(&spec.StreamChunk{
		ID: "msg_3", Model: "m",
		Choices: []spec.StreamChoice{{Delta: spec.Delta{Content: "x"}}},
	}))
	mustWrite(t, enc.Close())

	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
			t.Fatalf("data 行不是合法 JSON: %q (%v)", line, err)
		}
	}
}
