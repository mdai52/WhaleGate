package model

import "time"

// Skill 注入模式。
const (
	// SkillModeAlways 正文全量注入 system 提示词。
	SkillModeAlways = "always"
	// SkillModeIndex 仅注入名称与描述索引，由模型按需请求全文。
	SkillModeIndex = "index"
)

// Skill 一个可注入的技能。
type Skill struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string `gorm:"size:128;not null;uniqueIndex" json:"name"`
	DisplayName string `gorm:"size:256;not null;default:''" json:"display_name"`
	Description string `gorm:"type:text;not null;default:''" json:"description"`
	// Content SKILL.md 正文（已去除 frontmatter）。
	Content string `gorm:"type:text;not null;default:''" json:"content"`
	// Mode 注入模式：always / index。
	Mode    string `gorm:"size:16;not null;default:'always'" json:"mode"`
	Enabled bool   `gorm:"not null;default:true" json:"enabled"`
	// Source dir=目录扫描 / manual=手动登记。
	Source    string    `gorm:"size:16;not null;default:'dir'" json:"source"`
	FilePath  string    `gorm:"size:512;not null;default:''" json:"file_path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (Skill) TableName() string { return "skills" }
