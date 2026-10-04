package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func TestMessagesURL(t *testing.T) {
	cases := []struct{ base, want string }{
		{"https://api.anthropic.com", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/v1", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/v1/", "https://api.anthropic.com/v1/messages"},
		{"https://gateway.example/v1/", "https://gateway.example/v1/messages"},
	}
	for _, c := range cases {
		if got := MessagesURL(c.base); got != c.want {
			t.Errorf("MessagesURL(%q) = %q, want %q", c.base, got, c.want)
		}
	}
}

func TestModelsURL(t *testing.T) {
	if got := ModelsURL("https://api.anthropic.com"); got != "https://api.anthropic.com/v1/models" {
		t.Errorf("无 /v1 前缀时应自动补全，got %q", got)
	}
	if got := ModelsURL("https://api.anthropic.com/v1"); got != "https://api.anthropic.com/v1/models" {
		t.Errorf("带 /v1 前缀时不应重复拼接，got %q", got)
	}
}

func TestBuildRequest_SystemAndTools(t *testing.T) {
	u := &spec.UnifiedRequest{
		Model:  "claude-sonnet-4-5",
		Stream: true,
		Messages: []spec.Message{
			{Role: spec.RoleSystem, Content: "你是一个助手"},
			{Role: spec.RoleUser, Content: "查一下天气"},
			{
				Role:      spec.RoleAssistant,
				ToolCalls: []spec.ToolCall{{ID: "call_1", Type: "function", Name: "get_weather", Arguments: `{"city":"上海"}`}},
			},
			{Role: spec.RoleTool, ToolCallID: "call_1", Content: "晴，26℃"},
		},
		Tools: []spec.ToolDefinition{{
			Type: "function", Name: "get_weather", Description: "查询天气",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
		}},
		MaxTokens: nil,
	}
	body, err := BuildRequest(u)
	if err != nil {
		t.Fatalf("BuildRequest 失败: %v", err)
	}
	assertSystem := func(want string) {
		if got, _ := body["system"].(string); got != want {
			t.Errorf("system = %v, want %q", body["system"], want)
		}
	}
	assertSystem("你是一个助手")

	// max_tokens 在 Anthropic 是必填字段，缺失时必须兜底。
	if got, _ := body["max_tokens"].(int); got != DefaultMaxTokens {
		t.Errorf("max_tokens = %v, want %d", body["max_tokens"], DefaultMaxTokens)
	}
	if got, _ := body["stream"].(bool); !got {
		t.Errorf("stream 应为 true")
	}

	msgs := body["messages"].([]Message)
	if len(msgs) != 3 {
		t.Fatalf("messages 长度 = %d, want 3（system 已提取，tool_result 合并进 user）", len(msgs))
	}
	if msgs[0].Role != spec.RoleUser {
		t.Errorf("msgs[0].Role = %q, want user", msgs[0].Role)
	}

	// 助手的 tool_calls 应转成 tool_use 块。
	assistant := msgs[1]
	if assistant.Role != spec.RoleAssistant {
		t.Fatalf("msgs[1].Role = %q, want assistant", assistant.Role)
	}
	blocks, err := toBlocks(assistant.Content)
	if err != nil {
		t.Fatalf("解析助手内容失败: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Type != BlockToolUse {
		t.Fatalf("助手消息应含一个 tool_use 块，got %+v", blocks)
	}
	if blocks[0].Name != "get_weather" || blocks[0].ID != "call_1" {
		t.Errorf("tool_use 块字段错误: %+v", blocks[0])
	}
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(encodeJSONObject(blocks[0].Input)), &input); err != nil {
		t.Fatalf("tool_use.input 应为合法对象: %v", err)
	}
	if input["city"] != "上海" {
		t.Errorf("tool_use.input.city = %v, want 上海", input["city"])
	}

	// OpenAI 的 role=tool 必须降级为 user 消息里的 tool_result 块。
	result := msgs[2]
	if result.Role != spec.RoleUser {
		t.Fatalf("tool 消息应合并为 user 消息，got %q", result.Role)
	}
	blocks, err = toBlocks(result.Content)
	if err != nil {
		t.Fatalf("解析 tool_result 失败: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Type != BlockToolResut {
		t.Fatalf("应为 tool_result 块，got %+v", blocks)
	}
	if blocks[0].ToolUseID != "call_1" {
		t.Errorf("tool_use_id = %q, want call_1", blocks[0].ToolUseID)
	}

	tools := body["tools"].([]Tool)
	if len(tools) != 1 || tools[0].Name != "get_weather" {
		t.Fatalf("tools 转换错误: %+v", tools)
	}
	if tools[0].InputSchema == nil {
		t.Errorf("input_schema 未写入")
	}
}

func TestBuildRequest_MultipleSystemMessages(t *testing.T) {
	u := &spec.UnifiedRequest{
		Model: "claude-sonnet-4-5",
		Messages: []spec.Message{
			{Role: spec.RoleSystem, Content: "规则一"},
			{Role: "developer", Content: "规则二"},
			{Role: spec.RoleUser, Content: "你好"},
		},
	}
	body, err := BuildRequest(u)
	if err != nil {
		t.Fatalf("BuildRequest 失败: %v", err)
	}
	list, ok := body["system"].([]string)
	if !ok || len(list) != 2 {
		t.Fatalf("多条 system 应合并为数组，got %#v", body["system"])
	}
	msgs := body["messages"].([]Message)
	if len(msgs) != 1 || msgs[0].Role != spec.RoleUser {
		t.Fatalf("system 不应留在 messages 中: %+v", msgs)
	}
}

func TestBuildRequest_EmptyMessages(t *testing.T) {
	if _, err := BuildRequest(&spec.UnifiedRequest{Model: "claude"}); err == nil {
		t.Fatal("空 messages 应返回错误")
	}
}

func TestBuildRequest_Images(t *testing.T) {
	u := &spec.UnifiedRequest{
		Model: "claude-sonnet-4-5",
		Messages: []spec.Message{{
			Role:    spec.RoleUser,
			Content: "看图",
			Images: []spec.Image{
				{Type: "image_url", MimeType: "image/jpeg", B64Data: "AAAA"},
				{Type: "image_url", URL: "https://example.com/a.png"},
			},
		}},
	}
	body, err := BuildRequest(u)
	if err != nil {
		t.Fatalf("BuildRequest 失败: %v", err)
	}
	msgs := body["messages"].([]Message)
	blocks, err := toBlocks(msgs[0].Content)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("应为 1 个文本块 + 2 个图片块，got %d", len(blocks))
	}
	if blocks[1].Type != BlockImage || blocks[1].Source.Type != "base64" || blocks[1].Source.MediaType != "image/jpeg" {
		t.Errorf("内联图片应转为 base64 source: %+v", blocks[1])
	}
	if blocks[2].Source.Type != "url" || blocks[2].Source.URL != "https://example.com/a.png" {
		t.Errorf("远程图片应转为 url source: %+v", blocks[2])
	}
}

func TestParseResponse(t *testing.T) {
	raw := []byte(`{
		"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-5",
		"content":[
			{"type":"thinking","thinking":"让我想想"},
			{"type":"text","text":"你好，"},
			{"type":"text","text":"有什么可以帮您？"},
			{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"天气"}}
		],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":12,"output_tokens":34}
	}`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("ParseResponse 失败: %v", err)
	}
	if resp.ID != "msg_01" {
		t.Errorf("ID = %q", resp.ID)
	}
	if got := resp.Text(); got != "你好，有什么可以帮您？" {
		t.Errorf("多个文本块应拼接且不含思维链，got %q", got)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices 长度 = %d", len(resp.Choices))
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("tool_calls 长度 = %d", len(calls))
	}
	if calls[0].ID != "toolu_1" || calls[0].Name != "search" {
		t.Errorf("tool_call 字段错误: %+v", calls[0])
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatalf("arguments 应为 JSON 对象字符串: %v", err)
	}
	if args["q"] != "天气" {
		t.Errorf("arguments.q = %v", args["q"])
	}
	if resp.Choices[0].FinishReason == nil || *resp.Choices[0].FinishReason != spec.FinishToolCalls {
		t.Errorf("stop_reason=tool_use 应映射为 tool_calls，got %v", resp.Choices[0].FinishReason)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 34 {
		t.Errorf("usage 转换错误: %+v", resp.Usage)
	}
	if resp.Usage.TotalTokens != 46 {
		t.Errorf("total_tokens = %d, want 46", resp.Usage.TotalTokens)
	}
}

func TestParseResponse_CacheTokensCountedAsPrompt(t *testing.T) {
	raw := []byte(`{"id":"m","type":"message","role":"assistant","model":"claude",
		"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",
		"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("ParseResponse 失败: %v", err)
	}
	if resp.Usage.PromptTokens != 130 {
		t.Errorf("缓存 token 应计入 prompt，got %d", resp.Usage.PromptTokens)
	}
}

func TestParseResponse_Error(t *testing.T) {
	raw := []byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	if _, err := ParseResponse(raw); err == nil {
		t.Fatal("上游错误体应返回 error")
	}
}

func TestExtractError(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"type":"error","error":{"type":"rate_limit_error","message":"限流了"}}`, "限流了"},
		{`{"error":{"message":"坏的 key"}}`, "坏的 key"},
		{`{"id":"m"}`, ""},
		{`not-json`, ""},
	}
	for _, c := range cases {
		if got := ExtractError([]byte(c.raw)); got != c.want {
			t.Errorf("ExtractError(%s) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestStopReasonToFinish(t *testing.T) {
	cases := map[string]string{
		"end_turn":      spec.FinishStop,
		"stop_sequence": spec.FinishStop,
		"max_tokens":    spec.FinishLength,
		"tool_use":      spec.FinishToolCalls,
		"refusal":       spec.FinishStop,
		"":              spec.FinishStop,
	}
	for in, want := range cases {
		if got := StopReasonToFinish(in); got != want {
			t.Errorf("StopReasonToFinish(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStreamDecoder_Text(t *testing.T) {
	d := NewStreamDecoder()
	// message_start 上报 id/model 与 input_tokens
	chunk, err := d.Decode([]byte(`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4-5","usage":{"input_tokens":20,"output_tokens":0}}}`))
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	if chunk == nil {
		t.Fatal("message_start 应产出首帧")
	}
	if chunk.ID != "msg_1" || chunk.Model != "claude-opus-4-5" {
		t.Errorf("首帧 id/model = %q/%q", chunk.ID, chunk.Model)
	}
	if chunk.Choices[0].Delta.Role != spec.RoleAssistant {
		t.Errorf("首帧应携带 assistant 角色")
	}
	if chunk.Usage == nil || chunk.Usage.PromptTokens != 20 {
		t.Errorf("首帧应上报 prompt tokens: %+v", chunk.Usage)
	}

	// ping / content_block_start / content_block_stop(文本) 均不产生输出
	for _, ev := range []string{
		`{"type":"ping"}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	} {
		if got, err := d.Decode([]byte(ev)); err != nil || got != nil {
			t.Errorf("%s 应返回 nil，got %+v err %v", ev, got, err)
		}
	}

	chunk, err = d.Decode([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}`))
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	if chunk == nil || chunk.Choices[0].Delta.Content != "你好" {
		t.Errorf("文本增量未正确下发: %+v", chunk)
	}

	// 思维链增量不应进入正文
	if got, _ := d.Decode([]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"推理中"}}`)); got != nil {
		t.Errorf("思维链增量不应下发，got %+v", got)
	}

	if got, _ := d.Decode([]byte(`{"type":"content_block_stop","index":0}`)); got != nil {
		t.Errorf("文本块结束无需下发，got %+v", got)
	}

	// message_delta 上报 finish_reason 与 output_tokens
	chunk, err = d.Decode([]byte(`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":7}}`))
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	if got := *chunk.Choices[0].FinishReason; got != spec.FinishLength {
		t.Errorf("finish_reason = %q, want length", got)
	}
	if chunk.Usage == nil || chunk.Usage.CompletionTokens != 7 {
		t.Errorf("output_tokens 应作为 completion 上报: %+v", chunk.Usage)
	}

	if got, _ := d.Decode([]byte(`{"type":"message_stop"}`)); got != nil {
		t.Errorf("message_stop 无需下发")
	}
	if got, _ := d.Decode([]byte(`[DONE]`)); got != nil {
		t.Errorf("[DONE] 应被忽略")
	}
}

func TestStreamDecoder_ToolCallAccumulation(t *testing.T) {
	d := NewStreamDecoder()
	// message_start 是有效输出帧（携带 id/model 与 input_tokens），只校验不报错即可。
	if _, err := d.Decode([]byte(`{"type":"message_start","message":{"id":"msg_2","model":"claude","usage":{"input_tokens":1}}}`)); err != nil {
		t.Fatalf("message_start 解析失败: %v", err)
	}
	mustNil(t, d, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_9","name":"search","input":{}}}`)
	// 工具参数是分片 JSON，必须跨事件拼装后才能使用。
	for _, part := range []string{`{"q":"`, `上海`, `","limit":3}`} {
		mustNil(t, d, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":`+strconvQuote(part)+`}}`)
	}
	chunk, err := d.Decode([]byte(`{"type":"content_block_stop","index":0}`))
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	if chunk == nil {
		t.Fatal("tool_use 块结束时应下发完整工具调用")
	}
	calls := chunk.Choices[0].Delta.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("ToolCalls 长度 = %d", len(calls))
	}
	call := calls[0]
	if call.ID != "toolu_9" || call.Name != "search" || call.Type != "function" {
		t.Errorf("工具调用字段错误: %+v", call)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		t.Fatalf("分片拼接后应为合法 JSON，got %q: %v", call.Arguments, err)
	}
	if args["q"] != "上海" || args["limit"] != float64(3) {
		t.Errorf("参数拼接结果错误: %v", args)
	}
}

// content_block_start 里一次性给全入参（无 subsequent input_json_delta）时也应可用。
func TestStreamDecoder_ToolCallFromBlockStart(t *testing.T) {
	d := NewStreamDecoder()
	mustNil(t, d, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_a","name":"calc","input":{"a":1}}}`)
	chunk, err := d.Decode([]byte(`{"type":"content_block_stop","index":0}`))
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	if chunk == nil || len(chunk.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatal("应下发一个工具调用")
	}
	if got := chunk.Choices[0].Delta.ToolCalls[0].Arguments; got != `{"a":1}` {
		t.Errorf("Arguments = %q, want {\"a\":1}", got)
	}

	// 没有任何入参时降级为空对象，避免下游解析失败。
	d2 := NewStreamDecoder()
	mustNil(t, d2, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_b","name":"noop"}}`)
	chunk2, err := d2.Decode([]byte(`{"type":"content_block_stop","index":0}`))
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	if got := chunk2.Choices[0].Delta.ToolCalls[0].Arguments; got != "{}" {
		t.Errorf("Arguments = %q, want {}", got)
	}
}

func TestStreamDecoder_ErrorEvent(t *testing.T) {
	d := NewStreamDecoder()
	_, err := d.Decode([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"上游过载"}}`))
	if err == nil {
		t.Fatal("error 事件应返回错误")
	}
}

func TestStreamDecoder_InvalidPayload(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Decode([]byte(`{broken`)); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

// ---------------------------------------------------------------- 辅助

func toBlocks(v interface{}) ([]ContentBlock, error) {
	switch typed := v.(type) {
	case []ContentBlock:
		return typed, nil
	case []interface{}:
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		var out []ContentBlock
		err = json.Unmarshal(data, &out)
		return out, err
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var out []ContentBlock
		err = json.Unmarshal(data, &out)
		return out, err
	}
}

func strconvQuote(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func mustNil(t *testing.T, d *StreamDecoder, payload string) {
	t.Helper()
	got, err := d.Decode([]byte(payload))
	if err != nil {
		t.Fatalf("Decode(%s) 失败: %v", payload, err)
	}
	if got != nil {
		t.Fatalf("Decode(%s) 应返回 nil，got %+v", payload, got)
	}
}
