package model

import (
	"encoding/json"
	"time"
)

// 画布导出任务状态，语义同视频工作台：0 排队 / 1 渲染中 / 2 成功 / 3 失败。
const (
	CanvasExportStatusQueued     = 0
	CanvasExportStatusProcessing = 1
	CanvasExportStatusSuccess    = 2
	CanvasExportStatusFailed     = 3
)

// Canvas 画布工作台中的一张画布。
type Canvas struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     uint      `gorm:"column:user_id;not null;index" json:"user_id"`
	Name       string    `gorm:"column:name;size:128;not null;default:''" json:"name"`
	Width      int       `gorm:"column:width;not null;default:0" json:"width"`
	Height     int       `gorm:"column:height;not null;default:0" json:"height"`
	Background string    `gorm:"column:background;size:32;not null;default:''" json:"background"`
	Payload    string    `gorm:"column:payload;type:text;not null" json:"-"`
	Version    int       `gorm:"column:version;not null;default:1" json:"version"`
	CreatedAt  time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

// TableName 表名。
func (Canvas) TableName() string { return "canvases" }

// CanvasExport 一次画布导出（异步任务）。
//
// 导出是 CPU 密集操作，走「排队 → worker 渲染 → 转存 → 前端轮询」的流程，
// 因此状态与结果都要落库，不能只存在内存里。
type CanvasExport struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	CanvasID     uint       `gorm:"column:canvas_id;not null;index" json:"canvas_id"`
	UserID       uint       `gorm:"column:user_id;not null;index" json:"user_id"`
	Format       string     `gorm:"column:format;size:16;not null;default:''" json:"format"`
	Scale        int        `gorm:"column:scale;not null;default:1" json:"scale"`
	Status       int        `gorm:"column:status;not null;default:0" json:"status"`
	Progress     int        `gorm:"column:progress;not null;default:0" json:"progress"`
	StorageKind  string     `gorm:"column:storage_kind;size:16;not null;default:''" json:"storage_kind"`
	StorageKey   string     `gorm:"column:storage_key;size:512;not null;default:''" json:"storage_key"`
	Bytes        int64      `gorm:"column:bytes;not null;default:0" json:"bytes"`
	Width        int        `gorm:"column:width;not null;default:0" json:"width"`
	Height       int        `gorm:"column:height;not null;default:0" json:"height"`
	ErrorMessage string     `gorm:"column:error_message;type:text;not null;default:''" json:"error_message,omitempty"`
	LatencyMS    int        `gorm:"column:latency_ms;not null;default:0" json:"latency_ms"`
	QueuedAt     time.Time  `gorm:"column:queued_at;not null" json:"queued_at"`
	StartedAt    *time.Time `gorm:"column:started_at" json:"started_at,omitempty"`
	FinishedAt   *time.Time `gorm:"column:finished_at" json:"finished_at,omitempty"`
}

// TableName 表名。
func (CanvasExport) TableName() string { return "canvas_exports" }

// Pending 任务是否仍未结束。
func (e *CanvasExport) Pending() bool {
	return e.Status == CanvasExportStatusQueued || e.Status == CanvasExportStatusProcessing
}

// PayloadJSON 把画布内容解析为前端可消费的结构。
//
// 库里存的是原始文本而不是 JSONB：GORM 对 mapstructure 之外的结构支持有限，
// 存文本既便于审计，也避免驱动差异。
func (c *Canvas) PayloadJSON() map[string]interface{} {
	out := map[string]interface{}{}
	if c == nil || c.Payload == "" {
		return out
	}
	if err := json.Unmarshal([]byte(c.Payload), &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}
