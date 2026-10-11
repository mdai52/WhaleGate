package model

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
)

// VendorProfile 厂商适配档案（管理员可维护）。
//
// 档案详情（端点、鉴权、字段命名、错误外壳等）整体放在 config 里：
// 这些字段是「一组相关配置」，拆成十几个列既难维护也难扩展，
// 落 JSONB 后新增维度不用改表结构。
type VendorProfile struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Code 档案代码，与 config.id 一致，全局唯一。
	Code string `gorm:"column:code;size:64;not null;uniqueIndex" json:"code"`
	// Name 展示名，从 config 同步而来，供列表页直接展示与排序。
	Name string `gorm:"column:name;size:128;not null;default:''" json:"name"`
	// GroupName 分组：国内 / 海外 / 聚合 / 本地。
	GroupName string `gorm:"column:group_name;size:32;not null;default:''" json:"group_name"`
	// MatchHosts 识别用的主机关键词，逗号分隔。
	MatchHosts string `gorm:"column:match_hosts;type:text;not null;default:''" json:"match_hosts"`
	// Enabled 停用的档案不参与识别与预设。
	Enabled bool `gorm:"column:enabled;not null;default:true" json:"enabled"`
	// Sort 排序权重，大的在前。
	Sort int `gorm:"column:sort;not null;default:0" json:"sort"`
	// Config 档案详情 JSON。
	Config string `gorm:"column:config;type:text;not null" json:"-"`
	// Builtin 内置档案：种子数据自带，界面上提示谨慎修改。
	Builtin bool `gorm:"column:builtin;not null;default:false" json:"builtin"`

	CreatedAt time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

// TableName 表名。
func (VendorProfile) TableName() string { return "vendor_profiles" }

// Profile 把档案详情解析为厂商档案，解析失败时返回 nil。
func (p *VendorProfile) Profile() *vendor.Vendor {
	if p == nil || p.Config == "" {
		return nil
	}
	var v vendor.Vendor
	if err := json.Unmarshal([]byte(p.Config), &v); err != nil {
		return nil
	}
	return &v
}

// Hosts 解析主机关键词列表（逗号 / 换行 / 分号均可分隔）。
func (p *VendorProfile) Hosts() []string {
	if p == nil || p.MatchHosts == "" {
		return nil
	}
	replaced := strings.NewReplacer("\n", ",", ";", ",", "，", ",")
	out := make([]string, 0, 4)
	for _, part := range strings.Split(replaced.Replace(p.MatchHosts), ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ApplyConfig 把解析后的档案写回行：同步 name / group / hosts 这些展示与识别字段，
// 保证「详情」与「列表展示」不会出现两份不一致的数据。
func (p *VendorProfile) ApplyConfig(v *vendor.Vendor) error {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.Config = string(raw)
	if v.ID != "" {
		p.Code = v.ID
	}
	if v.Name != "" {
		p.Name = v.Name
	}
	p.GroupName = v.Group
	p.MatchHosts = strings.Join(v.Hosts, ",")
	return nil
}
