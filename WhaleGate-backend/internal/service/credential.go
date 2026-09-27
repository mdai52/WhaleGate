package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/crypto"
	"github.com/whalegate/whalegate/internal/pkg/oauth"
)

// ListCredentialsFilter 凭证列表筛选条件。
type ListCredentialsFilter struct {
	// Keyword 匹配名称 / 账号 / 供应商 / 文件名。
	Keyword string
	// Status 1 启用 / 2 停用 / 3 失效，0 表示全部。
	Status int
	// Provider 供应商标识，空表示全部。
	Provider string
}

// ListCredentials 分页查询凭证，支持关键字（名称/账号/供应商）与状态筛选。
func (c *Container) ListCredentials(ctx context.Context, f ListCredentialsFilter, page, pageSize int) ([]model.Credential, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.Credential{})
	if f.Provider != "" {
		q = q.Where("provider = ?", f.Provider)
	}
	if f.Status > 0 {
		q = q.Where("status = ?", f.Status)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + strings.ToLower(kw) + "%"
		q = q.Where(
			"LOWER(name) LIKE ? OR LOWER(account) LIKE ? OR LOWER(provider) LIKE ? OR LOWER(file_name) LIKE ?",
			like, like, like, like,
		)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var list []model.Credential
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, total, nil
}

// RenameCredential 重命名凭证。
func (c *Container) RenameCredential(ctx context.Context, id uint, name string) (*model.Credential, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "名称不能为空")
	}
	var cred model.Credential
	if err := c.DB.WithContext(ctx).First(&cred, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	if err := c.DB.WithContext(ctx).Model(&cred).Update("name", truncate(name, 128)).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	cred.Name = truncate(name, 128)
	return &cred, nil
}

// ExportCredential 导出认证文件：按供应商输出标准格式 JSON。
func (c *Container) ExportCredential(ctx context.Context, id uint) (fileName string, payload []byte, err error) {
	cred, err := c.GetCredential(ctx, id)
	if err != nil {
		return "", nil, err
	}
	access, err := crypto.Decrypt(cred.AccessToken, c.encryptionKey())
	if err != nil {
		return "", nil, apierr.Wrap(apierr.ErrCrypto, err)
	}
	var refresh string
	if cred.RefreshToken != "" {
		if refresh, err = crypto.Decrypt(cred.RefreshToken, c.encryptionKey()); err != nil {
			return "", nil, apierr.Wrap(apierr.ErrCrypto, err)
		}
	}

	var out interface{}
	switch cred.Provider {
	case "anthropic":
		entry := map[string]interface{}{
			"accessToken":  access,
			"refreshToken": refresh,
			"scopes":       []string{"user:inference"},
		}
		if cred.ExpiresAt != nil {
			entry["expiresAt"] = cred.ExpiresAt.UnixMilli()
		}
		out = map[string]interface{}{"claudeAiOauth": entry}
	case "codex":
		tokens := map[string]interface{}{"access_token": access, "refresh_token": refresh}
		if cred.Account != "" {
			tokens["account_id"] = cred.Account
		}
		out = map[string]interface{}{"tokens": tokens}
	default:
		entry := map[string]interface{}{
			"provider":      cred.Provider,
			"access_token":  access,
			"refresh_token": refresh,
		}
		if cred.ExpiresAt != nil {
			entry["expires_at"] = cred.ExpiresAt.UnixMilli()
		}
		out = entry
	}

	payload, err = json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", nil, apierr.Wrap(apierr.ErrInternal, err)
	}
	base := cred.Name
	if base == "" {
		base = cred.Provider
	}
	return sanitizeFileName(base) + ".json", payload, nil
}

func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_")
	return replacer.Replace(name)
}

// GetCredential 按 ID 查询凭证。
func (c *Container) GetCredential(ctx context.Context, id uint) (*model.Credential, error) {
	var cred model.Credential
	if err := c.DB.WithContext(ctx).First(&cred, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	return &cred, nil
}

// CreateCredentialFromOAuth 保存 OAuth 流程换取到的令牌。
func (c *Container) CreateCredentialFromOAuth(ctx context.Context, providerID string, set *oauth.TokenSet, name string) (*model.Credential, error) {
	provider, ok := oauth.Get(providerID)
	if !ok {
		return nil, apierr.New(apierr.ErrInvalidParam, "未知的 OAuth 供应商")
	}
	if set == nil || set.AccessToken == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "令牌为空")
	}
	if strings.TrimSpace(name) == "" {
		name = provider.Name
	}

	cipherAccess, err := crypto.Encrypt(set.AccessToken, c.encryptionKey())
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrCrypto, err)
	}
	cipherRefresh := ""
	if set.RefreshToken != "" {
		if cipherRefresh, err = crypto.Encrypt(set.RefreshToken, c.encryptionKey()); err != nil {
			return nil, apierr.Wrap(apierr.ErrCrypto, err)
		}
	}

	var expiresAt *time.Time
	if !set.ExpiresAt.IsZero() {
		expiresAt = &set.ExpiresAt
	}

	cred := &model.Credential{
		Provider:     providerID,
		Name:         truncate(strings.TrimSpace(name), 128),
		Account:      truncate(strings.TrimSpace(set.Account), 256),
		AuthType:     model.CredentialAuthOAuth,
		AccessToken:  cipherAccess,
		RefreshToken: cipherRefresh,
		TokenType:    set.TokenType,
		ExpiresAt:    expiresAt,
		Status:       model.CredentialStatusEnabled,
	}
	if err := c.DB.WithContext(ctx).Create(cred).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return cred, nil
}

// ImportCredentialFile 从上传的认证文件导入凭证。
func (c *Container) ImportCredentialFile(ctx context.Context, fileName string, content []byte, name string) (*model.Credential, error) {
	if len(content) == 0 {
		return nil, apierr.New(apierr.ErrInvalidParam, "认证文件内容为空")
	}
	parsed, err := oauth.ParseCredentialFile(content)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}

	cipherAccess, err := crypto.Encrypt(parsed.AccessToken, c.encryptionKey())
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrCrypto, err)
	}
	cipherRefresh := ""
	if parsed.RefreshToken != "" {
		if cipherRefresh, err = crypto.Encrypt(parsed.RefreshToken, c.encryptionKey()); err != nil {
			return nil, apierr.Wrap(apierr.ErrCrypto, err)
		}
	}

	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = fileName
	}
	if displayName == "" {
		displayName = parsed.Provider + " 凭证"
	}

	cred := &model.Credential{
		Provider:     parsed.Provider,
		Name:         truncate(displayName, 128),
		Account:      truncate(parsed.Account, 256),
		AuthType:     model.CredentialAuthFile,
		AccessToken:  cipherAccess,
		RefreshToken: cipherRefresh,
		TokenType:    parsed.TokenType,
		ExpiresAt:    parsed.ExpiresAt,
		Status:       model.CredentialStatusEnabled,
		FileName:     truncate(fileName, 256),
	}
	if err := c.DB.WithContext(ctx).Create(cred).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return cred, nil
}

// SetCredentialStatus 启用/停用凭证，停用后引用它的渠道自动失效。
func (c *Container) SetCredentialStatus(ctx context.Context, id uint, status int) error {
	if status != model.CredentialStatusEnabled && status != model.CredentialStatusDisabled {
		return apierr.New(apierr.ErrInvalidParam, "状态取值非法")
	}
	res := c.DB.WithContext(ctx).Model(&model.Credential{}).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "凭证不存在")
	}
	c.invalidateChannelCache()
	return nil
}

// DeleteCredential 删除凭证。
func (c *Container) DeleteCredential(ctx context.Context, id uint) error {
	res := c.DB.WithContext(ctx).Delete(&model.Credential{}, id)
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "凭证不存在")
	}
	c.invalidateChannelCache()
	return nil
}

// ChannelUpstreamKey 取渠道调用上游所需的密钥：
// 绑定了凭证时使用凭证令牌（过期自动用 refresh_token 续期），否则使用静态 api_key。
func (c *Container) ChannelUpstreamKey(ctx context.Context, ch *model.Channel) (string, error) {
	if ch == nil {
		return "", apierr.New(apierr.ErrInternal, "渠道为空")
	}
	if ch.CredentialID == nil || *ch.CredentialID == 0 {
		return c.DecryptChannelKey(ch)
	}

	cred, err := c.GetCredential(ctx, *ch.CredentialID)
	if err != nil {
		return "", err
	}
	if cred.Status == model.CredentialStatusDisabled {
		return "", apierr.New(apierr.ErrForbidden, "凭证已停用，请先启用")
	}

	token, err := crypto.Decrypt(cred.AccessToken, c.encryptionKey())
	if err != nil {
		return "", apierr.Wrap(apierr.ErrCrypto, err)
	}

	if cred.ExpiresAt != nil && time.Now().After(*cred.ExpiresAt) {
		refreshed, rErr := c.refreshCredential(ctx, cred)
		if rErr != nil {
			return "", rErr
		}
		token = refreshed
	}
	return token, nil
}

// RefreshCredential 手动触发凭证续期。
func (c *Container) RefreshCredential(ctx context.Context, id uint) (*model.Credential, error) {
	cred, err := c.GetCredential(ctx, id)
	if err != nil {
		return nil, err
	}
	if cred.RefreshToken == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "该凭证没有 refresh_token，无法续期")
	}
	if _, err := c.refreshCredential(ctx, cred); err != nil {
		return nil, err
	}
	return c.GetCredential(ctx, id)
}

// refreshCredential 用 refresh_token 换取新令牌并落库。
func (c *Container) refreshCredential(ctx context.Context, cred *model.Credential) (string, error) {
	refresh, err := crypto.Decrypt(cred.RefreshToken, c.encryptionKey())
	if err != nil {
		return "", apierr.Wrap(apierr.ErrCrypto, err)
	}
	provider, ok := oauth.Get(cred.Provider)
	if !ok || provider.TokenURL == "" {
		return "", apierr.New(apierr.ErrInvalidParam, "该凭证的供应商缺少令牌端点配置，无法自动续期")
	}

	set, err := c.OAuth.Refresh(ctx, provider, refresh)
	if err != nil {
		_ = c.DB.WithContext(ctx).Model(&model.Credential{}).Where("id = ?", cred.ID).
			Updates(map[string]interface{}{"last_error": truncate(err.Error(), 500)})
		return "", apierr.Wrap(apierr.ErrUpstream, err)
	}

	cipherAccess, err := crypto.Encrypt(set.AccessToken, c.encryptionKey())
	if err != nil {
		return "", apierr.Wrap(apierr.ErrCrypto, err)
	}
	updates := map[string]interface{}{
		"access_token":    cipherAccess,
		"last_refresh_at": time.Now(),
		"last_error":      "",
		"status":          model.CredentialStatusEnabled,
	}
	if set.RefreshToken != "" {
		cipherRefresh, err := crypto.Encrypt(set.RefreshToken, c.encryptionKey())
		if err != nil {
			return "", apierr.Wrap(apierr.ErrCrypto, err)
		}
		updates["refresh_token"] = cipherRefresh
	}
	if !set.ExpiresAt.IsZero() {
		updates["expires_at"] = set.ExpiresAt
	}
	if err := c.DB.WithContext(ctx).Model(&model.Credential{}).Where("id = ?", cred.ID).
		Updates(updates).Error; err != nil {
		return "", apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.invalidateChannelCache()
	return set.AccessToken, nil
}

// MarkCredentialInvalid 凭证鉴权失败时标记为失效。
func (c *Container) MarkCredentialInvalid(ctx context.Context, id uint, reason string) {
	if id == 0 {
		return
	}
	if err := c.DB.WithContext(ctx).Model(&model.Credential{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     model.CredentialStatusInvalid,
			"last_error": truncate(reason, 500),
		}).Error; err != nil && c.Logger != nil {
		c.Logger.Warn("标记凭证失效失败", zap.Uint("credential_id", id), zap.Error(err))
	}
}

// EnsureCredentialAvailable 校验渠道绑定的凭证是否可用。
func (c *Container) EnsureCredentialAvailable(ctx context.Context, ch *model.Channel) error {
	if ch == nil || ch.CredentialID == nil || *ch.CredentialID == 0 {
		return nil
	}
	cred, err := c.GetCredential(ctx, *ch.CredentialID)
	if err != nil {
		return err
	}
	if !cred.IsUsable(time.Now()) && cred.RefreshToken == "" {
		return apierr.New(apierr.ErrForbidden, "渠道绑定的凭证已失效，请重新授权")
	}
	return nil
}
