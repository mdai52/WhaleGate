// Package oauth 提供上游 AI 服务的 OAuth 凭证获取能力：
// 授权码（PKCE，支持粘贴授权码）、设备码两种流程，以及认证文件解析。
//
// 内置 Anthropic (Claude) 与 OpenAI Codex 的 CLI 端点；其余供应商（Kimi、Muse 等）
// 通过配置文件补充端点与 client_id 即可接入。
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/whalegate/whalegate/internal/config"
)

// Flow 授权流程类型。
type Flow string

// 支持的流程。
const (
	FlowAuthorizationCode Flow = "authorization_code"
	FlowDeviceCode        Flow = "device_code"
)

// TokenBodyFormat token 端点的请求体格式。
type TokenBodyFormat string

// 请求体格式。
const (
	BodyForm TokenBodyFormat = "form"
	BodyJSON TokenBodyFormat = "json"
)

// Provider 一个上游服务的 OAuth 定义。
type Provider struct {
	// ID 唯一标识，如 anthropic / codex / kimi-cn。
	ID string `json:"id"`
	// Name 展示名。
	Name string `json:"name"`
	// Description 描述。
	Description string `json:"description,omitempty"`
	// Flow 授权流程。
	Flow Flow `json:"flow"`
	// AuthURL 授权页地址（授权码流程）。
	AuthURL string `json:"auth_url,omitempty"`
	// TokenURL 令牌端点。
	TokenURL string `json:"token_url,omitempty"`
	// DeviceAuthURL 设备码申请端点（设备码流程）。
	DeviceAuthURL string `json:"device_auth_url,omitempty"`
	// RedirectURI 授权码流程的回调地址（需在 OAuth 应用中登记）。
	RedirectURI string `json:"-"`
	// ClientID 客户端 ID，可来自内置值或配置。
	ClientID string `json:"-"`
	// ClientSecret 客户端密钥（部分供应商如 Google 的令牌端点要求）。
	ClientSecret string `json:"-"`
	// Scopes 授权范围。
	Scopes []string `json:"scopes,omitempty"`
	// SetupNote 面向管理员的配置提示：内置公开客户端无法直接用于自建站点时给出
	// 原因与解决路径（如 Gemini 需自建 Google Cloud 客户端）。配置覆盖后清空。
	SetupNote string `json:"setup_note,omitempty"`
	// UsesPKCE 是否使用 PKCE。
	UsesPKCE bool `json:"-"`
	// TokenBody token 端点请求体格式，默认 form。
	TokenBody TokenBodyFormat `json:"-"`
}

var (
	registryMu sync.RWMutex
	registry   = map[string]*Provider{}
)

func register(p *Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.ID] = p
}

// Get 按 ID 获取供应商定义。
func Get(id string) (*Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[id]
	return p, ok
}

// List 返回全部供应商（按 ID 排序）。
func List() []*Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	out := make([]*Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// 内置供应商：CLI 客户端的公开端点，可被同名配置覆盖。
func init() {
	register(&Provider{
		ID:          "anthropic",
		Name:        "Anthropic OAuth",
		Description: "通过 OAuth 流程登录 Anthropic (Claude) 服务，自动获取并保存认证文件。",
		Flow:        FlowAuthorizationCode,
		AuthURL:     "https://claude.ai/oauth/authorize",
		TokenURL:    "https://platform.claude.com/v1/oauth/token",
		ClientID:    "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		Scopes:      []string{"org:create_api_key", "user:profile", "user:inference"},
		UsesPKCE:    true,
		TokenBody:   BodyJSON,
	})
	register(&Provider{
		ID:          "codex",
		Name:        "Codex OAuth",
		Description: "通过 OAuth 流程登录 Codex 服务，自动获取并保存认证文件。",
		Flow:        FlowAuthorizationCode,
		AuthURL:     "https://auth.openai.com/oauth/authorize",
		TokenURL:    "https://auth.openai.com/oauth/token",
		ClientID:    "app_EMoamEEZ73f0CkXaXp7hrann",
		Scopes:      []string{"openid", "profile", "email", "offline_access"},
		UsesPKCE:    true,
		TokenBody:   BodyForm,
	})
}

// LoadFromConfig 把配置文件中声明的供应商注册进来。
//
// ID 与内置定义相同时做**字段级覆盖**：以内置定义打底，只替换配置中显式填写的字段。
// 这样接入自建 OAuth 客户端（例如用自己的 Google Cloud 项目登录 Gemini）时，
// 只需写 client_id / client_secret / redirect_uri，不必重复抄写端点与 scopes。
func LoadFromConfig(items []config.OAuthProviderConfig) {
	for _, item := range items {
		if item.ID == "" {
			continue
		}
		base := &Provider{}
		if existing, ok := Get(item.ID); ok {
			clone := *existing
			base = &clone
		}

		if item.Name != "" {
			base.Name = item.Name
		}
		if item.Description != "" {
			base.Description = item.Description
		}
		if item.AuthURL != "" {
			base.AuthURL = item.AuthURL
		}
		if item.TokenURL != "" {
			base.TokenURL = item.TokenURL
		}
		if item.DeviceAuthURL != "" {
			base.DeviceAuthURL = item.DeviceAuthURL
		}
		if item.RedirectURI != "" {
			base.RedirectURI = item.RedirectURI
		}
		if item.ClientID != "" {
			base.ClientID = item.ClientID
			// 管理员换用自建客户端后，内置客户端的配置提示不再适用。
			base.SetupNote = ""
		}
		if item.ClientSecret != "" {
			base.ClientSecret = item.ClientSecret
		}
		if len(item.Scopes) > 0 {
			base.Scopes = item.Scopes
		}
		if item.Flow != "" {
			if flow := Flow(item.Flow); flow == FlowDeviceCode {
				base.Flow = FlowDeviceCode
			} else {
				base.Flow = FlowAuthorizationCode
			}
		}
		if base.Flow == "" {
			base.Flow = FlowAuthorizationCode
		}
		if base.TokenBody = TokenBodyFormat(item.TokenBodyFormat); base.TokenBody != BodyJSON {
			base.TokenBody = BodyForm
		}
		if base.TokenURL == "" || base.ClientID == "" {
			continue
		}
		base.ID = item.ID
		base.UsesPKCE = true
		register(base)
	}
}

// ---------------------------------------------------------------- PKCE

// PKCE 生成 code_verifier 与 S256 code_challenge。
func PKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 64)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("生成随机数失败: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// randomState 生成防 CSRF 的 state。
func randomState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Now 便于测试注入的时间源。
var Now = time.Now
