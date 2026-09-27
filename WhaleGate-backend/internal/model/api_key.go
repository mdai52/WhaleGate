package model

import (
	"time"

	"gorm.io/gorm"
)

// APIKey 用户凭证。明文仅在创建时返回一次，库中只保存哈希。
type APIKey struct {
	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint       `gorm:"not null;index" json:"user_id"`
	Name        string     `gorm:"size:64;not null" json:"name"`
	KeyHash     string     `gorm:"size:128;not null;uniqueIndex" json:"-"`
	Prefix      string     `gorm:"size:16;not null" json:"prefix"`
	MaskedKey   string     `gorm:"size:64;not null" json:"masked_key"`
	Status      int        `gorm:"not null;default:1" json:"status"`
	QPM         int        `gorm:"not null;default:0" json:"qpm"`
	Concurrency int        `gorm:"not null;default:0" json:"concurrency"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	// RequestCount 累计请求次数，转发结算时累加。
	RequestCount int64 `gorm:"not null;default:0" json:"request_count"`
	// TotalTokens 累计 token 消耗。
	TotalTokens int64          `gorm:"not null;default:0" json:"total_tokens"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名。
func (APIKey) TableName() string { return "api_keys" }

// IsRevoked 是否已吊销。
func (k *APIKey) IsRevoked() bool { return k.RevokedAt != nil }

// IsExpired 是否已过期（未设置过期时间视为永不过期）。
func (k *APIKey) IsExpired(now time.Time) bool {
	return k.ExpiresAt != nil && k.ExpiresAt.Before(now)
}

// IsUsable 是否可用于鉴权。
func (k *APIKey) IsUsable(now time.Time) bool {
	if k.Status != 1 {
		return false
	}
	return !k.IsRevoked() && !k.IsExpired(now)
}
