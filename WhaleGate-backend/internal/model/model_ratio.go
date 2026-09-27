package model

import "time"

// ModelRatio 模型倍率表。
// 单位：点 / 1K tokens（1 点 = 0.001 元，见 constant.PointsPerYuan）。
type ModelRatio struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Model 对外模型名（支持通配符 "*" 作为兜底）。
	Model string `gorm:"size:128;not null;uniqueIndex" json:"model"`
	// PromptRatio 每千 prompt token 的点数。
	PromptRatio float64 `gorm:"not null;default:0" json:"prompt_ratio"`
	// CompletionRatio 每千 completion token 的点数（含思维链）。
	CompletionRatio float64    `gorm:"not null;default:0" json:"completion_ratio"`
	Enabled         bool       `gorm:"not null;default:true" json:"enabled"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"-"`
}

// TableName 表名。
func (ModelRatio) TableName() string { return "model_ratios" }
