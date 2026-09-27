package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	urlpkg "net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/crypto"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// ChannelInput 渠道创建/更新入参。
type ChannelInput struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
	// APIKey 上游密钥明文，落库前加密。为空表示保留原值。
	APIKey       string            `json:"api_key"`
	Models       []string          `json:"models"`
	ModelMapping map[string]string `json:"model_mapping"`
	// ModelAlias 模型别名：一个上游模型可 fork 成多个对外模型名。
	ModelAlias map[string]model.AliasConfig `json:"model_alias"`
	// RequestOverride 渠道级参数注入，强制合并到上游请求体。
	RequestOverride map[string]interface{} `json:"request_override"`
	// ParamSchema 上游参数能力声明：支持哪些自定义参数、默认值等。
	ParamSchema *spec.ParamSchema `json:"param_schema"`
	// CredentialID 绑定的 OAuth 凭证 ID，0 表示清除绑定（优先于静态 api_key）。
	CredentialID   *uint `json:"credential_id"`
	Weight         int   `json:"weight"`
	Priority       int   `json:"priority"`
	Status         int   `json:"status"`
	TimeoutSeconds int   `json:"timeout_seconds"`
	MaxRetries     int   `json:"max_retries"`
	// Capabilities 自动探测得到的能力集合，留空表示未探测。
	Capabilities []string `json:"capabilities"`
	// ContextWindow 自动探测得到的上下文窗口。
	ContextWindow int `json:"context_window"`
	// MaxOutput 自动探测得到的输出上限。
	MaxOutput int `json:"max_output"`
}

const enabledChannelCacheKey = "channels:enabled"

// CreateChannel 新增渠道，上游密钥加密存储。
func (c *Container) CreateChannel(ctx context.Context, in ChannelInput) (*model.Channel, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "渠道名称不能为空")
	}
	if strings.TrimSpace(in.BaseURL) == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "上游地址不能为空")
	}
	if in.Type != model.ChannelTypeOpenAI && in.Type != model.ChannelTypeGemini {
		return nil, apierr.New(apierr.ErrInvalidParam, "渠道类型仅支持 openai 或 gemini")
	}

	ch := &model.Channel{
		Name:           strings.TrimSpace(in.Name),
		Type:           in.Type,
		BaseURL:        strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		Weight:         defaultInt(in.Weight, 1),
		Priority:       in.Priority,
		Status:         defaultInt(in.Status, model.ChannelStatusEnabled),
		TimeoutSeconds: in.TimeoutSeconds,
		MaxRetries:     in.MaxRetries,
	}
	if err := ch.SetModelList(in.Models); err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if len(in.ModelMapping) > 0 {
		data, err := json.Marshal(in.ModelMapping)
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		ch.ModelMapping = string(data)
	}
	if len(in.ModelAlias) > 0 {
		if err := ch.SetAliasMap(in.ModelAlias); err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
	}
	if len(in.RequestOverride) > 0 {
		if err := ch.SetOverrideMap(in.RequestOverride); err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
	}
	if in.ParamSchema != nil {
		if err := ch.SetSchema(in.ParamSchema); err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
	}
	if in.CredentialID != nil {
		if *in.CredentialID > 0 {
			if _, err := c.GetCredential(ctx, *in.CredentialID); err != nil {
				return nil, err
			}
		}
		ch.CredentialID = in.CredentialID
	}
	if in.APIKey != "" {
		cipher, err := crypto.Encrypt(in.APIKey, c.encryptionKey())
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrCrypto, err)
		}
		ch.APIKey = cipher
	}
	applyDetectMetadata(ch, in)

	if err := c.DB.WithContext(ctx).Create(ch).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.invalidateChannelCache()
	return ch, nil
}

// UpdateChannel 更新渠道；APIKey 为空时保留原值。
func (c *Container) UpdateChannel(ctx context.Context, id uint, in ChannelInput) (*model.Channel, error) {
	var ch model.Channel
	if err := c.DB.WithContext(ctx).First(&ch, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrChannelNotFound, err)
	}

	patch := map[string]interface{}{}
	if strings.TrimSpace(in.Name) != "" {
		patch["name"] = strings.TrimSpace(in.Name)
	}
	if in.Type != "" {
		if in.Type != model.ChannelTypeOpenAI && in.Type != model.ChannelTypeGemini {
			return nil, apierr.New(apierr.ErrInvalidParam, "渠道类型仅支持 openai 或 gemini")
		}
		patch["type"] = in.Type
	}
	if strings.TrimSpace(in.BaseURL) != "" {
		patch["base_url"] = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	}
	if in.APIKey != "" {
		cipher, err := crypto.Encrypt(in.APIKey, c.encryptionKey())
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrCrypto, err)
		}
		patch["api_key"] = cipher
	}
	if in.Models != nil {
		data, err := json.Marshal(in.Models)
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		patch["models"] = string(data)
	}
	if in.ModelMapping != nil {
		data, err := json.Marshal(in.ModelMapping)
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		patch["model_mapping"] = string(data)
	}
	if in.ModelAlias != nil {
		data, err := json.Marshal(in.ModelAlias)
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		patch["model_alias"] = string(data)
	}
	if in.RequestOverride != nil {
		data, err := json.Marshal(in.RequestOverride)
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		patch["request_override"] = string(data)
	}
	if in.ParamSchema != nil {
		data, err := json.Marshal(in.ParamSchema)
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		patch["param_schema"] = string(data)
	}
	if in.CredentialID != nil {
		if *in.CredentialID > 0 {
			if _, err := c.GetCredential(ctx, *in.CredentialID); err != nil {
				return nil, err
			}
		}
		patch["credential_id"] = *in.CredentialID
	}
	// 自动探测元数据
	if len(in.Capabilities) > 0 {
		caps, err := json.Marshal(in.Capabilities)
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		patch["capabilities"] = string(caps)
		patch["detected_at"] = time.Now()
	}
	if in.ContextWindow > 0 {
		patch["context_window"] = in.ContextWindow
	}
	if in.MaxOutput > 0 {
		patch["max_output"] = in.MaxOutput
	}
	if in.Weight > 0 {
		patch["weight"] = in.Weight
	}
	if in.Priority != 0 {
		patch["priority"] = in.Priority
	}
	if in.Status != 0 {
		patch["status"] = in.Status
	}
	if in.TimeoutSeconds > 0 {
		patch["timeout_seconds"] = in.TimeoutSeconds
	}
	if in.MaxRetries >= 0 {
		patch["max_retries"] = in.MaxRetries
	}
	if len(patch) == 0 {
		return &ch, nil
	}

	if err := c.DB.WithContext(ctx).Model(&ch).Updates(patch).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.invalidateChannelCache()
	if err := c.DB.WithContext(ctx).First(&ch, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return &ch, nil
}

// DeleteChannel 删除渠道。
func (c *Container) DeleteChannel(ctx context.Context, id uint) error {
	res := c.DB.WithContext(ctx).Delete(&model.Channel{}, id)
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrChannelNotFound, "渠道不存在")
	}
	c.invalidateChannelCache()
	return nil
}

// GetChannel 查询渠道详情。
func (c *Container) GetChannel(ctx context.Context, id uint) (*model.Channel, error) {
	var ch model.Channel
	if err := c.DB.WithContext(ctx).First(&ch, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrChannelNotFound, err)
	}
	return &ch, nil
}

// ListChannels 分页查询渠道。
func (c *Container) ListChannels(ctx context.Context, page, pageSize int) ([]model.Channel, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.Channel{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var list []model.Channel
	if err := q.Order("priority DESC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, total, nil
}

// DecryptChannelKey 解密渠道上游密钥。
func (c *Container) DecryptChannelKey(ch *model.Channel) (string, error) {
	if ch == nil || ch.APIKey == "" {
		return "", nil
	}
	plain, err := crypto.Decrypt(ch.APIKey, c.encryptionKey())
	if err != nil {
		return "", apierr.Wrap(apierr.ErrCrypto, err)
	}
	return plain, nil
}

// SelectChannel 按优先级与权重选择可用渠道，exclude 中的渠道将被跳过。
func (c *Container) SelectChannel(ctx context.Context, modelName string, exclude map[uint]struct{}) (*model.Channel, error) {
	list, err := c.EnabledChannels(ctx)
	if err != nil {
		return nil, err
	}

	var candidates []*model.Channel
	bestPriority := -1 << 31
	for i := range list {
		ch := list[i]
		if _, skip := exclude[ch.ID]; skip {
			continue
		}
		if !ch.SupportsModel(modelName) {
			continue
		}
		if ch.Priority > bestPriority {
			bestPriority = ch.Priority
			candidates = candidates[:0]
			candidates = append(candidates, ch)
			continue
		}
		if ch.Priority == bestPriority {
			candidates = append(candidates, ch)
		}
	}
	if len(candidates) == 0 {
		return nil, apierr.New(apierr.ErrNoChannel, "模型 "+modelName+" 暂无可用渠道")
	}
	return weightedPick(candidates), nil
}

// RecordChannelFailure 记录渠道失败；连续失败达到阈值后自动熔断。
func (c *Container) RecordChannelFailure(ctx context.Context, ch *model.Channel, reason string) {
	if ch == nil {
		return
	}
	failKey := fmt.Sprintf("%s:chfail:%d", c.prefix(), ch.ID)
	window := c.Config.Gateway.AutoDisableWindow
	if window <= 0 {
		window = 5 * time.Minute
	}
	threshold := c.Config.Gateway.AutoDisableThreshold
	if threshold <= 0 {
		threshold = 5
	}

	count := int64(1)
	if c.RDB != nil {
		n, err := c.RDB.Incr(ctx, failKey).Result()
		if err == nil {
			count = n
			_ = c.RDB.Expire(ctx, failKey, window).Err()
		}
	}

	patch := map[string]interface{}{"last_error": truncate(reason, 512), "fail_count": count}
	if count >= int64(threshold) {
		patch["status"] = model.ChannelStatusAutoDisabled
		patch["disabled_at"] = time.Now()
		c.invalidateChannelCache()
		if c.Logger != nil {
			c.Logger.Warn("渠道连续失败已自动禁用",
				zap.Uint("channel_id", ch.ID), zap.String("channel", ch.Name), zap.Int64("failures", count))
		}
	}
	if err := c.DB.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", ch.ID).Updates(patch).Error; err != nil {
		if c.Logger != nil {
			c.Logger.Warn("记录渠道失败失败", zap.Uint("channel_id", ch.ID), zap.Error(err))
		}
	}
}

// RecordChannelSuccess 清除渠道失败计数，并在曾熔断时恢复启用。
func (c *Container) RecordChannelSuccess(ctx context.Context, ch *model.Channel) {
	if ch == nil {
		return
	}
	if c.RDB != nil {
		_ = c.RDB.Del(ctx, fmt.Sprintf("%s:chfail:%d", c.prefix(), ch.ID)).Err()
	}
	if ch.FailCount == 0 {
		return
	}
	if err := c.DB.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", ch.ID).
		Updates(map[string]interface{}{"fail_count": 0, "last_error": ""}).Error; err != nil && c.Logger != nil {
		c.Logger.Warn("重置渠道失败计数失败", zap.Uint("channel_id", ch.ID), zap.Error(err))
	}
}

// TestChannel 轻量连通性测试：拉取上游模型列表，不产生 token 消耗。
func (c *Container) TestChannel(ctx context.Context, ch *model.Channel) (map[string]interface{}, error) {
	key, err := c.DecryptChannelKey(ch)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(ch.BaseURL, "/")
	headerName, headerValue := gateway.BuildUpstreamAuthHeader(ch.Type, key)
	headers := map[string]string{}
	if ch.Type == model.ChannelTypeGemini {
		url += "/models?key=" + urlpkg.QueryEscape(key)
	} else {
		headers[headerName] = headerValue
		url += "/models"
	}

	timeout := time.Duration(ch.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	start := time.Now()
	resp, err := gateway.NewUpstream(timeout).Do(ctx, http.MethodGet, url, headers, nil)
	if err != nil {
		c.RecordChannelFailure(ctx, ch, err.Error())
		return nil, apierr.Wrap(apierr.ErrUpstream, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode >= http.StatusBadRequest {
		msg := gateway.ExtractUpstreamError(ch.Type, body)
		if msg == "" {
			msg = string(body)
		}
		c.RecordChannelFailure(ctx, ch, msg)
		return nil, apierr.Errorf(apierr.ErrUpstream, "上游返回 %d: %s", resp.StatusCode, truncate(msg, 300))
	}
	c.RecordChannelSuccess(ctx, ch)

	return map[string]interface{}{
		"status":     resp.StatusCode,
		"latency_ms": time.Since(start).Milliseconds(),
	}, nil
}

// enabledChannels 读取启用渠道列表（带 15 秒进程内缓存）。
func (c *Container) EnabledChannels(ctx context.Context) ([]*model.Channel, error) {
	if c.KeyCache != nil {
		if v, ok := c.KeyCache.Get(enabledChannelCacheKey); ok {
			if list, ok := v.([]*model.Channel); ok {
				return list, nil
			}
		}
	}

	var list []*model.Channel
	if err := c.DB.WithContext(ctx).Where("status = ?", model.ChannelStatusEnabled).
		Order("priority DESC, id ASC").Find(&list).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	if c.KeyCache != nil {
		c.KeyCache.SetWithTTL(enabledChannelCacheKey, list, 15*time.Second)
	}
	return list, nil
}

func (c *Container) invalidateChannelCache() {
	if c.KeyCache != nil {
		c.KeyCache.Delete(enabledChannelCacheKey)
	}
}

func (c *Container) encryptionKey() []byte {
	return []byte(c.Config.Security.EncryptionKey)
}

// weightedPick 按权重随机选择，权重总和为 0 时退化为等概率。
func weightedPick(channels []*model.Channel) *model.Channel {
	var total int
	for _, ch := range channels {
		total += ch.EffectiveWeight()
	}
	if total <= 0 {
		return channels[rand.Intn(len(channels))]
	}
	pick := rand.Intn(total)
	for _, ch := range channels {
		pick -= ch.EffectiveWeight()
		if pick < 0 {
			return ch
		}
	}
	return channels[len(channels)-1]
}

func defaultInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
