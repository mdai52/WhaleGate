package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/crypto"
)

// WebAuthn 会话有效期。
const webauthnSessionTTL = 5 * time.Minute

// webauthnUser 适配 WebAuthn 库的用户接口。
type webauthnUser struct {
	id          []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

func (u *webauthnUser) WebAuthnID() []byte                         { return u.id }
func (u *webauthnUser) WebAuthnName() string                       { return u.name }
func (u *webauthnUser) WebAuthnDisplayName() string                { return u.displayName }
func (u *webauthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// userHandle 由用户 ID 生成 8 字节 user handle。
func userHandle(userID uint) []byte {
	buf := make([]byte, 8)
	for i := 0; i < 8; i++ {
		buf[7-i] = byte(userID >> (8 * i))
	}
	return buf
}

// handleToUserID 反向解析 user handle。
func handleToUserID(handle []byte) uint {
	var id uint
	for _, b := range handle {
		id = id<<8 | uint(b)
	}
	return id
}

// passkeyEnabled 检查通行密钥是否启用并已初始化。
func (c *Container) passkeyEnabled() (*webauthn.WebAuthn, error) {
	if c.WebAuthn == nil {
		return nil, apierr.New(apierr.ErrForbidden, "通行密钥未启用")
	}
	return c.WebAuthn, nil
}

// loadWebAuthnUser 组装带已有凭证的用户对象。
func (c *Container) loadWebAuthnUser(ctx context.Context, user *model.User) (*webauthnUser, error) {
	credentials, err := c.loadPasskeyCredentials(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	display := user.Nickname
	if display == "" {
		display = user.Username
	}
	return &webauthnUser{
		id:          userHandle(user.ID),
		name:        user.Username,
		displayName: display,
		credentials: credentials,
	}, nil
}

// loadPasskeyCredentials 解密并反序列化用户的通行密钥凭证。
func (c *Container) loadPasskeyCredentials(ctx context.Context, userID uint) ([]webauthn.Credential, error) {
	var rows []model.WebAuthnCredential
	if err := c.DB.WithContext(ctx).Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	out := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		plain, err := crypto.Decrypt(row.PublicKey, c.encryptionKey())
		if err != nil {
			if c.Logger != nil {
				c.Logger.Warn("通行密钥解密失败", zap.Uint("credential_id", row.ID), zap.Error(err))
			}
			continue
		}
		var cred webauthn.Credential
		if err := json.Unmarshal([]byte(plain), &cred); err != nil {
			continue
		}
		out = append(out, cred)
	}
	return out, nil
}

// ---------------------------------------------------------------- 注册（绑定）

// PasskeyRegistrationBegin 开始注册通行密钥，返回给浏览器的 PublicKeyCredentialCreationOptions。
// sessionID 需由前端在 finish 时回传。
func (c *Container) PasskeyRegistrationBegin(ctx context.Context, user *model.User) (sessionID string, options *protocol.CredentialCreation, err error) {
	wa, err := c.passkeyEnabled()
	if err != nil {
		return "", nil, err
	}
	wu, err := c.loadWebAuthnUser(ctx, user)
	if err != nil {
		return "", nil, err
	}

	creation, session, err := wa.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		return "", nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	sessionID = sessionIDFor(user.ID)
	if err := c.saveWebAuthnSession(ctx, "reg:"+sessionID, session); err != nil {
		return "", nil, err
	}
	return sessionID, creation, nil
}

// PasskeyRegistrationFinish 校验并保存新注册的通行密钥。
func (c *Container) PasskeyRegistrationFinish(ctx context.Context, user *model.User, sessionID, name string, raw []byte) (*model.WebAuthnCredential, error) {
	wa, err := c.passkeyEnabled()
	if err != nil {
		return nil, err
	}
	wu, err := c.loadWebAuthnUser(ctx, user)
	if err != nil {
		return nil, err
	}

	session, err := c.takeWebAuthnSession(ctx, "reg:"+sessionID)
	if err != nil {
		return nil, err
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(raw)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	credential, err := wa.CreateCredential(wu, *session, parsed)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}

	payload, err := json.Marshal(credential)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}
	cipher, err := crypto.Encrypt(string(payload), c.encryptionKey())
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrCrypto, err)
	}
	transports, _ := json.Marshal(credential.Transport)

	displayName := name
	if displayName == "" {
		displayName = "通行密钥"
	}
	row := &model.WebAuthnCredential{
		UserID:       user.ID,
		Name:         displayName,
		CredentialID: base64.RawURLEncoding.EncodeToString(credential.ID),
		PublicKey:    cipher,
		AAGUID:       base64.RawURLEncoding.EncodeToString(credential.Authenticator.AAGUID),
		SignCount:    int64(credential.Authenticator.SignCount),
		Transports:   string(transports),
	}
	if err := c.DB.WithContext(ctx).Create(row).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return row, nil
}

// ---------------------------------------------------------------- 登录

// PasskeyLoginBegin 开始无用户名（可发现凭证）登录。
func (c *Container) PasskeyLoginBegin(ctx context.Context) (sessionID string, options *protocol.CredentialAssertion, err error) {
	wa, err := c.passkeyEnabled()
	if err != nil {
		return "", nil, err
	}
	assertion, session, err := wa.BeginDiscoverableLogin()
	if err != nil {
		return "", nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	sessionID = fmt.Sprintf("login:%d", time.Now().UnixNano())
	if err := c.saveWebAuthnSession(ctx, sessionID, session); err != nil {
		return "", nil, err
	}
	return sessionID, assertion, nil
}

// PasskeyLoginFinish 校验断言并完成登录。
func (c *Container) PasskeyLoginFinish(ctx context.Context, sessionID string, raw []byte) (*LoginResult, error) {
	wa, err := c.passkeyEnabled()
	if err != nil {
		return nil, err
	}
	session, err := c.takeWebAuthnSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(raw)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}

	waUser, credential, err := wa.ValidatePasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		userID := handleToUserID(userHandle)
		if userID == 0 {
			return nil, fmt.Errorf("无效的 user handle")
		}
		var user model.User
		if err := c.DB.WithContext(ctx).First(&user, userID).Error; err != nil {
			return nil, fmt.Errorf("用户不存在")
		}
		return c.loadWebAuthnUser(ctx, &user)
	}, *session, parsed)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidCredential, err)
	}

	wu, ok := waUser.(*webauthnUser)
	if !ok {
		return nil, apierr.New(apierr.ErrInternal, "凭证校验结果异常")
	}
	userID := handleToUserID(wu.WebAuthnID())

	var user model.User
	if err := c.DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !user.IsEnabled() {
		return nil, apierr.New(apierr.ErrUserDisabled, "账号已被禁用，请联系管理员")
	}

	// 更新签名计数与最近使用时间
	c.updatePasskeyUsage(ctx, userID, credential.ID, credential.Authenticator.SignCount)
	go func(id uint) {
		bg, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = c.DB.WithContext(bg).Model(&model.User{}).Where("id = ?", id).
			Update("last_login_at", time.Now()).Error
	}(user.ID)

	token, err := c.JWT.Generate(jwtClaimsFor(&user))
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}
	return &LoginResult{
		Token:     token,
		ExpiresIn: int64(c.JWT.TTL().Seconds()),
		User:      &user,
	}, nil
}

func (c *Container) updatePasskeyUsage(ctx context.Context, userID uint, credentialID []byte, signCount uint32) {
	encoded := base64.RawURLEncoding.EncodeToString(credentialID)
	_ = c.DB.WithContext(ctx).Model(&model.WebAuthnCredential{}).
		Where("user_id = ? AND credential_id = ?", userID, encoded).
		Updates(map[string]interface{}{"sign_count": signCount, "last_used_at": time.Now()}).Error
}

// ---------------------------------------------------------------- 管理

// ListPasskeys 列出用户的通行密钥。
func (c *Container) ListPasskeys(ctx context.Context, userID uint) ([]model.WebAuthnCredential, error) {
	var rows []model.WebAuthnCredential
	if err := c.DB.WithContext(ctx).Where("user_id = ?", userID).
		Order("id DESC").Find(&rows).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return rows, nil
}

// DeletePasskey 删除用户的通行密钥。
func (c *Container) DeletePasskey(ctx context.Context, userID uint, id uint) error {
	res := c.DB.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.WebAuthnCredential{})
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "通行密钥不存在")
	}
	return nil
}

// ---------------------------------------------------------------- 会话存储

func sessionIDFor(userID uint) string {
	return fmt.Sprintf("%d", userID)
}

func (c *Container) webAuthnKey(id string) string {
	return fmt.Sprintf("%s:wa:%s", c.prefix(), id)
}

func (c *Container) saveWebAuthnSession(ctx context.Context, id string, session *webauthn.SessionData) error {
	data, err := json.Marshal(session)
	if err != nil {
		return apierr.Wrap(apierr.ErrInternal, err)
	}
	if err := c.RDB.Set(ctx, c.webAuthnKey(id), data, webauthnSessionTTL).Err(); err != nil {
		return apierr.Wrap(apierr.ErrCache, err)
	}
	return nil
}

// takeWebAuthnSession 读取并立即删除会话（防重放）。
func (c *Container) takeWebAuthnSession(ctx context.Context, id string) (*webauthn.SessionData, error) {
	key := c.webAuthnKey(id)
	data, err := c.RDB.GetDel(ctx, key).Bytes()
	if err != nil || len(data) == 0 {
		return nil, apierr.New(apierr.ErrInvalidParam, "通行密钥会话已过期，请重新发起")
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}
	return &session, nil
}
