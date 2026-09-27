package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// 凭证状态。
const (
	CredentialStatusEnabled  = 1
	CredentialStatusDisabled = 2
	CredentialStatusInvalid  = 3
)

// 凭证类型。
const (
	CredentialAuthOAuth = "oauth"
	CredentialAuthFile  = "file"
	CredentialAuthKey   = "api_key"
)

// Credential OAuth 凭证（认证文件）。access_token / refresh_token 加密存储。
type Credential struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Provider string `gorm:"size:64;not null;index" json:"provider"`
	Name     string `gorm:"size:128;not null" json:"name"`
	Account  string `gorm:"size:256" json:"account,omitempty"`
	AuthType string `gorm:"size:32;not null;default:oauth" json:"auth_type"`
	// AccessToken AES-256-GCM 密文。
	AccessToken string `gorm:"type:text" json:"-"`
	// RefreshToken AES-256-GCM 密文。
	RefreshToken string     `gorm:"type:text" json:"-"`
	TokenType    string     `gorm:"size:32" json:"token_type,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Status       int        `gorm:"not null;default:1" json:"status"`
	// Quota 配额快照 JSON（上游可提供时刷新）。
	Quota         string         `gorm:"type:text" json:"quota"`
	LastRefreshAt *time.Time     `json:"last_refresh_at,omitempty"`
	LastError     string         `gorm:"type:text" json:"last_error,omitempty"`
	FileName      string         `gorm:"size:256" json:"file_name,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名。
func (Credential) TableName() string { return "credentials" }

// IsUsable 凭证当前是否可用于鉴权。
func (c *Credential) IsUsable(now time.Time) bool {
	if c == nil || c.Status != CredentialStatusEnabled {
		return false
	}
	if c.AccessToken == "" {
		return false
	}
	if c.ExpiresAt != nil && c.ExpiresAt.Before(now) {
		return false
	}
	return true
}

// QuotaMap 解析配额快照。
func (c *Credential) QuotaMap() map[string]interface{} {
	if c == nil || c.Quota == "" {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(c.Quota), &out); err != nil {
		return nil
	}
	return out
}

// SetQuota 序列化配额快照。
func (c *Credential) SetQuota(m map[string]interface{}) error {
	if len(m) == 0 {
		c.Quota = "{}"
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.Quota = string(data)
	return nil
}
