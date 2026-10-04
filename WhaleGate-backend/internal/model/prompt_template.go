package model

import "time"

// PromptTemplate 绘图工作台的提示词模板（词库条目）。
type PromptTemplate struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Category 分类键，取值见 PromptCategories。
	Category string `gorm:"size:32;not null;index" json:"category"`
	// Title 标题。
	Title string `gorm:"size:128;not null" json:"title"`
	// Summary 摘要，用于卡片副标题。
	Summary string `gorm:"size:255;not null;default:''" json:"summary"`
	// Prompt 提示词正文，可直接送入生图接口。
	Prompt string `gorm:"type:text;not null" json:"prompt"`
	// Tags 标签，序列化为 JSON 数组存储。
	Tags []string `gorm:"type:text;serializer:json;not null;default:'[]'" json:"tags"`
	// ModelHint 推荐模型名，仅作展示与默认值，不做强校验。
	ModelHint string `gorm:"size:64;not null;default:''" json:"model_hint"`
	// AspectRatio 推荐画幅，如 1:1 / 9:16 / 16:9。
	AspectRatio string `gorm:"size:16;not null;default:''" json:"aspect_ratio"`
	// Sort 排序权重，越大越靠前。
	Sort int `gorm:"not null;default:0" json:"sort"`
	// Enabled 是否对外可见。
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (PromptTemplate) TableName() string { return "prompt_templates" }

// PromptCategory 词库分类的展示定义。
type PromptCategory struct {
	// Key 分类键，与 PromptTemplate.Category 对应。
	Key string
	// Label 展示名。
	Label string
}

// PromptCategories 词库分类，顺序即前端展示顺序。
var PromptCategories = []PromptCategory{
	{Key: "portrait", Label: "人物 / 写真 / 形象"},
	{Key: "product", Label: "商品 / 电商 / 品牌"},
	{Key: "poster", Label: "海报 / 封面 / 版式"},
	{Key: "infographic", Label: "信息图 / 知识 / 教程"},
	{Key: "design", Label: "UI / 产品设计"},
	{Key: "illustration", Label: "插画 / 动漫 / 游戏 3D"},
	{Key: "photo", Label: "摄影 / 真实场景 / 电影感"},
	{Key: "editing", Label: "图像编辑 / 修饰 / 风格"},
	{Key: "typography", Label: "字体字效"},
	{Key: "brand", Label: "字体 / Logo / 品牌识别"},
}

// PromptCategoryLabel 返回分类键对应的展示名，未知分类回落为键本身。
func PromptCategoryLabel(key string) string {
	for _, c := range PromptCategories {
		if c.Key == key {
			return c.Label
		}
	}
	return key
}
