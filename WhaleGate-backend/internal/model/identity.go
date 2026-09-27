package model

import (
	"time"

	"gorm.io/gorm"
)

// 第三方身份提供方。
const (
	IdentityProviderGitHub = "github"
	// 通行密钥不落在 identity 表，单独用 webauthn_credentials。
	IdentityProviderPasskey = "passkey"
)

// UserIdentity 用户与第三方账号的绑定关系。
type UserIdentity struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID   uint   `gorm:"not null;index" json:"user_id"`
	Provider string `gorm:"size:64;not null" json:"provider"`
	// ProviderUID 第三方账号唯一 ID。
	ProviderUID string `gorm:"size:128;not null" json:"provider_uid"`
	Email       string `gorm:"size:256" json:"email,omitempty"`
	DisplayName string `gorm:"size:256" json:"display_name,omitempty"`
	// AccessToken 第三方访问令牌密文。
	AccessToken string         `gorm:"type:text" json:"-"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名。
func (UserIdentity) TableName() string { return "user_identities" }

// WebAuthnCredential 通行密钥凭证。
type WebAuthnCredential struct {
	ID           uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint   `gorm:"not null;index" json:"user_id"`
	Name         string `gorm:"size:128;not null" json:"name"`
	CredentialID string `gorm:"type:text;not null" json:"credential_id"`
	// PublicKey COSE 公钥 JSON 的密文。
	PublicKey  string    `gorm:"type:text;not null" json:"-"`
	AAGUID     string    `gorm:"size:64" json:"aaguid,omitempty"`
	SignCount  int64     `gorm:"not null;default:0" json:"sign_count"`
	Transports string    `gorm:"type:text" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	// LastUsedAt 最近一次用于登录的时间。
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// TableName 表名。
func (WebAuthnCredential) TableName() string { return "webauthn_credentials" }
