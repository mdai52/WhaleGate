package model

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/util"
)

// 渠道状态。
const (
	// ChannelStatusEnabled 正常。
	ChannelStatusEnabled = 1
	// ChannelStatusManualDisabled 人工禁用。
	ChannelStatusManualDisabled = 2
	// ChannelStatusAutoDisabled 连续失败后自动熔断。
	ChannelStatusAutoDisabled = 3
)

// 渠道协议类型。
const (
	ChannelTypeOpenAI = "openai"
	ChannelTypeGemini = "gemini"
)

// Channel 上游渠道。api_key 列存储 AES-256-GCM 密文。
type Channel struct {
	ID           uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string `gorm:"size:128;not null" json:"name"`
	Type         string `gorm:"size:32;not null" json:"type"`
	BaseURL      string `gorm:"size:512;not null" json:"base_url"`
	APIKey       string `gorm:"size:1024;not null" json:"-"`
	Models       string `gorm:"type:text" json:"models"`
	ModelMapping string `gorm:"type:text" json:"model_mapping"`
	// ModelAlias 模型别名：{"对外名":{"model":"上游真实模型","override":{...}}}
	ModelAlias string `gorm:"type:text" json:"model_alias"`
	// RequestOverride 渠道级请求参数注入，合并到发往上游的请求体。
	RequestOverride string `gorm:"type:text" json:"request_override"`
	// ParamSchema 上游参数能力声明，用于自定义参数的自动识别与分配。
	ParamSchema string `gorm:"type:text" json:"param_schema"`
	// CredentialID 绑定的 OAuth 凭证，优先于静态 api_key。
	CredentialID   *uint      `gorm:"column:credential_id" json:"credential_id,omitempty"`
	Weight         int        `gorm:"not null;default:1" json:"weight"`
	Priority       int        `gorm:"not null;default:0" json:"priority"`
	Status         int        `gorm:"not null;default:1" json:"status"`
	TimeoutSeconds int        `gorm:"not null;default:0" json:"timeout_seconds"`
	MaxRetries     int        `gorm:"not null;default:0" json:"max_retries"`
	LastError      string     `gorm:"type:text" json:"last_error,omitempty"`
	FailCount      int        `gorm:"not null;default:0" json:"fail_count"`
	DisabledAt     *time.Time `json:"disabled_at,omitempty"`
	// Capabilities 自动探测得到的渠道能力集合（JSON 数组文本）。
	Capabilities string `gorm:"type:text;not null;default:'[]'" json:"capabilities"`
	// ContextWindow 探测到的最大上下文窗口。
	ContextWindow int `gorm:"not null;default:0" json:"context_window"`
	// MaxOutput 探测到的最大输出 token。
	MaxOutput int `gorm:"not null;default:0" json:"max_output"`
	// DetectedAt 最近一次自动探测时间。
	DetectedAt *time.Time     `json:"detected_at,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 表名。
func (Channel) TableName() string { return "channels" }

// IsEnabled 是否可用。
func (c *Channel) IsEnabled() bool { return c != nil && c.Status == ChannelStatusEnabled }

// ModelList 解析支持的模型列表。
func (c *Channel) ModelList() []string {
	if c == nil || strings.TrimSpace(c.Models) == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(c.Models), &list); err != nil {
		return nil
	}
	return list
}

// SetModelList 序列化模型列表。
func (c *Channel) SetModelList(list []string) error {
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	c.Models = string(data)
	return nil
}

// AliasConfig 模型别名配置：把同一个上游模型 fork 成多个对外模型名，
// 每个别名可携带独立的参数注入（例如不同分辨率的生图模型）。
type AliasConfig struct {
	// Model 上游真实模型名，为空表示沿用别名自身。
	Model string `json:"model,omitempty"`
	// Override 该别名专属的请求参数注入，合并进上游请求体。
	Override map[string]interface{} `json:"override,omitempty"`
}

// AliasMap 解析别名配置。
func (c *Channel) AliasMap() map[string]AliasConfig {
	if c == nil || strings.TrimSpace(c.ModelAlias) == "" {
		return nil
	}
	var out map[string]AliasConfig
	if err := json.Unmarshal([]byte(c.ModelAlias), &out); err != nil {
		return nil
	}
	return out
}

// SetAliasMap 序列化别名配置。
func (c *Channel) SetAliasMap(m map[string]AliasConfig) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.ModelAlias = string(data)
	return nil
}

// OverrideMap 解析渠道级参数注入。
func (c *Channel) OverrideMap() map[string]interface{} {
	if c == nil || strings.TrimSpace(c.RequestOverride) == "" {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(c.RequestOverride), &out); err != nil {
		return nil
	}
	return out
}

// SetOverrideMap 序列化参数注入配置。
func (c *Channel) SetOverrideMap(m map[string]interface{}) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.RequestOverride = string(data)
	return nil
}

// Schema 解析参数能力声明。
func (c *Channel) Schema() *spec.ParamSchema {
	if c == nil || strings.TrimSpace(c.ParamSchema) == "" {
		return nil
	}
	var out spec.ParamSchema
	if err := json.Unmarshal([]byte(c.ParamSchema), &out); err != nil {
		return nil
	}
	return &out
}

// SetSchema 序列化参数能力声明。
func (c *Channel) SetSchema(s *spec.ParamSchema) error {
	if s == nil {
		c.ParamSchema = ""
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	c.ParamSchema = string(data)
	return nil
}

// ResolveModel 解析对外模型名，返回上游真实模型名与需要注入的参数。
// 优先级：别名（含专属 override）> 模型映射 > 原名；渠道级 override 始终生效并被别名覆盖。
func (c *Channel) ResolveModel(external string) (string, map[string]interface{}) {
	override := c.OverrideMap()
	if alias, ok := c.AliasMap()[external]; ok {
		upstream := alias.Model
		if upstream == "" {
			upstream = external
		}
		if len(alias.Override) > 0 {
			override = util.MergeMap(override, alias.Override)
		}
		return upstream, override
	}
	return c.MapModel(external), override
}

// ExposedModels 对外暴露的模型名：登记的 model 列表 + 全部别名。
func (c *Channel) ExposedModels() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range c.ModelList() {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	for alias := range c.AliasMap() {
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		out = append(out, alias)
	}
	return out
}

// SupportsModel 判断渠道是否支持该模型；列表为空表示通配。
func (c *Channel) SupportsModel(model string) bool {
	if _, ok := c.AliasMap()[model]; ok {
		return true
	}
	list := c.ModelList()
	if len(list) == 0 {
		return true
	}
	for _, m := range list {
		if m == model {
			return true
		}
	}
	return false
}

// MapModel 应用模型名映射，未配置时原样返回。
func (c *Channel) MapModel(model string) string {
	if c == nil || strings.TrimSpace(c.ModelMapping) == "" {
		return model
	}
	var mapping map[string]string
	if err := json.Unmarshal([]byte(c.ModelMapping), &mapping); err != nil {
		return model
	}
	if target, ok := mapping[model]; ok && target != "" {
		return target
	}
	return model
}

// EffectiveWeight 返回参与负载均衡的权重，非法值退化为 1。
func (c *Channel) EffectiveWeight() int {
	if c == nil || c.Weight <= 0 {
		return 1
	}
	return c.Weight
}
