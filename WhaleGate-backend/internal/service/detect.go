package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm/clause"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
)

// DetectInput 自动探测入参：只需上游地址与密钥。
type DetectInput struct {
	// BaseURL 上游 API 地址，例如 https://api.openai.com/v1。
	BaseURL string `json:"base_url"`
	// APIKey 上游密钥明文；为空且提供了 ChannelID 时，自动使用渠道已保存的密钥。
	APIKey string `json:"api_key"`
	// Type 可选协议提示，为空时自动按顺序试探。
	Type string `json:"type"`
	// ChannelID 渠道 ID，编辑态未重新输入密钥时用于读取已保存密钥。
	ChannelID uint `json:"channel_id,omitempty"`
}

// DetectResult 自动探测结果：可直接用于填充渠道表单。
type DetectResult struct {
	// DetectedType 探测到的原始协议：openai / anthropic / claude-code / codex / gemini。
	DetectedType string `json:"detected_type"`
	// SuggestedType 建议的渠道类型（归一化到网关支持的 openai / gemini）。
	SuggestedType string `json:"suggested_type"`
	// BaseURL 规范化后的地址。
	BaseURL string `json:"base_url"`
	// Endpoint 实际命中的模型列表地址。
	Endpoint string `json:"endpoint"`
	// LatencyMS 探测耗时。
	LatencyMS int64 `json:"latency_ms"`
	// ModelCount 模型总数。
	ModelCount int `json:"model_count"`
	// Models 模型列表（含能力与窗口推断）。
	Models []gateway.DetectedModel `json:"models"`
	// Capabilities 汇总能力。
	Capabilities []string `json:"capabilities"`
	// ContextWindow 全部模型中最大的上下文窗口。
	ContextWindow int `json:"context_window"`
	// MaxOutput 全部模型中最大的输出上限。
	MaxOutput int `json:"max_output"`
	// SuggestedName 依据地址推断的渠道名。
	SuggestedName string `json:"suggested_name"`
	// Warning 需要人工注意的提示（例如协议不被转发核心支持）。
	Warning string `json:"warning,omitempty"`
}

// DetectChannel 用地址与密钥探测上游协议、模型列表与能力，
// 并把结果写入全局模型目录，供渠道/倍率配置直接选择。
func (c *Container) DetectChannel(ctx context.Context, in DetectInput) (*DetectResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.BaseURL == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "请先填写上游 API 地址")
	}
	if in.APIKey == "" {
		if in.ChannelID == 0 {
			return nil, apierr.New(apierr.ErrInvalidParam, "请先填写上游密钥")
		}
		ch, err := c.GetChannel(ctx, in.ChannelID)
		if err != nil {
			return nil, err
		}
		key, err := c.DecryptChannelKey(ch)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, apierr.New(apierr.ErrInvalidParam, "该渠道未保存上游密钥，请先填写")
		}
		in.APIKey = key
	}

	probe, err := gateway.ProbeUpstream(ctx, in.BaseURL, in.APIKey, in.Type)
	if err != nil {
		if c.Logger != nil {
			c.Logger.Info("上游探测失败", zap.String("base_url", in.BaseURL), zap.Error(err))
		}
		return nil, apierr.Errorf(apierr.ErrUpstream, "自动探测失败：%s", err.Error())
	}

	out := &DetectResult{
		DetectedType:  probe.Type,
		SuggestedType: normalizeChannelType(probe.Type),
		BaseURL:       probe.BaseURL,
		Endpoint:      probe.Endpoint,
		LatencyMS:     probe.LatencyMS,
		Models:        probe.Models,
		ModelCount:    len(probe.Models),
		Capabilities:  gateway.SuggestedCapabilities(probe.Models),
		SuggestedName: suggestName(probe),
	}
	for _, m := range probe.Models {
		if m.ContextWindow > out.ContextWindow {
			out.ContextWindow = m.ContextWindow
		}
		if m.MaxOutput > out.MaxOutput {
			out.MaxOutput = m.MaxOutput
		}
	}
	if out.SuggestedType != probe.Type {
		out.Warning = "检测到 " + probe.Type + " 原生协议，本网关转发仅支持 OpenAI 兼容与 Gemini 原生；" +
			"若该上游提供 OpenAI 兼容端点，请改用对应地址后再探测。"
	}

	// 异步落模型目录，不阻塞探测响应。
	go c.syncModelCatalog(probe.Type, probe.Models)

	return out, nil
}

// normalizeChannelType 把探测协议归一化到网关支持的渠道类型。
func normalizeChannelType(detected string) string {
	if detected == model.ChannelTypeGemini {
		return model.ChannelTypeGemini
	}
	return model.ChannelTypeOpenAI
}

// suggestName 依据地址与协议推断渠道名。
func suggestName(probe *gateway.ProbeResult) string {
	host := probe.BaseURL
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	if idx := strings.IndexAny(host, "/?"); idx >= 0 {
		host = host[:idx]
	}
	if host == "" {
		host = probe.Type
	}
	return probe.Type + "-" + host
}

// syncModelCatalog 把探测结果写入全局模型目录。
func (c *Container) syncModelCatalog(providerType string, models []gateway.DetectedModel) {
	if len(models) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	now := time.Now()
	rows := make([]model.ModelCatalog, 0, len(models))
	for _, m := range models {
		if m.ID == "" {
			continue
		}
		row := model.ModelCatalog{
			ModelID:       m.ID,
			DisplayName:   m.DisplayName,
			ProviderType:  providerType,
			ContextWindow: m.ContextWindow,
			MaxOutput:     m.MaxOutput,
			Source:        model.CatalogSourceDetect,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		_ = row.SetCapabilities(m.Capabilities)
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return
	}

	err := c.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "model_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"display_name", "provider_type", "capabilities",
			"context_window", "max_output", "updated_at",
		}),
	}).CreateInBatches(rows, 100).Error
	if err != nil && c.Logger != nil {
		c.Logger.Warn("同步模型目录失败", zap.Error(err))
	}
}

// CatalogFilter 模型目录筛选条件。
type CatalogFilter struct {
	// Keyword 匹配模型名或展示名。
	Keyword string
	// ProviderType 协议类型，空表示全部。
	ProviderType string
	// Capability 能力过滤，例如 chat / image。
	Capability string
}

// ListModelCatalog 查询全局模型目录。
func (c *Container) ListModelCatalog(ctx context.Context, f CatalogFilter, page, pageSize int) ([]model.ModelCatalog, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.ModelCatalog{})
	if f.ProviderType != "" {
		q = q.Where("provider_type = ?", f.ProviderType)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + strings.ToLower(kw) + "%"
		q = q.Where("LOWER(model_id) LIKE ? OR LOWER(display_name) LIKE ?", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var list []model.ModelCatalog
	if err := q.Order("model_id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	if f.Capability != "" {
		list = filterByCapability(list, f.Capability)
	}
	return list, total, nil
}

// filterByCapability 在内存中按能力过滤（能力以 JSON 文本存储）。
func filterByCapability(list []model.ModelCatalog, capability string) []model.ModelCatalog {
	out := make([]model.ModelCatalog, 0, len(list))
	for _, item := range list {
		for _, cap := range item.CapabilityList() {
			if cap == capability {
				out = append(out, item)
				break
			}
		}
	}
	return out
}

// applyDetectMetadata 把入参中的探测元数据写入渠道。
func applyDetectMetadata(ch *model.Channel, in ChannelInput) {
	if ch == nil {
		return
	}
	if len(in.Capabilities) > 0 {
		caps, err := json.Marshal(in.Capabilities)
		if err == nil {
			ch.Capabilities = string(caps)
			now := time.Now()
			ch.DetectedAt = &now
		}
	}
	if in.ContextWindow > 0 {
		ch.ContextWindow = in.ContextWindow
	}
	if in.MaxOutput > 0 {
		ch.MaxOutput = in.MaxOutput
	}
}
