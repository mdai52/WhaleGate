package oauth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPKCE(t *testing.T) {
	verifier, challenge, err := PKCE()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(verifier) < 43 {
		t.Fatalf("code_verifier 过短: %d", len(verifier))
	}
	if strings.ContainsAny(verifier, "+/=") {
		t.Fatalf("verifier 应为 URL 安全字符: %s", verifier)
	}
	if len(challenge) < 43 {
		t.Fatalf("code_challenge 过短: %d", len(challenge))
	}

	// S256：challenge = BASE64URL(SHA256(verifier))
	v2, c2, _ := PKCE()
	if v2 == verifier || c2 == challenge {
		t.Fatal("两次生成的 PKCE 不应相同")
	}
}

func TestProviderRegistry(t *testing.T) {
	p, ok := Get("anthropic")
	if !ok {
		t.Fatal("应内置 anthropic 供应商")
	}
	if p.Flow != FlowAuthorizationCode || !p.UsesPKCE {
		t.Fatalf("anthropic 应为 PKCE 授权码流程: %+v", p)
	}
	if p.TokenURL == "" || p.ClientID == "" {
		t.Fatal("anthropic 缺少令牌端点或 client_id")
	}

	codex, ok := Get("codex")
	if !ok {
		t.Fatal("应内置 codex 供应商")
	}
	if codex.ClientID == "" || codex.TokenURL == "" {
		t.Fatal("codex 配置不完整")
	}

	if _, ok := Get("not-exist"); ok {
		t.Fatal("不存在的供应商不应返回")
	}
}

func TestLoadFromConfig(t *testing.T) {
	LoadFromConfig(nil)
	if _, ok := Get("anthropic"); !ok {
		t.Fatal("空配置不应影响内置供应商")
	}
}

func TestParseCredentialFileClaude(t *testing.T) {
	raw := []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-xxx","refreshToken":"rt-xxx","expiresAt":1893456000000,"scopes":["user:inference"]}}`)
	out, err := ParseCredentialFile(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.Provider != "anthropic" {
		t.Fatalf("供应商推断错误: %s", out.Provider)
	}
	if out.AccessToken != "sk-ant-xxx" || out.RefreshToken != "rt-xxx" {
		t.Fatalf("令牌解析错误: %+v", out)
	}
	if out.ExpiresAt == nil || out.ExpiresAt.UnixMilli() != 1893456000000 {
		t.Fatalf("过期时间解析错误: %+v", out.ExpiresAt)
	}
}

func TestParseCredentialFileCodex(t *testing.T) {
	raw := []byte(`{"tokens":{"access_token":"eyJhbGciOi","refresh_token":"rt-2","account_id":"acc-1"},"last_refresh":"2024-01-01"}`)
	out, err := ParseCredentialFile(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.Provider != "codex" || out.AccessToken != "eyJhbGciOi" || out.Account != "acc-1" {
		t.Fatalf("解析错误: %+v", out)
	}
}

func TestParseCredentialFileGeneric(t *testing.T) {
	raw := []byte(`{"access_token":"tok","refresh_token":"rt","expires_in":3600,"provider":"kimi-cn"}`)
	out, err := ParseCredentialFile(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.Provider != "kimi-cn" || out.AccessToken != "tok" {
		t.Fatalf("解析错误: %+v", out)
	}
	if out.ExpiresAt == nil {
		t.Fatal("expires_in 应换算为过期时间")
	}
}

func TestParseCredentialFileInvalid(t *testing.T) {
	if _, err := ParseCredentialFile([]byte(`not json`)); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
	if _, err := ParseCredentialFile([]byte(`{"foo":1}`)); err == nil {
		t.Fatal("缺少 access_token 应报错")
	}
}

func TestSessionsUnknownState(t *testing.T) {
	s := NewSessions(nil)
	if _, err := s.CompleteAuthorization(context.Background(), "nope", "code"); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("未知 state 应返回 ErrUnknownState，实际 %v", err)
	}
	if _, err := s.PollDevice(context.Background(), "nope"); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("未知 state 应返回 ErrUnknownState，实际 %v", err)
	}
}

func TestStartAuthorizationMissingConfig(t *testing.T) {
	s := NewSessions(nil)
	// 未配置 client_id 的自定义供应商应给出明确错误
	p := &Provider{ID: "bad", Name: "bad", Flow: FlowAuthorizationCode}
	if _, err := s.StartAuthorization(p, ""); err == nil {
		t.Fatal("缺少 auth_url/client_id 应报错")
	}
}
