package model

import "time"

// 作品生成状态。
const (
	// ImageWorkStatusSuccess 生成成功。
	ImageWorkStatusSuccess = 1
	// ImageWorkStatusFailed 生成失败，失败原因见 ErrorMessage。
	ImageWorkStatusFailed = 2
)

// ImageAsset 一张生成图片的存储描述。
type ImageAsset struct {
	// Storage 写入时使用的存储后端类型（local / s3）。
	// 随作品保存，管理员切换存储后历史作品仍按原后端回读。
	Storage string `json:"storage,omitempty"`
	// Key 对象键，形如 {workID}/{index}.png。
	Key string `json:"key,omitempty"`
	// URL 上游直接给出的远程地址；仅在转存失败时兜底保留。
	URL string `json:"url,omitempty"`
	// MimeType 图片类型，如 image/png。
	MimeType string `json:"mime_type,omitempty"`
	// RevisedPrompt 上游改写后的提示词。
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// UsesStorage 判断该图片由本服务托管（而非只保留上游地址）。
func (a ImageAsset) UsesStorage() bool { return a.Key != "" }

// ImageWork 一次文生图记录，即作品库中的一件"作品"。
type ImageWork struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// UserID 归属用户。
	UserID uint `gorm:"not null;index" json:"user_id"`
	// APIKeyID 通过 API Key 发起时记录密钥 ID，工作台发起为 0。
	APIKeyID uint `gorm:"not null;default:0" json:"api_key_id"`
	// ChannelID 实际处理的渠道 ID。
	ChannelID uint `gorm:"not null;default:0" json:"channel_id"`
	// Model 对外模型名。
	Model string `gorm:"size:128;not null" json:"model"`
	// Prompt 用户输入的提示词。
	Prompt string `gorm:"type:text;not null" json:"prompt"`
	// Params 生成参数（尺寸、张数、质量等），序列化为 JSON 对象。
	Params map[string]interface{} `gorm:"type:text;serializer:json;not null;default:'{}'" json:"params"`
	// Images 生成结果，序列化为 JSON 数组（元素见 ImageAsset）。
	Images []ImageAsset `gorm:"type:text;serializer:json;not null;default:'[]'" json:"images"`
	// Status 见 ImageWorkStatus* 常量。
	Status int `gorm:"not null;default:1" json:"status"`
	// ErrorMessage 失败原因。
	ErrorMessage string `gorm:"type:text;not null;default:''" json:"error_message,omitempty"`
	// ImageCount 成功生成的图片张数，便于列表页免解析 Images。
	ImageCount int `gorm:"not null;default:0" json:"image_count"`
	// LatencyMS 端到端耗时。
	LatencyMS int `gorm:"not null;default:0" json:"latency_ms"`
	// Points 本次生成实际扣除的点数；自用模式下记录应扣点数，但用户余额不扣减。
	Points int64 `gorm:"not null;default:0" json:"points"`
	// SelfUse 该次生成发生在自用模式下（免费）。
	SelfUse bool `gorm:"not null;default:false" json:"self_use"`
	// Source 来源：studio（工作台）或 api（/v1 接口）。
	Source    string    `gorm:"size:16;not null;default:'studio'" json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (ImageWork) TableName() string { return "image_works" }
