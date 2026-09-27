package oauth

import (
	"encoding/json"
	"fmt"
	"time"
)

// ParsedFile 从认证文件中解析出的凭证信息。
type ParsedFile struct {
	// Provider 依据文件结构推断的供应商 ID（可为空，表示通用格式）。
	Provider string
	// AccessToken 明文访问令牌。
	AccessToken string
	// RefreshToken 刷新令牌。
	RefreshToken string
	// ExpiresAt 过期时间。
	ExpiresAt *time.Time
	// Account 账号标识（邮箱或账户 ID）。
	Account string
	// TokenType 令牌类型。
	TokenType string
	// Extra 其他保留字段。
	Extra map[string]interface{}
}

// ParseCredentialFile 解析常见 CLI 工具导出的认证文件：
//   - Claude Code: {"claudeAiOauth":{"accessToken":"...","refreshToken":"...","expiresAt":1760000000000}}
//   - Codex CLI:   {"tokens":{"access_token":"...","refresh_token":"...","account_id":"..."}}
//   - 通用格式:    {"access_token":"...","refresh_token":"...","expires_in":3600}
func ParseCredentialFile(content []byte) (*ParsedFile, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("认证文件不是合法 JSON: %w", err)
	}

	if v, ok := raw["claudeAiOauth"].(map[string]interface{}); ok {
		out := &ParsedFile{Provider: "anthropic", TokenType: "bearer"}
		out.AccessToken, _ = v["accessToken"].(string)
		out.RefreshToken, _ = v["refreshToken"].(string)
		if ms, ok := toInt64(v["expiresAt"]); ok && ms > 0 {
			t := time.UnixMilli(ms)
			out.ExpiresAt = &t
		}
		if out.AccessToken == "" {
			return nil, fmt.Errorf("认证文件缺少 accessToken")
		}
		return out, nil
	}

	if tokens, ok := raw["tokens"].(map[string]interface{}); ok {
		out := &ParsedFile{Provider: "codex"}
		out.AccessToken, _ = tokens["access_token"].(string)
		out.RefreshToken, _ = tokens["refresh_token"].(string)
		out.TokenType, _ = tokens["token_type"].(string)
		out.Account, _ = tokens["account_id"].(string)
		if out.AccessToken == "" {
			return nil, fmt.Errorf("认证文件缺少 access_token")
		}
		return out, nil
	}

	// Google Cloud 服务账号（Vertex JSON 登录）：整个 JSON 加密保存，
	// 后续绑定到 vertex 类渠道时用它换取短期访问令牌。
	if t, _ := raw["type"].(string); t == "service_account" {
		email, _ := raw["client_email"].(string)
		if email == "" {
			return nil, fmt.Errorf("服务账号文件缺少 client_email")
		}
		return &ParsedFile{
			Provider:    "vertex",
			TokenType:   "service_account",
			Account:     email,
			AccessToken: string(content),
		}, nil
	}

	// 通用格式
	out := &ParsedFile{}
	out.AccessToken, _ = raw["access_token"].(string)
	out.RefreshToken, _ = raw["refresh_token"].(string)
	out.TokenType, _ = raw["token_type"].(string)
	out.Account, _ = raw["account_id"].(string)
	if out.AccessToken == "" {
		return nil, fmt.Errorf("认证文件缺少 access_token 字段")
	}
	if ms, ok := toInt64(raw["expires_at"]); ok && ms > 0 {
		t := time.UnixMilli(ms)
		out.ExpiresAt = &t
	} else if sec, ok := toInt64(raw["expires_in"]); ok && sec > 0 {
		t := time.Now().Add(time.Duration(sec) * time.Second)
		out.ExpiresAt = &t
	}
	if provider, ok := raw["provider"].(string); ok && provider != "" {
		out.Provider = provider
	}
	return out, nil
}

func toInt64(v interface{}) (int64, bool) {
	switch typed := v.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case json.Number:
		n, err := typed.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}
