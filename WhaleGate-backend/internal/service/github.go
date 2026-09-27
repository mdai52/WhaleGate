package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/crypto"
)

// GitHub OAuth 端点。
const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubUserAPI      = "https://api.github.com/user"
)

// ticketTTL 一次性登录票据有效期。
const ticketTTL = 60 * time.Second

// githubStateTTL GitHub OAuth state 有效期。
const githubStateTTL = 10 * time.Minute

// GitHubEnabled 是否启用了 GitHub 登录。
func (c *Container) GitHubEnabled() bool {
	return c.Config != nil && c.Config.Auth.GitHub.Enabled && c.Config.Auth.GitHub.ClientID != ""
}

// GitHubAuthorizeURL 生成 GitHub 授权地址并记录 state（防 CSRF）。
// mode 为 "bind"（已登录绑定）或 "login"（第三方登录），userID 在 bind 模式下记录。
func (c *Container) GitHubAuthorizeURL(ctx context.Context, mode string, userID uint) (string, error) {
	if !c.GitHubEnabled() {
		return "", apierr.New(apierr.ErrForbidden, "未启用 GitHub 登录")
	}

	state := uuid.NewString()
	if err := c.RDB.Set(ctx, c.githubStateKey(state), fmt.Sprintf("%s:%d", mode, userID), githubStateTTL).Err(); err != nil {
		return "", apierr.Wrap(apierr.ErrCache, err)
	}

	q := url.Values{}
	q.Set("client_id", c.Config.Auth.GitHub.ClientID)
	q.Set("state", state)
	q.Set("allow_signup", "true")
	if len(c.Config.Auth.GitHub.Scopes) > 0 {
		q.Set("scope", strings.Join(c.Config.Auth.GitHub.Scopes, " "))
	}
	return githubAuthorizeURL + "?" + q.Encode(), nil
}

// consumeGitHubState 校验并消费 state，返回其中的模式与用户 ID。
func (c *Container) consumeGitHubState(ctx context.Context, state string) (mode string, userID uint, err error) {
	if state == "" {
		return "", 0, apierr.New(apierr.ErrInvalidParam, "缺少 state 参数")
	}
	raw, err := c.RDB.GetDel(ctx, c.githubStateKey(state)).Result()
	if err != nil || raw == "" {
		return "", 0, apierr.New(apierr.ErrInvalidParam, "state 无效或已过期，请重新发起")
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", 0, apierr.New(apierr.ErrInvalidParam, "state 内容异常")
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &userID); err != nil {
		userID = 0
	}
	return parts[0], userID, nil
}

// githubUser GitHub 返回的用户信息。
type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// exchangeGitHubCode 用授权码换取访问令牌并拉取用户信息。
func (c *Container) exchangeGitHubCode(ctx context.Context, code string) (string, *githubUser, error) {
	cfg := c.Config.Auth.GitHub
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", nil, apierr.Wrap(apierr.ErrInternal, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", nil, apierr.Wrap(apierr.ErrUpstream, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= http.StatusBadRequest {
		return "", nil, apierr.Errorf(apierr.ErrUpstream, "GitHub 返回 %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", nil, apierr.Wrap(apierr.ErrUpstreamInvalid, err)
	}
	if tokenResp.AccessToken == "" {
		msg := tokenResp.Error
		if msg == "" {
			msg = "GitHub 未返回访问令牌"
		}
		return "", nil, apierr.Errorf(apierr.ErrUpstream, "%s", msg)
	}

	profile, err := c.fetchGitHubUser(ctx, tokenResp.AccessToken)
	if err != nil {
		return "", nil, err
	}
	return tokenResp.AccessToken, profile, nil
}

func (c *Container) fetchGitHubUser(ctx context.Context, token string) (*githubUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserAPI, nil)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrUpstream, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, apierr.Errorf(apierr.ErrUpstream, "GitHub 用户信息接口返回 %d", resp.StatusCode)
	}
	profile := &githubUser{}
	if err := json.Unmarshal(body, profile); err != nil {
		return nil, apierr.Wrap(apierr.ErrUpstreamInvalid, err)
	}
	if profile.ID == 0 {
		return nil, apierr.New(apierr.ErrUpstreamInvalid, "GitHub 用户信息缺少 id")
	}
	return profile, nil
}

// BindGitHub 把 GitHub 账号绑定到当前登录用户。
func (c *Container) BindGitHub(ctx context.Context, userID uint, code, state string) (*model.UserIdentity, error) {
	if !c.GitHubEnabled() {
		return nil, apierr.New(apierr.ErrForbidden, "未启用 GitHub 登录")
	}
	mode, stateUserID, err := c.consumeGitHubState(ctx, state)
	if err != nil {
		return nil, err
	}
	if mode == "bind" && stateUserID > 0 && stateUserID != userID {
		return nil, apierr.New(apierr.ErrForbidden, "state 与当前用户不匹配")
	}

	token, profile, err := c.exchangeGitHubCode(ctx, code)
	if err != nil {
		return nil, err
	}
	cipher, err := crypto.Encrypt(token, c.encryptionKey())
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrCrypto, err)
	}

	uid := fmt.Sprintf("%d", profile.ID)
	var existing model.UserIdentity
	err = c.DB.WithContext(ctx).
		Where("provider = ? AND provider_uid = ?", model.IdentityProviderGitHub, uid).
		First(&existing).Error
	if err == nil {
		if existing.UserID != userID {
			return nil, apierr.New(apierr.ErrConflict, "该 GitHub 账号已绑定到其它账号")
		}
		if err := c.DB.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
			"email":        profile.Email,
			"display_name": displayNameOf(profile),
			"access_token": cipher,
		}).Error; err != nil {
			return nil, apierr.Wrap(apierr.ErrDatabase, err)
		}
		existing.Email = profile.Email
		existing.DisplayName = displayNameOf(profile)
		return &existing, nil
	}

	identity := &model.UserIdentity{
		UserID:      userID,
		Provider:    model.IdentityProviderGitHub,
		ProviderUID: uid,
		Email:       profile.Email,
		DisplayName: displayNameOf(profile),
		AccessToken: cipher,
	}
	if err := c.DB.WithContext(ctx).Create(identity).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, apierr.New(apierr.ErrConflict, "该 GitHub 账号已绑定")
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return identity, nil
}

// LoginWithGitHub 使用已绑定的 GitHub 账号登录，返回一次性票据（由前端换取令牌）。
func (c *Container) LoginWithGitHub(ctx context.Context, code, state string) (string, error) {
	if !c.GitHubEnabled() {
		return "", apierr.New(apierr.ErrForbidden, "未启用 GitHub 登录")
	}
	if _, _, err := c.consumeGitHubState(ctx, state); err != nil {
		return "", err
	}

	_, profile, err := c.exchangeGitHubCode(ctx, code)
	if err != nil {
		return "", err
	}

	var identity model.UserIdentity
	err = c.DB.WithContext(ctx).
		Where("provider = ? AND provider_uid = ?", model.IdentityProviderGitHub, fmt.Sprintf("%d", profile.ID)).
		First(&identity).Error
	if err != nil {
		return "", apierr.New(apierr.ErrNotFound, "该 GitHub 账号尚未绑定，请先登录后前往用户中心绑定")
	}

	var user model.User
	if err := c.DB.WithContext(ctx).First(&user, identity.UserID).Error; err != nil {
		return "", apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !user.IsEnabled() {
		return "", apierr.New(apierr.ErrUserDisabled, "账号已被禁用，请联系管理员")
	}

	token, err := c.JWT.Generate(jwtClaimsFor(&user))
	if err != nil {
		return "", apierr.Wrap(apierr.ErrInternal, err)
	}
	return c.issueTicket(ctx, token)
}

// issueTicket 把令牌封装为一次性票据，避免出现在浏览器地址栏。
func (c *Container) issueTicket(ctx context.Context, token string) (string, error) {
	ticket := uuid.NewString()
	if err := c.RDB.Set(ctx, c.ticketKey(ticket), token, ticketTTL).Err(); err != nil {
		return "", apierr.Wrap(apierr.ErrCache, err)
	}
	return ticket, nil
}

// ExchangeTicket 用一次性票据换取登录令牌（票据用完即焚）。
func (c *Container) ExchangeTicket(ctx context.Context, ticket string) (string, error) {
	if ticket == "" {
		return "", apierr.New(apierr.ErrInvalidParam, "缺少票据")
	}
	token, err := c.RDB.GetDel(ctx, c.ticketKey(ticket)).Result()
	if err != nil || token == "" {
		return "", apierr.New(apierr.ErrInvalidParam, "票据无效或已过期")
	}
	return token, nil
}

// ---------------------------------------------------------------- 绑定管理

// ListIdentities 列出用户的第三方绑定。
func (c *Container) ListIdentities(ctx context.Context, userID uint) ([]model.UserIdentity, error) {
	var rows []model.UserIdentity
	if err := c.DB.WithContext(ctx).Where("user_id = ?", userID).
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return rows, nil
}

// UnbindIdentity 解除第三方绑定。若该账号仅剩第三方登录方式会给出提示。
func (c *Container) UnbindIdentity(ctx context.Context, userID uint, id uint) error {
	var identity model.UserIdentity
	if err := c.DB.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).
		First(&identity).Error; err != nil {
		return apierr.Wrap(apierr.ErrNotFound, err)
	}
	res := c.DB.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.UserIdentity{})
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "绑定不存在")
	}
	return nil
}

func displayNameOf(profile *githubUser) string {
	if profile.Name != "" {
		return profile.Name
	}
	return profile.Login
}

func (c *Container) githubStateKey(state string) string {
	return fmt.Sprintf("%s:ghstate:%s", c.prefix(), state)
}

func (c *Container) ticketKey(ticket string) string {
	return fmt.Sprintf("%s:ticket:%s", c.prefix(), ticket)
}
