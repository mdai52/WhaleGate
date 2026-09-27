// Package model 定义与数据库表一一对应的 GORM 模型。
package model

import (
	"time"

	"gorm.io/gorm"
)

// User 用户（支持管理端账号与门户账号）。
type User struct {
	ID           uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string `gorm:"size:64;not null;uniqueIndex" json:"username"`
	Email        string `gorm:"size:128;uniqueIndex" json:"email,omitempty"`
	PasswordHash string `gorm:"size:128;not null" json:"-"`
	Nickname     string `gorm:"size:64" json:"nickname,omitempty"`
	Role         string `gorm:"size:32;not null;default:user" json:"role"`
	Status       int    `gorm:"not null;default:1" json:"status"`
	// MustChangePassword 首次登录（或管理员重置密码后）必须修改密码。
	MustChangePassword bool           `gorm:"not null;default:false" json:"must_change_password"`
	Quota              int64          `gorm:"not null;default:0" json:"quota"`
	UsedQuota          int64          `gorm:"not null;default:0" json:"used_quota"`
	RequestCount       int64          `gorm:"not null;default:0" json:"request_count"`
	LastLoginAt        *time.Time     `json:"last_login_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名。
func (User) TableName() string { return "users" }

// IsAdmin 是否管理员。
func (u *User) IsAdmin() bool { return u.Role == "admin" }

// IsEnabled 是否启用。
func (u *User) IsEnabled() bool { return u.Status == 1 }
