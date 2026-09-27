package gemini

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func TestParseRequest(t *testing.T) {
	raw := []byte(`{
      "systemInstruction": {"parts": [{"text": "你是助手"}]},
      "contents": [
        {"role": "user", "parts": [{"text": "你好"}]},
        {"role": "model", "parts": [{"text": "嗨"}]}
      ],
      "generationConfig": {"temperature": 0.3, "topP": 0.8, "maxOutputTokens": 256, "stopSequences": ["END"]}
    }`)

	req, err := ParseRequest(raw, "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if req.Model != "gemini-2.0-flash" {
		t.Fatalf("模型错误: %s", req.Model)
	}
	if req.NativeProtocol != spec.ProtocolGemini {
		t.Fatalf("协议错误: %s", req.NativeProtocol)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("消息数量错误: %d", len(req.Messages))
	}
	if req.Messages[0].Role != spec.RoleSystem || req.Messages[0].Content != "你是助手" {
		t.Fatalf("systemInstruction 转换错误: %+v", req.Messages[0])
	}
	if req.Messages[1].Role != spec.RoleUser || req.Messages[1].Content != "你好" {
		t.Fatalf("user 转换错误: %+v", req.Messages[1])
	}
	if req.Messages[2].Role != spec.RoleAssistant || req.Messages[2].Content != "嗨" {
		t.Fatalf("model 应映射为 assistant: %+v", req.Messages[2])
	}
	if req.Temperature == nil || *req.Temperature != 0.3 {
		t.Fatalf("temperature 错误: %v", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.8 {
		t.Fatalf("topP 错误: %v", req.TopP)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 256 {
		t.Fatalf("maxOutputTokens 错误: %v", req.MaxTokens)
	}
	if len(req.Stop) != 1 || req.Stop[0] != "END" {
		t.Fatalf("stopSequences 错误: %v", req.Stop)
	}
}

func TestParseRequestMissingModel(t *testing.T) {
	if _, err := ParseRequest([]byte(`{"contents":[]}`), ""); err == nil {
		t.Fatal("缺少模型名应报错")
	}
	if _, err := ParseRequest([]byte(`{"contents":[]}`), "m"); err == nil {
		t.Fatal("空 contents 应报错")
	}
	if _, err := ParseRequest([]byte(`{`), "m"); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestParseRequestTools(t *testing.T) {
	raw := []byte(`{"contents":[{"role":"user","parts":[{"text":"天气"}]}],"tools":[{"functionDeclarations":[{"name":"get_weather","description":"查天气","parameters":{"type":"object"}}]}]}`)
	req, err := ParseRequest(raw, "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
		t.Fatalf("工具解析错误: %+v", req.Tools)
	}
}

func TestBuildRequest(t *testing.T) {
	temp := 0.2
	maxTokens := 64
	body, err := BuildRequest(&spec.UnifiedRequest{
		Model:       "gemini-2.0-flash",
		Messages:    []spec.Message{{Role: spec.RoleUser, Content: "你好"}},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		Stop:        []string{"END"},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	contents, ok := body["contents"].([]map[string]interface{})
	if !ok || len(contents) != 1 {
		t.Fatalf("contents 错误: %v", body["contents"])
	}
	if contents[0]["role"] != RoleUser {
		t.Fatalf("role 错误: %v", contents[0]["role"])
	}
	genCfg, ok := body["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("缺少 generationConfig")
	}
	if genCfg["maxOutputTokens"] != 64 || genCfg["temperature"] != 0.2 {
		t.Fatalf("generationConfig 错误: %v", genCfg)
	}
}

func TestBuildRequestSystemInstruction(t *testing.T) {
	body, err := BuildRequest(&spec.UnifiedRequest{
		Model: "m",
		Messages: []spec.Message{
			{Role: spec.RoleSystem, Content: "系统提示"},
			{Role: spec.RoleUser, Content: "hi"},
			{Role: spec.RoleAssistant, Content: "yo"},
		},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, ok := body["systemInstruction"]; !ok {
		t.Fatal("system 应转为 systemInstruction")
	}
	contents := body["contents"].([]map[string]interface{})
	if len(contents) != 2 {
		t.Fatalf("system 不应进入 contents: %v", contents)
	}
	if contents[1]["role"] != RoleModel {
		t.Fatalf("assistant 应映射为 model: %v", contents[1]["role"])
	}
}

func TestBuildRequestEmpty(t *testing.T) {
	if _, err := BuildRequest(&spec.UnifiedRequest{Model: "m"}); err == nil {
		t.Fatal("空 contents 应报错")
	}
	if _, err := BuildRequest(nil); err == nil {
		t.Fatal("nil 请求应报错")
	}
}

func TestParseResponse(t *testing.T) {
	raw := []byte(`{
      "candidates":[{"content":{"role":"model","parts":[{"text":"你好呀"}]},"finishReason":"STOP","index":0}],
      "usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":4,"totalTokenCount":12},
      "modelVersion":"gemini-2.0-flash"
    }`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Text() != "你好呀" {
		t.Fatalf("文本错误: %q", resp.Text())
	}
	if resp.Choices[0].Message.Role != spec.RoleAssistant {
		t.Fatalf("角色错误: %s", resp.Choices[0].Message.Role)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 8 || resp.Usage.CompletionTokens != 4 || resp.Usage.TotalTokens != 12 {
		t.Fatalf("用量错误: %+v", resp.Usage)
	}
	if *resp.Choices[0].FinishReason != spec.FinishStop {
		t.Fatalf("结束原因错误: %v", *resp.Choices[0].FinishReason)
	}
	if resp.Model != "gemini-2.0-flash" {
		t.Fatalf("模型错误: %s", resp.Model)
	}
}

func TestParseResponseError(t *testing.T) {
	raw := []byte(`{"error":{"code":404,"message":"模型不支持","status":"NOT_FOUND"}}`)
	_, err := ParseResponse(raw)
	if err == nil || !strings.Contains(err.Error(), "模型不支持") {
		t.Fatalf("上游错误应被识别: %v", err)
	}
	if _, err := ParseResponse([]byte(`{}`)); err == nil {
		t.Fatal("空响应应报错")
	}
}

func TestBuildResponse(t *testing.T) {
	reason := spec.FinishLength
	body := BuildResponse(&spec.UnifiedResponse{
		Model:   "gemini-2.0-flash",
		Choices: []spec.Choice{{Index: 0, Message: spec.Message{Role: spec.RoleAssistant, Content: "hi"}, FinishReason: &reason}},
		Usage:   &spec.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	})
	candidates, ok := body["candidates"].([]map[string]interface{})
	if !ok || len(candidates) != 1 {
		t.Fatalf("candidates 错误: %v", body["candidates"])
	}
	if candidates[0]["finishReason"] != FinishMaxTokens {
		t.Fatalf("finishReason 映射错误: %v", candidates[0]["finishReason"])
	}
	content := candidates[0]["content"].(map[string]interface{})
	if content["role"] != RoleModel {
		t.Fatalf("role 错误: %v", content["role"])
	}
	meta := body["usageMetadata"].(map[string]interface{})
	if meta["totalTokenCount"] != 3 {
		t.Fatalf("usageMetadata 错误: %v", meta)
	}
}

func TestBuildResponseNil(t *testing.T) {
	if _, ok := BuildResponse(nil)["error"]; !ok {
		t.Fatal("nil 响应应返回错误体")
	}
}

func TestStreamEventRoundTrip(t *testing.T) {
	event, err := BuildStreamEvent(&spec.StreamChunk{
		Model:   "gemini-2.0-flash",
		Choices: []spec.StreamChoice{{Index: 0, Delta: spec.Delta{Content: "逐"}}},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if !strings.HasPrefix(string(event), "data: ") {
		t.Fatalf("SSE 前缀错误: %q", event)
	}

	payload := strings.TrimPrefix(strings.TrimSpace(string(event)), "data: ")
	chunk, err := ParseStreamEvent([]byte(payload))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if chunk.Choices[0].Delta.Content != "逐" {
		t.Fatalf("往返不一致: %+v", chunk)
	}
}

func TestParseStreamEventFinishAndUsage(t *testing.T) {
	chunk, err := ParseStreamEvent([]byte(`{"candidates":[{"content":{"parts":[{"text":"end"}]},"finishReason":"MAX_TOKENS","index":0}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if *chunk.Choices[0].FinishReason != spec.FinishLength {
		t.Fatalf("结束原因错误: %v", *chunk.Choices[0].FinishReason)
	}
	if chunk.Usage == nil || chunk.Usage.TotalTokens != 3 {
		t.Fatalf("用量错误: %+v", chunk.Usage)
	}
}

func TestParseStreamEventEmpty(t *testing.T) {
	chunk, err := ParseStreamEvent([]byte(""))
	if err != nil || chunk != nil {
		t.Fatalf("空载荷应返回 nil: %v %v", chunk, err)
	}
	if _, err := ParseStreamEvent([]byte("not-json")); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestFinishReasonMapping(t *testing.T) {
	cases := map[string]string{
		FinishStop:      spec.FinishStop,
		FinishMaxTokens: spec.FinishLength,
		FinishSafety:    "safety",
		FinishOther:     "other",
		"":              spec.FinishStop,
	}
	for in, want := range cases {
		if got := MapFinishReasonToUnified(in); got != want {
			t.Errorf("MapFinishReasonToUnified(%q) = %q，期望 %q", in, got, want)
		}
	}
	if got := MapFinishReasonToGemini(spec.FinishStop); got != FinishStop {
		t.Errorf("stop 应映射为 STOP，实际 %s", got)
	}
	if got := MapFinishReasonToGemini(spec.FinishLength); got != FinishMaxTokens {
		t.Errorf("length 应映射为 MAX_TOKENS，实际 %s", got)
	}
	if got := MapFinishReasonToGemini(""); got != FinishStop {
		t.Errorf("空值应映射为 STOP，实际 %s", got)
	}
}

func TestExtractError(t *testing.T) {
	if got := ExtractError([]byte(`{"error":{"message":"API key 无效"}}`)); got != "API key 无效" {
		t.Fatalf("提取错误失败: %q", got)
	}
	if got := ExtractError([]byte(`{"candidates":[]}`)); got != "" {
		t.Fatalf("正常响应不应提取到错误: %q", got)
	}
}

func TestFunctionCallConversion(t *testing.T) {
	// Gemini 响应中的 functionCall 应转换为统一工具调用，args 编码为 JSON 字符串。
	raw := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"北京"}}}]},"index":0}]}`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].Name != "get_weather" {
		t.Fatalf("工具调用解析错误: %+v", calls)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatalf("参数不是合法 JSON: %v", err)
	}
	if args["city"] != "北京" {
		t.Fatalf("参数内容错误: %v", args)
	}
}
