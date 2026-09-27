package model

import (
	"encoding/json"
	"time"
)

// 模型目录来源。
const (
	CatalogSourceDetect = "detect"
	CatalogSourceManual = "manual"
)

// ModelCatalog 全局模型目录：自动探测与手动登记的模型元数据，
// 供渠道配置与倍率配置直接下拉选择，避免手填模型名。
type ModelCatalog struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ModelID     string `gorm:"size:128;not null;uniqueIndex" json:"model_id"`
	DisplayName string `gorm:"size:256;not null;default:''" json:"display_name"`
	// ProviderType 探测时的协议类型：openai / anthropic / claude-code / codex / gemini。
	ProviderType string `gorm:"size:32;not null;default:''" json:"provider_type"`
	// Capabilities 能力集合（JSON 数组文本）。
	Capabilities  string    `gorm:"type:text;not null;default:'[]'" json:"capabilities"`
	ContextWindow int       `gorm:"not null;default:0" json:"context_window"`
	MaxOutput     int       `gorm:"not null;default:0" json:"max_output"`
	Source        string    `gorm:"size:16;not null;default:'detect'" json:"source"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TableName 表名。
func (ModelCatalog) TableName() string { return "model_catalog" }

// SetCapabilities 序列化能力集合。
func (m *ModelCatalog) SetCapabilities(caps []string) error {
	if len(caps) == 0 {
		m.Capabilities = "[]"
		return nil
	}
	data, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	m.Capabilities = string(data)
	return nil
}

// CapabilityList 解析能力集合。
func (m *ModelCatalog) CapabilityList() []string {
	if m == nil || m.Capabilities == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(m.Capabilities), &out); err != nil {
		return nil
	}
	return out
}
