package model

import (
	"time"

	"gorm.io/gorm"
)

// KeyGroup API Key 分组：把倍率与限流策略打包成一组预设，供密钥选择。
//
// 分组是"策略预设"而非"权限域"：密钥上的 group_tag 记录分组标识，
// 调用时按该分组叠加倍率系数与 QPM / 并发限制。
//
// 未匹配到分组时（历史遗留的自定义标签、分组被删除或禁用）按中性策略处理，
// 即倍率系数 1.0、不额外限流，避免存量密钥因分组变更而直接不可用。
type KeyGroup struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Name 分组标识（唯一），密钥的 group_tag 记录的就是它。
	Name string `gorm:"size:64;not null;index" json:"name"`
	// DisplayName 展示名，仅用于界面。
	DisplayName string `gorm:"size:64;not null;default:''" json:"display_name"`
	// Description 说明，便于管理员记录该分组用途。
	Description string `gorm:"size:255;not null;default:''" json:"description"`
	// RatioMultiplier 倍率系数：在模型倍率算出的点数上相乘，1.0 表示不加价。
	RatioMultiplier float64 `gorm:"not null;default:1" json:"ratio_multiplier"`
	// QPM 分组级每分钟请求上限，0 表示继续沿用密钥配置与全局默认。
	QPM int `gorm:"not null;default:0" json:"qpm"`
	// Concurrency 分组级并发上限，0 表示继续沿用密钥配置与全局默认。
	Concurrency int `gorm:"not null;default:0" json:"concurrency"`
	// Enabled 是否启用；禁用后归属该分组的密钥按中性策略运行。
	Enabled   bool           `gorm:"not null;default:true" json:"enabled"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名。
func (KeyGroup) TableName() string { return "key_groups" }
