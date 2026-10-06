package model

import "time"

// 视频脚本生成状态，语义同其它工作台：1 成功 / 2 失败。
const (
	VideoScriptStatusSuccess = 1
	VideoScriptStatusFailed  = 2
)

// 视频生成任务状态。视频是异步任务，状态比图片多两档。
const (
	// VideoWorkStatusQueued 已提交给上游，排队中。
	VideoWorkStatusQueued = 0
	// VideoWorkStatusProcessing 上游正在生成。
	VideoWorkStatusProcessing = 1
	// VideoWorkStatusSuccess 生成成功，内容已转存。
	VideoWorkStatusSuccess = 2
	// VideoWorkStatusFailed 生成失败或超时，原因见 ErrorMessage。
	VideoWorkStatusFailed = 3
)

// VideoScriptScene 分镜脚本中的一镜。
type VideoScriptScene struct {
	// Index 镜头序号，从 1 开始。
	Index int `json:"index"`
	// Title 镜头小标题。
	Title string `json:"title,omitempty"`
	// Visual 画面内容描述。
	Visual string `json:"visual"`
	// Narration 旁白或台词。
	Narration string `json:"narration,omitempty"`
	// Camera 运镜方式，如「推镜头 / 固定 / 侧移」。
	Camera string `json:"camera,omitempty"`
	// Seconds 建议时长（秒）。
	Seconds int `json:"seconds"`
	// Prompt 可直接送入视频生成模型的画面提示词。
	Prompt string `json:"prompt"`
}

// VideoScript 一次脚本创作的结果。
type VideoScript struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// UserID 归属用户。
	UserID uint `gorm:"not null;index" json:"user_id"`
	// APIKeyID 通过 API Key 发起时记录密钥 ID，工作台发起为 0。
	APIKeyID uint `gorm:"not null;default:0" json:"api_key_id"`
	// ChannelID 实际处理的渠道 ID。
	ChannelID uint `gorm:"not null;default:0" json:"channel_id"`
	// Model 生成脚本所用的对话模型。
	Model string `gorm:"size:128;not null;default:''" json:"model"`
	// Title 脚本标题，由模型生成或取自主题。
	Title string `gorm:"size:255;not null;default:''" json:"title"`
	// Brief 创作需求：主题、风格、受众等。
	Brief string `gorm:"type:text;not null;default:''" json:"brief"`
	// Params 生成参数：style / audience / duration / scenes。
	Params map[string]interface{} `gorm:"type:text;serializer:json;not null;default:'{}'" json:"params"`
	// Script 分镜列表，序列化为 JSON 数组（元素见 VideoScriptScene）。
	Script []VideoScriptScene `gorm:"type:text;serializer:json;not null;default:'[]'" json:"script"`
	// SceneCount 镜头数，便于列表页免解析 Script。
	SceneCount int `gorm:"not null;default:0" json:"scene_count"`
	// TotalSeconds 建议总时长（秒）。
	TotalSeconds int `gorm:"not null;default:0" json:"total_seconds"`
	// Status 见 VideoScriptStatus* 常量。
	Status int `gorm:"not null;default:1" json:"status"`
	// ErrorMessage 失败原因。
	ErrorMessage string `gorm:"type:text;not null;default:''" json:"error_message,omitempty"`
	// Points 本次脚本生成扣除的点数（按 token 计费）。
	Points int64 `gorm:"not null;default:0" json:"points"`
	// SelfUse 是否发生在自用模式下（免费）。
	SelfUse   bool      `gorm:"not null;default:false" json:"self_use"`
	LatencyMS int       `gorm:"not null;default:0" json:"latency_ms"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (VideoScript) TableName() string { return "video_scripts" }

// VideoAsset 一段已生成视频的存储描述。
type VideoAsset struct {
	// Storage 写入时使用的存储后端类型（local / s3）。
	// 随作品保存，管理员切换存储后历史作品仍按原后端回读。
	Storage string `json:"storage,omitempty"`
	// Key 对象键，形如 video/{workID}/0.mp4。
	Key string `json:"key,omitempty"`
	// URL 上游直接给出的远程地址；仅在转存失败时兜底保留。
	URL string `json:"url,omitempty"`
	// MimeType 视频类型，如 video/mp4。
	MimeType string `json:"mime_type,omitempty"`
	// DurationSec 该片段时长（秒）。
	DurationSec int `json:"duration_sec,omitempty"`
}

// UsesStorage 判断该视频由本服务托管（而非只保留上游地址）。
func (a VideoAsset) UsesStorage() bool { return a.Key != "" }

// VideoWork 一次视频生成任务，即作品库中的一件作品。
type VideoWork struct {
	ID        uint `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint `gorm:"not null;index" json:"user_id"`
	APIKeyID  uint `gorm:"not null;default:0" json:"api_key_id"`
	ChannelID uint `gorm:"not null;default:0" json:"channel_id"`
	// Model 对外模型名。
	Model string `gorm:"size:128;not null;default:''" json:"model"`
	// UpstreamModel 经别名/映射改写后实际发往上游的模型名。
	UpstreamModel string `gorm:"size:128;not null;default:''" json:"upstream_model"`
	// Prompt 画面提示词。
	Prompt string `gorm:"type:text;not null;default:''" json:"prompt"`
	// NegativePrompt 不希望出现的元素。
	NegativePrompt string `gorm:"type:text;not null;default:''" json:"negative_prompt"`
	// Params 生成参数（秒数、画幅、帧率等），序列化为 JSON 对象。
	Params map[string]interface{} `gorm:"type:text;serializer:json;not null;default:'{}'" json:"params"`
	// Videos 生成结果，序列化为 JSON 数组（元素见 VideoAsset）。
	Videos []VideoAsset `gorm:"type:text;serializer:json;not null;default:'[]'" json:"videos"`
	// Status 见 VideoWorkStatus* 常量。
	Status int `gorm:"not null;default:0;index" json:"status"`
	// TaskID 上游任务 ID，用于轮询与取内容；同步返回的上游为空。
	TaskID string `gorm:"size:128;not null;default:''" json:"task_id"`
	// Progress 上游上报的进度百分比，0-100。
	Progress int `gorm:"not null;default:0" json:"progress"`
	// DurationSec 实际生成视频的秒数，按秒计费的计量单位。
	DurationSec int `gorm:"not null;default:0" json:"duration_sec"`
	// ErrorMessage 失败原因。
	ErrorMessage string `gorm:"type:text;not null;default:''" json:"error_message,omitempty"`
	// Points 本次生成扣除的点数；自用模式下记录应扣点数但不扣余额。
	Points int64 `gorm:"not null;default:0" json:"points"`
	// SelfUse 是否发生在自用模式下（免费）。
	SelfUse bool `gorm:"not null;default:false" json:"self_use"`
	// Source 来源：studio（工作台）或 api（/v1/videos）。
	Source    string    `gorm:"size:16;not null;default:'studio'" json:"source"`
	LatencyMS int       `gorm:"not null;default:0" json:"latency_ms"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (VideoWork) TableName() string { return "video_works" }

// VideoWorkStatusText 任务状态转文案，便于前端直接展示。
func VideoWorkStatusText(status int) string {
	switch status {
	case VideoWorkStatusQueued:
		return "排队中"
	case VideoWorkStatusProcessing:
		return "生成中"
	case VideoWorkStatusSuccess:
		return "已完成"
	case VideoWorkStatusFailed:
		return "失败"
	default:
		return "未知"
	}
}
