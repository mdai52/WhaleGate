package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func TestParseRequestBasic(t *testing.T) {
	raw := []byte(`{
      "model": "gpt-4o-mini",
      "messages": [
        {"role": "system", "content": "你是助手"},
        {"role": "user", "content": "你好"}
      ],
      "temperature": 0.7,
      "top_p": 0.9,
      "max_tokens": 512,
      "stop": ["\n"],
      "stream": true,
      "user": "u-1"
    }`)

	req, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if req.Model != "gpt-4o-mini" {
		t.Fatalf("模型错误: %s", req.Model)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("消息数量错误: %d", len(req.Messages))
	}
	if req.Messages[0].Role != spec.RoleSystem || req.Messages[0].Content != "你是助手" {
		t.Fatalf("system 消息错误: %+v", req.Messages[0])
	}
	if req.Messages[1].Content != "你好" {
		t.Fatalf("user 消息错误: %+v", req.Messages[1])
	}
	if !req.Stream {
		t.Fatal("stream 应为 true")
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Fatalf("temperature 错误: %v", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Fatalf("top_p 错误: %v", req.TopP)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 512 {
		t.Fatalf("max_tokens 错误: %v", req.MaxTokens)
	}
	if len(req.Stop) != 1 || req.Stop[0] != "\n" {
		t.Fatalf("stop 错误: %v", req.Stop)
	}
	if req.User != "u-1" {
		t.Fatalf("user 错误: %s", req.User)
	}
}

func TestParseRequestMissingModel(t *testing.T) {
	if _, err := ParseRequest([]byte(`{"messages":[]}`)); err == nil {
		t.Fatal("缺少 model 应报错")
	}
}

func TestParseRequestInvalidJSON(t *testing.T) {
	if _, err := ParseRequest([]byte(`{`)); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestParseRequestMultimodalContent(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"看图"},{"type":"text","text":"说明"}]}]}`)
	req, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if req.Messages[0].Content != "看图说明" {
		t.Fatalf("多段文本拼接错误: %q", req.Messages[0].Content)
	}
}

func TestParseRequestStopAsString(t *testing.T) {
	req, err := ParseRequest([]byte(`{"model":"m","messages":[],"stop":"END"}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(req.Stop) != 1 || req.Stop[0] != "END" {
		t.Fatalf("stop 字符串解析错误: %v", req.Stop)
	}
}

func TestParseRequestTools(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"get_weather","description":"查询天气","parameters":{"type":"object"}}}]}`)
	req, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
		t.Fatalf("工具解析错误: %+v", req.Tools)
	}
	if req.Tools[0].Parameters["type"] != "object" {
		t.Fatalf("参数解析错误: %+v", req.Tools[0].Parameters)
	}
}

func TestParseRequestToolCalls(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"f","arguments":"{\"a\":1}"}}]}]}`)
	req, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	calls := req.Messages[0].ToolCalls
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "f" {
		t.Fatalf("工具调用解析错误: %+v", calls)
	}
	if calls[0].Arguments != `{"a":1}` {
		t.Fatalf("参数解析错误: %q", calls[0].Arguments)
	}
}

func TestBuildRequest(t *testing.T) {
	temp := 0.5
	maxTokens := 128
	body, err := BuildRequest(&spec.UnifiedRequest{
		Model:       "gpt-4o",
		Stream:      false,
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		Messages:    []spec.Message{{Role: spec.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if body["model"] != "gpt-4o" {
		t.Fatalf("model 错误: %v", body["model"])
	}
	if body["temperature"] != 0.5 {
		t.Fatalf("temperature 错误: %v", body["temperature"])
	}
	if body["max_tokens"] != 128 {
		t.Fatalf("max_tokens 错误: %v", body["max_tokens"])
	}
	if _, ok := body["stream_options"]; ok {
		t.Fatal("非流式不应带 stream_options")
	}
}

func TestBuildRequestStreamAddsUsageOption(t *testing.T) {
	body, err := BuildRequest(&spec.UnifiedRequest{
		Stream:   true,
		Messages: []spec.Message{{Role: spec.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if body["stream"] != true {
		t.Fatal("stream 应为 true")
	}
	if _, ok := body["stream_options"]; !ok {
		t.Fatal("流式应带 stream_options.include_usage")
	}
}

func TestBuildRequestEmptyMessages(t *testing.T) {
	if _, err := BuildRequest(&spec.UnifiedRequest{Model: "m"}); err == nil {
		t.Fatal("空 messages 应报错")
	}
	if _, err := BuildRequest(nil); err == nil {
		t.Fatal("nil 请求应报错")
	}
}

func TestParseResponse(t *testing.T) {
	raw := []byte(`{
      "id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"gpt-4o",
      "choices":[{"index":0,"message":{"role":"assistant","content":"你好呀"},"finish_reason":"stop"}],
      "usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
    }`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.ID != "chatcmpl-1" || resp.Model != "gpt-4o" {
		t.Fatalf("基础字段错误: %+v", resp)
	}
	if resp.Text() != "你好呀" {
		t.Fatalf("文本错误: %q", resp.Text())
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Fatalf("用量错误: %+v", resp.Usage)
	}
	if resp.Choices[0].FinishReason == nil || *resp.Choices[0].FinishReason != spec.FinishStop {
		t.Fatalf("结束原因错误: %v", resp.Choices[0].FinishReason)
	}
}

func TestParseResponseError(t *testing.T) {
	raw := []byte(`{"error":{"message":"模型不存在","type":"invalid_request_error"}}`)
	_, err := ParseResponse(raw)
	if err == nil {
		t.Fatal("上游错误应被识别")
	}
	if !strings.Contains(err.Error(), "模型不存在") {
		t.Fatalf("错误信息缺失: %v", err)
	}
}

func TestParseResponseInvalid(t *testing.T) {
	if _, err := ParseResponse([]byte(`{"id":"x"}`)); err == nil {
		t.Fatal("缺少 choices 与 usage 应报错")
	}
	if _, err := ParseResponse([]byte(`not-json`)); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestBuildResponse(t *testing.T) {
	reason := spec.FinishStop
	body := BuildResponse(&spec.UnifiedResponse{
		ID:      "chatcmpl-2",
		Model:   "gpt-4o",
		Created: 1,
		Choices: []spec.Choice{{
			Index:        0,
			Message:      spec.Message{Role: spec.RoleAssistant, Content: "hi"},
			FinishReason: &reason,
		}},
		Usage: &spec.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	})
	if body["object"] != ObjectChatCompletion {
		t.Fatalf("object 错误: %v", body["object"])
	}
	usage, ok := body["usage"].(map[string]interface{})
	if !ok || usage["total_tokens"] != 3 {
		t.Fatalf("usage 错误: %v", body["usage"])
	}
	data, _ := json.Marshal(body)
	if !strings.Contains(string(data), `"finish_reason":"stop"`) {
		t.Fatalf("finish_reason 缺失: %s", data)
	}
}

func TestBuildResponseNil(t *testing.T) {
	body := BuildResponse(nil)
	if _, ok := body["error"]; !ok {
		t.Fatal("nil 响应应返回错误体")
	}
}

func TestStreamEventRoundTrip(t *testing.T) {
	event, err := BuildStreamEvent(&spec.StreamChunk{
		ID:    "chatcmpl-3",
		Model: "gpt-4o",
		Choices: []spec.StreamChoice{{
			Index: 0,
			Delta: spec.Delta{Role: spec.RoleAssistant, Content: "你"},
		}},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if !strings.HasPrefix(string(event), "data: ") || !strings.HasSuffix(string(event), "\n\n") {
		t.Fatalf("SSE 格式错误: %q", event)
	}

	payload := strings.TrimPrefix(strings.TrimSpace(string(event)), "data: ")
	chunk, err := ParseStreamEvent([]byte(payload))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if chunk.ID != "chatcmpl-3" || chunk.Choices[0].Delta.Content != "你" {
		t.Fatalf("往返不一致: %+v", chunk)
	}
	if chunk.Object != ObjectChatCompletionChunk {
		t.Fatalf("object 错误: %s", chunk.Object)
	}
}

func TestParseStreamEventDone(t *testing.T) {
	chunk, err := ParseStreamEvent([]byte("[DONE]"))
	if err != nil {
		t.Fatalf("[DONE] 不应报错: %v", err)
	}
	if chunk != nil {
		t.Fatal("[DONE] 应返回 nil")
	}
	if chunk, err := ParseStreamEvent([]byte("   ")); err != nil || chunk != nil {
		t.Fatalf("空载荷应返回 nil: %v %v", chunk, err)
	}
}

func TestParseStreamEventUsageOnly(t *testing.T) {
	chunk, err := ParseStreamEvent([]byte(`{"id":"x","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if chunk.Usage == nil || chunk.Usage.TotalTokens != 7 {
		t.Fatalf("usage 解析错误: %+v", chunk.Usage)
	}
	if !chunk.IsEmpty() {
		t.Fatal("无 choices 的块应判定为空")
	}
}

func TestParseStreamEventUpstreamError(t *testing.T) {
	_, err := ParseStreamEvent([]byte(`{"error":{"message":"过载"}}`))
	if err == nil {
		t.Fatal("流式错误应被识别")
	}
}

func TestDoneEvent(t *testing.T) {
	if string(DoneEvent()) != "data: [DONE]\n\n" {
		t.Fatalf("结束标记错误: %q", DoneEvent())
	}
}

func TestExtractError(t *testing.T) {
	if got := ExtractError([]byte(`{"error":{"message":"配额不足"}}`)); got != "配额不足" {
		t.Fatalf("提取错误失败: %q", got)
	}
	if got := ExtractError([]byte(`{"choices":[]}`)); got != "" {
		t.Fatalf("正常响应不应提取到错误: %q", got)
	}
	if got := ExtractError([]byte(`not-json`)); got != "" {
		t.Fatalf("非法 JSON 不应提取到错误: %q", got)
	}
}
