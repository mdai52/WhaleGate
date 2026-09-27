package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 会话相关错误。
var (
	// ErrPending 设备码流程仍在等待用户授权。
	ErrPending = errors.New("等待用户授权")
	// ErrUnknownState 会话不存在或已过期。
	ErrUnknownState = errors.New("授权会话不存在或已过期")
	// ErrAccessDenied 用户拒绝了授权。
	ErrAccessDenied = errors.New("用户取消了授权")
)

// sessionTTL 授权会话有效期。
const sessionTTL = 10 * time.Minute

// TokenSet 换取到的令牌。
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
	Account      string
	// Extra 令牌响应中的其他字段（如 id_token、project_id）。
	Extra map[string]interface{}
}

// StartResult 发起授权后返回给前端的信息。
type StartResult struct {
	// State 会话标识，用于轮询与完成。
	State string `json:"state"`
	// AuthorizeURL 授权码流程：引导用户打开的授权地址。
	AuthorizeURL string `json:"authorize_url,omitempty"`
	// VerificationURL 设备码流程：用户打开的验证地址。
	VerificationURL string `json:"verification_url,omitempty"`
	// UserCode 设备码流程：用户需要输入的代码。
	UserCode string `json:"user_code,omitempty"`
	// ExpiresIn 会话有效期（秒）。
	ExpiresIn int `json:"expires_in"`
}

type pendingSession struct {
	provider    *Provider
	state       string
	verifier    string
	redirectURI string
	deviceCode  string
	interval    int
	createdAt   time.Time
}

// Sessions 管理进行中的授权会话（内存态，重启即失效，符合一次性授权语义）。
type Sessions struct {
	mu      sync.Mutex
	pending map[string]*pendingSession
	client  *http.Client
}

// NewSessions 创建会话管理器。
func NewSessions(client *http.Client) *Sessions {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	s := &Sessions{pending: map[string]*pendingSession{}, client: client}
	go s.gc()
	return s
}

func (s *Sessions) gc() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		s.mu.Lock()
		now := Now()
		for state, p := range s.pending {
			if now.Sub(p.createdAt) > sessionTTL {
				delete(s.pending, state)
			}
		}
		s.mu.Unlock()
	}
}

// StartAuthorization 发起授权，返回授权地址（授权码流程）或设备码信息。
func (s *Sessions) StartAuthorization(p *Provider, redirectURI string) (*StartResult, error) {
	if p == nil {
		return nil, fmt.Errorf("供应商不存在")
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	sess := &pendingSession{provider: p, state: state, createdAt: Now()}

	switch p.Flow {
	case FlowDeviceCode:
		if p.DeviceAuthURL == "" || p.ClientID == "" {
			return nil, fmt.Errorf("该供应商缺少 device_auth_url 或 client_id 配置")
		}
		device, err := s.requestDeviceCode(p)
		if err != nil {
			return nil, err
		}
		sess.deviceCode = device.DeviceCode
		sess.interval = device.Interval
		s.mu.Lock()
		s.pending[state] = sess
		s.mu.Unlock()
		return &StartResult{
			State:           state,
			VerificationURL: device.VerificationURL,
			UserCode:        device.UserCode,
			ExpiresIn:       int(sessionTTL.Seconds()),
		}, nil

	default:
		if p.AuthURL == "" || p.ClientID == "" {
			return nil, fmt.Errorf("该供应商缺少 auth_url 或 client_id 配置")
		}
		if p.UsesPKCE {
			sess.redirectURI = redirectURI
		}

		q := url.Values{}
		q.Set("response_type", "code")
		q.Set("client_id", p.ClientID)
		q.Set("state", state)
		if len(p.Scopes) > 0 {
			q.Set("scope", strings.Join(p.Scopes, " "))
		}
		if p.UsesPKCE {
			verifier, challenge, err := PKCE()
			if err != nil {
				return nil, err
			}
			sess.verifier = verifier
			q.Set("code_challenge", challenge)
			q.Set("code_challenge_method", "S256")
		}
		if redirectURI != "" {
			q.Set("redirect_uri", redirectURI)
		}

		s.mu.Lock()
		s.pending[state] = sess
		s.mu.Unlock()

		separator := "?"
		if strings.Contains(p.AuthURL, "?") {
			separator = "&"
		}
		return &StartResult{
			State:        state,
			AuthorizeURL: p.AuthURL + separator + q.Encode(),
			ExpiresIn:    int(sessionTTL.Seconds()),
		}, nil
	}
}

// CompleteAuthorization 授权码流程：用用户粘贴的授权码换取令牌。
func (s *Sessions) CompleteAuthorization(ctx context.Context, state, code string) (*TokenSet, error) {
	sess, ok := s.take(state)
	if !ok {
		return nil, ErrUnknownState
	}
	if code == "" {
		return nil, fmt.Errorf("授权码不能为空")
	}
	return s.exchange(ctx, sess.provider, map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     sess.provider.ClientID,
		"code_verifier": sess.verifier,
		"redirect_uri":  sess.redirectURI,
	})
}

// PollDevice 设备码流程：轮询令牌端点，尚未授权时返回 ErrPending。
func (s *Sessions) PollDevice(ctx context.Context, state string) (*TokenSet, error) {
	s.mu.Lock()
	sess, ok := s.pending[state]
	s.mu.Unlock()
	if !ok {
		return nil, ErrUnknownState
	}

	set, err := s.exchange(ctx, sess.provider, map[string]string{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": sess.deviceCode,
		"client_id":   sess.provider.ClientID,
	})
	if err != nil {
		if errors.Is(err, ErrPending) || errors.Is(err, ErrAccessDenied) {
			return nil, err
		}
		return nil, err
	}

	s.take(state)
	return set, nil
}

// Refresh 用 refresh_token 续期令牌。
func (s *Sessions) Refresh(ctx context.Context, p *Provider, refreshToken string) (*TokenSet, error) {
	return s.exchange(ctx, p, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     p.ClientID,
	})
}

// take 取出并移除会话。
func (s *Sessions) take(state string) (*pendingSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.pending[state]
	if ok {
		delete(s.pending, state)
	}
	return sess, ok
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

func (s *Sessions) requestDeviceCode(p *Provider) (*deviceCodeResponse, error) {
	body := map[string]string{"client_id": p.ClientID}
	if len(p.Scopes) > 0 {
		body["scope"] = strings.Join(p.Scopes, " ")
	}
	data, err := s.postJSON(context.Background(), p.DeviceAuthURL, body)
	if err != nil {
		return nil, err
	}
	out := &deviceCodeResponse{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("解析设备码响应失败: %w", err)
	}
	if out.DeviceCode == "" {
		return nil, fmt.Errorf("设备码响应缺少 device_code")
	}
	if out.VerificationURL == "" {
		out.VerificationURL = out.VerificationURI
	}
	if out.Interval <= 0 {
		out.Interval = 5
	}
	return out, nil
}

// exchange 向令牌端点换取令牌。
func (s *Sessions) exchange(ctx context.Context, p *Provider, form map[string]string) (*TokenSet, error) {
	endpoint := p.TokenURL
	body := map[string]string{}
	for k, v := range form {
		if v != "" {
			body[k] = v
		}
	}
	// 部分供应商（如 Google）的令牌端点要求携带客户端密钥
	if p.ClientSecret != "" {
		body["client_secret"] = p.ClientSecret
	}

	var (
		data []byte
		err  error
	)
	if p.TokenBody == BodyJSON {
		data, err = s.postJSON(ctx, endpoint, body)
	} else {
		data, err = s.postForm(ctx, endpoint, body)
	}
	if err != nil {
		return nil, err
	}

	var resp struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		AccountID        string `json:"account_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析令牌响应失败: %w", err)
	}

	switch resp.Error {
	case "authorization_pending", "slow_down":
		return nil, ErrPending
	case "access_denied":
		return nil, ErrAccessDenied
	case "":
	default:
		msg := resp.ErrorDescription
		if msg == "" {
			msg = resp.Error
		}
		return nil, fmt.Errorf("授权失败: %s", msg)
	}

	if resp.AccessToken == "" {
		return nil, fmt.Errorf("令牌响应缺少 access_token")
	}

	set := &TokenSet{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		TokenType:    resp.TokenType,
		Account:      resp.AccountID,
	}
	if resp.ExpiresIn > 0 {
		set.ExpiresAt = Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		delete(raw, "access_token")
		delete(raw, "refresh_token")
		delete(raw, "token_type")
		delete(raw, "expires_in")
		if len(raw) > 0 {
			set.Extra = raw
		}
	}
	return set, nil
}

func (s *Sessions) postJSON(ctx context.Context, endpoint string, body map[string]string) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.do(req)
}

func (s *Sessions) postForm(ctx context.Context, endpoint string, body map[string]string) ([]byte, error) {
	values := url.Values{}
	for k, v := range body {
		values.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(req)
}

func (s *Sessions) do(req *http.Request) ([]byte, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return data, fmt.Errorf("上游返回 %d", resp.StatusCode)
	}
	return data, nil
}
