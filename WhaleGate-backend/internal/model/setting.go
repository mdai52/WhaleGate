package model

import "time"

// 全局设置键名。
const (
	// SettingSelfUseMode 自用模式：开启后所有调用不计费。
	SettingSelfUseMode = "self_use_mode"
)

// AppSetting 全局运行时设置，value 存放 JSON 文本。
type AppSetting struct {
	Key       string    `gorm:"column:setting_key;primaryKey;size:64" json:"key"`
	Value     string    `gorm:"column:value;type:text" json:"value"`
	UpdatedBy uint      `gorm:"column:updated_by" json:"updated_by,omitempty"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// TableName 表名。
func (AppSetting) TableName() string { return "app_settings" }
