package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestParseOpenAIModels 解析 OpenAI 风格模型列表（对象数组）。
func TestParseOpenAIModels(t *testing.T) {
	body := []byte(`{"object":"list","data":[
		{"id":"gpt-4o","owned_by":"openai"},
		{"id":"text-embedding-3-large","owned_by":"openai"},
		{"id":"dall-e-3","owned_by":"openai"}
	]}`)
	models, err := parseOpenAIModels(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("期望 3 个模型，实际 %d", len(models))
	}
	want := map[string][]string{
		"gpt-4o":                 {CapChat, CapVision, CapTool},
		"text-embedding-3-large": {CapEmbedding},
		"dall-e-3":               {CapImage},
	}
	for _, m := range models {
		expect, ok := want[m.ID]
		if !ok {
			t.Fatalf("未预期的模型: %s", m.ID)
		}
		for _, c := range expect {
			if !hasCap(m.Capabilities, c) {
				t.Errorf("%s 应含能力 %s，实际 %v", m.ID, c, m.Capabilities)
			}
		}
	}
	if models[0].ContextWindow != 128000 {
		t.Errorf("gpt-4o 上下文应为 128000，实际 %d", models[0].ContextWindow)
	}
	if models[0].MaxOutput != 16384 {
		t.Errorf("gpt-4o 输出上限应为 16384，实际 %d", models[0].MaxOutput)
	}
}

// TestParseOpenAIModelsStringArray 兼容直接返回字符串数组的上游。
func TestParseOpenAIModelsStringArray(t *testing.T) {
	models, err := parseOpenAIModels([]byte(`["gpt-4o-mini","claude-3-5-sonnet-20241022"]`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("期望 2 个模型，实际 %d", len(models))
	}
	if models[1].ContextWindow != 200000 {
		t.Errorf("claude-3-5 上下文应为 200000，实际 %d", models[1].ContextWindow)
	}
}

// TestParseGeminiModels 解析 Gemini 原生模型列表并转换能力。
func TestParseGeminiModels(t *testing.T) {
	body := []byte(`{"models":[
		{"name":"models/gemini-1.5-pro","displayName":"Gemini 1.5 Pro",
		 "inputTokenLimit":2097152,"outputTokenLimit":8192,
		 "supportedGenerationMethods":["generateContent","countTokens"]},
		{"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"]}
	]}`)
	models, err := parseGeminiModels(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("期望 2 个模型，实际 %d", len(models))
	}
	pro := models[0]
	if pro.ID != "gemini-1.5-pro" {
		t.Errorf("应去掉 models/ 前缀，实际 %q", pro.ID)
	}
	if pro.ContextWindow != 2097152 || pro.MaxOutput != 8192 {
		t.Errorf("窗口/输出不符: %d / %d", pro.ContextWindow, pro.MaxOutput)
	}
	if !hasCap(pro.Capabilities, CapChat) {
		t.Errorf("应含 chat 能力，实际 %v", pro.Capabilities)
	}
	if !hasCap(models[1].Capabilities, CapEmbedding) {
		t.Errorf("embedding 模型识别失败: %v", models[1].Capabilities)
	}
}

// TestProbeUpstreamOpenAI 端到端：探测 OpenAI 兼容上游。
func TestProbeUpstreamOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`))
	}))
	defer srv.Close()

	res, err := ProbeUpstream(context.Background(), srv.URL+"/v1", "sk-test", "")
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	if res.Type != ProtocolOpenAI {
		t.Errorf("协议应为 openai，实际 %s", res.Type)
	}
	if len(res.Models) != 2 {
		t.Errorf("期望 2 个模型，实际 %d", len(res.Models))
	}
	if res.Models[0].ID != "gpt-4o" {
		t.Errorf("模型排序/解析异常: %s", res.Models[0].ID)
	}
}

// TestProbeUpstreamGemini 端到端：探测 Gemini 原生上游（密钥走 ?key=）。
func TestProbeUpstreamGemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "AIza-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-2.0-flash",
			"supportedGenerationMethods":["generateContent"],"inputTokenLimit":1048576}]}`))
	}))
	defer srv.Close()

	res, err := ProbeUpstream(context.Background(), srv.URL, "AIza-test", "")
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	if res.Type != ProtocolGemini {
		t.Errorf("协议应为 gemini，实际 %s", res.Type)
	}
	if len(res.Models) != 1 || res.Models[0].ID != "gemini-2.0-flash" {
		t.Errorf("模型解析异常: %+v", res.Models)
	}
}

// TestProbeUpstreamUnauthorized 密钥错误应明确报错而不是静默失败。
func TestProbeUpstreamUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	if _, err := ProbeUpstream(context.Background(), srv.URL, "bad-key", ""); err == nil {
		t.Fatal("密钥错误时应返回错误")
	}
}

// TestProbeUpstreamInvalid 地址/密钥为空应直接拒绝。
func TestProbeUpstreamInvalid(t *testing.T) {
	if _, err := ProbeUpstream(context.Background(), "", "key", ""); err == nil {
		t.Error("空地址应报错")
	}
	if _, err := ProbeUpstream(context.Background(), "http://x", "", ""); err == nil {
		t.Error("空密钥应报错")
	}
}

// TestInferCapabilities 能力分类覆盖。
func TestInferCapabilities(t *testing.T) {
	cases := map[string]string{
		"text-embedding-3-large": CapEmbedding,
		"dall-e-3":               CapImage,
		"whisper-1":              CapAudio,
		"deepseek-r1":            CapReasoning,
		"claude-3-7-sonnet":      CapVision,
		"qwen2.5-vl-72b":         CapVision,
	}
	for id, want := range cases {
		got := inferCapabilities(id)
		if !hasCap(got, want) {
			t.Errorf("%s 应含 %s，实际 %v", id, want, got)
		}
	}
}

// TestFilterModels 按能力过滤模型。
func TestFilterModels(t *testing.T) {
	models := []DetectedModel{
		{ID: "a", Capabilities: []string{CapChat}},
		{ID: "b", Capabilities: []string{CapEmbedding}},
	}
	out := FilterModels(models, []string{CapEmbedding})
	if len(out) != 1 || out[0].ID != "b" {
		t.Errorf("过滤结果异常: %+v", out)
	}
	if len(FilterModels(models, nil)) != 2 {
		t.Error("空过滤条件应返回全部")
	}
}

func hasCap(caps []string, target string) bool {
	for _, c := range caps {
		if c == target {
			return true
		}
	}
	return false
}
