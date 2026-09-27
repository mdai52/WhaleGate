package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/gemini"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/service"
)

func TestParseGeminiPath(t *testing.T) {
	cases := []struct {
		path   string
		model  string
		action string
	}{
		{"/gemini-2.0-flash:generateContent", "gemini-2.0-flash", "generateContent"},
		{"/gemini-2.0-flash:streamGenerateContent", "gemini-2.0-flash", "streamGenerateContent"},
		{"/models/gemini-1.5-pro:generateContent", "models/gemini-1.5-pro", "generateContent"},
		{"/gemini-2.0-flash", "gemini-2.0-flash", ""},
	}
	for _, c := range cases {
		model, action := parseGeminiPath(c.path)
		if model != c.model || action != c.action {
			t.Errorf("parseGeminiPath(%q) = (%q, %q)，期望 (%q, %q)", c.path, model, action, c.model, c.action)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	if isRetryable(nil) {
		t.Fatal("nil 不应重试")
	}
	if isRetryable(apierr.New(apierr.ErrModelNotFound, "")) {
		t.Fatal("模型不存在不应重试")
	}
	if isRetryable(apierr.New(apierr.ErrInvalidParam, "")) {
		t.Fatal("参数错误不应重试")
	}
	if !isRetryable(apierr.New(apierr.ErrUpstream, "")) {
		t.Fatal("上游错误应重试")
	}
	if !isRetryable(apierr.New(apierr.ErrUpstreamTimeout, "")) {
		t.Fatal("上游超时应重试")
	}
}

func TestUpstreamHTTPError(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   int
	}{
		{http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`, apierr.CodeUpstream},
		{http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`, apierr.CodeRateLimited},
		{http.StatusNotFound, `{"error":{"message":"model not found"}}`, apierr.CodeModelNotFound},
		{http.StatusGatewayTimeout, `timeout`, apierr.CodeUpstreamTimeout},
		{http.StatusBadGateway, `bad gateway`, apierr.CodeUpstream},
	}
	for _, c := range cases {
		err := upstreamHTTPError(gateway.ProtocolOpenAI, c.status, []byte(c.body))
		if apierr.CodeOf(err) != c.code {
			t.Errorf("status %d 期望码 %d，实际 %d", c.status, c.code, apierr.CodeOf(err))
		}
	}
}

func TestUpstreamHTTPErrorGemini(t *testing.T) {
	err := upstreamHTTPError(gateway.ProtocolGemini, 400, []byte(`{"error":{"message":"API key not valid"}}`))
	if apierr.CodeOf(err) != apierr.CodeUpstream {
		t.Fatalf("期望上游错误，实际 %v", err)
	}
	if !contains(err.Error(), "API key not valid") {
		t.Fatalf("错误信息缺失: %v", err)
	}
}

func TestClassifyUpstreamError(t *testing.T) {
	if err := classifyUpstreamError(nil); err != nil {
		t.Fatalf("nil 应返回 nil: %v", err)
	}
	err := classifyUpstreamError(context.DeadlineExceeded)
	if apierr.CodeOf(err) != apierr.CodeUpstreamTimeout {
		t.Fatalf("超时应映射为上游超时，实际 %v", err)
	}
}

func TestTruncateText(t *testing.T) {
	if got := truncateText("abc", 10); got != "abc" {
		t.Fatalf("短文本不应截断: %q", got)
	}
	if got := truncateText("abcdef", 3); got != "abc" {
		t.Fatalf("超长文本应截断: %q", got)
	}
}

func TestGeminiActionConstants(t *testing.T) {
	if geminiActionGenerateContent != gemini.ActionGenerateContent ||
		geminiActionStreamGenerateContent != gemini.ActionStreamGenerateContent {
		t.Fatal("动作常量与上游协议不一致")
	}
}

func TestResolveUsageReported(t *testing.T) {
	svc := &service.Container{Config: &config.Config{
		Gateway: config.GatewayConfig{EstimateCompletionTokens: 1024},
	}}
	usage, confidence := resolveUsage(svc, &spec.UnifiedRequest{Model: "m"}, &relayOutcome{
		Usage: &spec.Usage{PromptTokens: 20, CompletionTokens: 30, ReasoningTokens: 10, TotalTokens: 50},
	})
	if confidence != model.ConfidenceReported {
		t.Fatalf("上游有 usage 时应标记为 reported，实际 %s", confidence)
	}
	if usage.TotalTokens != 50 || usage.ReasoningTokens != 10 {
		t.Fatalf("用量解析错误: %+v", usage)
	}
}

func TestResolveUsageEstimated(t *testing.T) {
	svc := &service.Container{Config: &config.Config{
		Gateway: config.GatewayConfig{EstimateCompletionTokens: 512},
	}}
	// 上游未返回 usage（或全为 0）时按请求参数估算。
	usage, confidence := resolveUsage(svc, &spec.UnifiedRequest{
		Model:    "m",
		Messages: []spec.Message{{Role: spec.RoleUser, Content: "hello world"}},
	}, &relayOutcome{})
	if confidence != model.ConfidenceEstimated {
		t.Fatalf("应标记为 estimated，实际 %s", confidence)
	}
	if usage.CompletionTokens != 512 || usage.PromptTokens <= 0 {
		t.Fatalf("估算结果错误: %+v", usage)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
