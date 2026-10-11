package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
	"github.com/whalegate/whalegate/internal/pkg/util"
)

// maxImageResponseBody 单次生图响应最大读取量。b64 图片体积约为原始数据的
// 4/3，四张 1024x1024 的 PNG 已接近该量级。
const maxImageResponseBody = 64 << 20

// maxImagePromptRunes 提示词长度上限，防止超长文本打爆上游。
const maxImagePromptRunes = 8000

// ImageGenerateInput 一次文生图请求。
type ImageGenerateInput struct {
	// Model 对外模型名。
	Model string
	// Prompt 提示词。
	Prompt string
	// NegativePrompt 负向提示词。
	NegativePrompt string
	// N 生成张数，<=0 时按 1 处理。
	N int
	// Size 尺寸，如 1024x1024；为空时由上游默认。
	Size string
	// Quality 质量档位。
	Quality string
	// Style 风格。
	Style string
	// ResponseFormat 期望返回格式：url / b64_json；为空由上游决定。
	ResponseFormat string
	// User 终端用户标识。
	User string
	// Extra 上游扩展参数，原样透传。
	Extra map[string]interface{}
	// GroupMultiplier 所属分组的倍率系数，在点数字上相乘；未分组传 1。
	GroupMultiplier float64
	// Meta 调用日志需要的请求上下文与计费主体；为 nil 时无法计费也无法归因来源。
	Meta *ImageCallMeta
}

// ImageGeneration 一次图片生成的结果。
//
// 失败路径也会返回填好渠道与模型信息的该结构（此时 Response 为 nil）：
// 失败恰恰是最需要知道"打到了哪个渠道、哪个上游模型"的时候，
// 日志里这两个字段为空会让排查无从下手。
type ImageGeneration struct {
	// Model 对外模型名。
	Model string
	// UpstreamModel 经别名/映射改写后实际发往上游的模型名。
	UpstreamModel string
	// ChannelID 实际处理的渠道 ID。
	ChannelID uint
	// ChannelName 渠道名，用于日志。
	ChannelName string
	// LatencyMS 端到端耗时。
	LatencyMS int64
	// ImageCount 实际生成的张数，按张计费的计量单位。
	ImageCount int
	// Points 本次扣减的点数；自用模式下记录"应扣点数"，但用户余额不扣减。
	Points int64
	// SelfUse 本次调用发生在自用模式下（免费）。
	SelfUse bool
	// Response 归一后的图片结果；失败时为 nil。
	Response *spec.ImageResponse
}

// usage 安全取出用量；无结果或上游未上报时返回 nil。
func (g *ImageGeneration) usage() *spec.Usage {
	if g == nil || g.Response == nil || g.Response.Usage == nil {
		return nil
	}
	g.Response.Usage.Normalize()
	return g.Response.Usage
}

// GenerateImage 选择渠道并调用上游完成一次图片生成。
//
// 失败时会按网关配置切换渠道重试；协议不支持生图的渠道直接跳过，
// 且不计入渠道健康度（把生图模型挂在聊天渠道上属于配置问题，不应触发熔断）。
func (c *Container) GenerateImage(ctx context.Context, in ImageGenerateInput) (*ImageGeneration, error) {
	if err := c.normalizeImageInput(&in); err != nil {
		c.logGeneration(in, nil, model.CallStatusFailed, apierr.CodeOf(err), err.Error(), false)
		return nil, err
	}

	// 计费：图片没有 token 维度的口径，张数是唯一可计量的单位，
	// 因此先按"请求张数"预扣，成功后按"实际张数"多退少补。
	// 自用模式下跳过预扣，调用免费（仍记录应扣点数用于成本分析）。
	selfUse := c.SelfUseMode()
	var reservation *Reservation
	if userID := imageBillingUser(in); !selfUse && userID > 0 {
		var err error
		var reservePoints int64
		if reservePoints, err = c.PointsForImage(ctx, in.Model, in.N, in.GroupMultiplier); err == nil {
			reservation, err = c.ReserveQuota(ctx, userID, reservePoints)
		}
		if err != nil {
			// 额度不足在这里就被挡住，工作台与对外接口都不必再打一次上游。
			c.logGeneration(in, nil, model.CallStatusFailed, apierr.CodeOf(err), err.Error(), selfUse)
			return nil, err
		}
	}

	start := time.Now()
	attempts := 1 + c.Config.Gateway.MaxRetries
	if attempts < 1 {
		attempts = 1
	}
	exclude := make(map[uint]struct{}, attempts)
	var (
		lastErr error
		// lastAttempt 最后一次尝试的渠道与上游模型，失败时供日志留痕。
		lastAttempt *ImageGeneration
	)

	for attempt := 0; attempt < attempts; attempt++ {
		channel, err := c.SelectChannel(ctx, in.Model, exclude)
		if err != nil {
			if lastErr == nil {
				lastErr = err
			}
			break
		}

		// 模型名改写只解析一次，转发与日志共用同一结果。
		upstreamModel, override := channel.ResolveModel(in.Model)
		lastAttempt = &ImageGeneration{
			Model:         in.Model,
			UpstreamModel: upstreamModel,
			ChannelID:     channel.ID,
			ChannelName:   channel.Name,
			LatencyMS:     time.Since(start).Milliseconds(),
		}

		if !gateway.SupportsImageGeneration(channel.Type) {
			exclude[channel.ID] = struct{}{}
			if lastErr == nil {
				lastErr = apierr.Errorf(apierr.ErrInvalidParam,
					"渠道「%s」协议为 %s，不支持图片生成", channel.Name, channel.Type)
			}
			continue
		}

		generation, err := c.generateImageOnce(ctx, channel, in, upstreamModel, override, start)
		if err == nil {
			c.RecordChannelSuccess(ctx, channel)
			c.settleImage(ctx, in, generation, reservation, selfUse)
			c.logGeneration(in, generation, model.CallStatusSuccess, 0, "", selfUse)
			return generation, nil
		}
		c.RecordChannelFailure(ctx, channel, err.Error())
		exclude[channel.ID] = struct{}{}
		lastErr = err
		lastAttempt.LatencyMS = time.Since(start).Milliseconds()
		if ctx.Err() != nil {
			break
		}
	}

	if lastErr == nil {
		lastErr = apierr.New(apierr.ErrNoChannel, "模型 "+in.Model+" 暂无可用渠道")
	}
	// 失败全额回补。ctx 可能因客户端断开而取消，此时退回根上下文确保回补发出。
	if reservation != nil {
		releaseCtx := ctx
		if ctx.Err() != nil {
			releaseCtx = context.Background()
		}
		reservation.Release(releaseCtx)
	}
	c.logGeneration(in, lastAttempt, model.CallStatusFailed, apierr.CodeOf(lastErr), lastErr.Error(), selfUse)
	return nil, lastErr
}

// settleImage 结算一次成功的生图：按实际张数多退少补，并把扣除结果写回给调用方。
//
// 口径与 chat 转发保持一致：成功才扣费，自用模式只记录应扣点数。
func (c *Container) settleImage(
	ctx context.Context,
	in ImageGenerateInput,
	generation *ImageGeneration,
	reservation *Reservation,
	selfUse bool,
) {
	if generation == nil {
		return
	}
	if generation.Response != nil {
		generation.ImageCount = len(generation.Response.Images)
	}
	points, err := c.PointsForImage(ctx, in.Model, generation.ImageCount, in.GroupMultiplier)
	if err != nil {
		points = 0
		if c.Logger != nil {
			c.Logger.Warn("计算生图点数失败", zap.Error(err))
		}
	}
	generation.Points = points
	generation.SelfUse = selfUse
	// 结算与统计要写 Redis / 数据库，请求上下文已取消时退回根上下文，避免漏结算。
	settleCtx := ctx
	if ctx.Err() != nil {
		settleCtx = context.Background()
	}
	if reservation != nil {
		reservation.Settle(settleCtx, points)
	}
	// 密钥使用统计：生图没有 token 口径，token 记 0，只累计点数与请求数。
	if keyID := imageBillingKey(in); keyID > 0 {
		c.UpdateAPIKeyUsage(settleCtx, keyID, 0, points)
	}
}

// imageBillingUser / imageBillingKey 取出计费与统计主体。
// Meta 缺失（如内部调用）时返回 0，此时跳过计费——两个入口都要求鉴权，
// 正常链路不会走到这里。
func imageBillingUser(in ImageGenerateInput) uint {
	if in.Meta == nil {
		return 0
	}
	return in.Meta.UserID
}

func imageBillingKey(in ImageGenerateInput) uint {
	if in.Meta == nil {
		return 0
	}
	return in.Meta.APIKeyID
}

// logGeneration 落一次图片生成的调用日志。
//
// 成功与失败都留痕：在线生图是用户直接触发的高成本操作，没有日志就无法
// 按模型、渠道、错误码排查，也无法核对按张计费的扣减是否与实际出图一致。
func (c *Container) logGeneration(
	in ImageGenerateInput,
	attempt *ImageGeneration,
	status int,
	errorCode int,
	errorMessage string,
	selfUse bool,
) {
	entry := &model.CallLog{
		Model:           in.Model,
		Protocol:        gateway.ProtocolOpenAI,
		Status:          status,
		ErrorCode:       errorCode,
		ErrorMessage:    truncate(errorMessage, 500),
		UsageConfidence: model.ConfidenceEstimated,
		SelfUse:         selfUse,
		CreatedAt:       time.Now(),
	}
	if meta := in.Meta; meta != nil {
		entry.UserID = meta.UserID
		entry.APIKeyID = meta.APIKeyID
		entry.TraceID = meta.TraceID
		entry.ClientIP = meta.ClientIP
		entry.RequestScheme = meta.RequestScheme
	}
	// attempt 为 nil 表示连可用渠道都没选出来（如模型未配置）。
	if attempt != nil {
		entry.ChannelID = attempt.ChannelID
		entry.ChannelName = attempt.ChannelName
		entry.UpstreamModel = attempt.UpstreamModel
		entry.LatencyMS = attempt.LatencyMS
		// 计费口径：张数为计量单位，点数按实际出图张数结算（失败为 0）。
		entry.ImageCount = attempt.ImageCount
		entry.Points = attempt.Points
		if usage := attempt.usage(); usage != nil {
			entry.PromptTokens = usage.PromptTokens
			entry.CompletionTokens = usage.CompletionTokens
			entry.ReasoningTokens = usage.ReasoningTokens
			entry.TotalTokens = usage.TotalTokens
			entry.UsageConfidence = model.ConfidenceReported
		}
	}
	c.Logs.Submit(entry)
}

func (c *Container) normalizeImageInput(in *ImageGenerateInput) error {
	in.Model = strings.TrimSpace(in.Model)
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Model == "" {
		return apierr.New(apierr.ErrInvalidParam, "缺少 model")
	}
	if in.Prompt == "" {
		return apierr.New(apierr.ErrInvalidParam, "缺少 prompt")
	}
	if len([]rune(in.Prompt)) > maxImagePromptRunes {
		return apierr.Errorf(apierr.ErrInvalidParam, "提示词过长，最多 %d 字", maxImagePromptRunes)
	}
	max := c.Config.Studio.MaxImagesPerRequest
	if max <= 0 {
		max = 4
	}
	if in.N <= 0 {
		in.N = 1
	}
	if in.N > max {
		return apierr.Errorf(apierr.ErrInvalidParam, "单次最多生成 %d 张图片", max)
	}
	return nil
}

// imageCallMode 一次生图调用使用的上游接口形态。
type imageCallMode int

const (
	// imageModeNative 渠道的原生生图端点：openai 走 /images/generations，
	// gemini 走 generateContent + responseModalities。
	imageModeNative imageCallMode = iota
	// imageModeChat OpenAI 兼容的 chat 接口 + modalities，结果在 message.images[]。
	imageModeChat
)

// generateImageOnce 向单个渠道发起生图调用。
//
// 先走渠道的原生生图端点；上游若明确拒绝该端点，再回退到 chat 接口重试一次。
// 聚合平台常把部分模型的生图能力只开在 chat 接口下（例如 Gemini 系模型），
// 只走原生端点会一直失败。
func (c *Container) generateImageOnce(
	ctx context.Context,
	channel *model.Channel,
	in ImageGenerateInput,
	upstreamModel string,
	override map[string]interface{},
	start time.Time,
) (*ImageGeneration, error) {
	generation, status, err := c.callImageUpstream(ctx, channel, in, upstreamModel, override, start, imageModeNative)
	if err == nil {
		return generation, nil
	}
	if channel.Type != gateway.ProtocolOpenAI || !isEndpointMismatch(status) {
		return nil, err
	}

	fallback, _, fallbackErr := c.callImageUpstream(ctx, channel, in, upstreamModel, override, start, imageModeChat)
	if fallbackErr != nil {
		// 换接口仍失败，返回这一次的原因：原生端点的报错多半只是"不支持该端点"。
		return nil, fallbackErr
	}
	return fallback, nil
}

// callImageUpstream 向上游发起一次生图调用，返回结果、上游 HTTP 状态码与错误。
func (c *Container) callImageUpstream(
	ctx context.Context,
	channel *model.Channel,
	in ImageGenerateInput,
	upstreamModel string,
	override map[string]interface{},
	start time.Time,
	mode imageCallMode,
) (*ImageGeneration, int, error) {
	apiKey, err := c.ChannelUpstreamKey(ctx, channel)
	if err != nil {
		return nil, 0, err
	}

	// 失败时也要把渠道与上游模型带回给日志，因此先建好结果骨架。
	attempt := &ImageGeneration{
		Model:         in.Model,
		UpstreamModel: upstreamModel,
		ChannelID:     channel.ID,
		ChannelName:   channel.Name,
		LatencyMS:     time.Since(start).Milliseconds(),
	}

	var (
		payload map[string]interface{}
		url     string
		parse   func([]byte) (*spec.ImageResponse, error)
	)
	switch mode {
	case imageModeChat:
		payload, err = gateway.BuildImageChatRequest(in.toSpec(), upstreamModel)
		if err != nil {
			return attempt, 0, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		url = gateway.BuildUpstreamURL(channel.Type, channel.BaseURL, upstreamModel, false, vendor.FromBaseURL(channel.BaseURL))
		parse = gateway.ParseImageChatResponse
	default:
		payload, err = gateway.BuildImageRequest(channel.Type, in.toSpec(), upstreamModel, vendor.FromBaseURL(channel.BaseURL))
		if err != nil {
			return attempt, 0, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		url, err = gateway.BuildImageUpstreamURL(channel.Type, channel.BaseURL, upstreamModel, vendor.FromBaseURL(channel.BaseURL))
		if err != nil {
			return attempt, 0, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		parse = func(raw []byte) (*spec.ImageResponse, error) {
			return gateway.ParseImageResponse(channel.Type, raw, vendor.FromBaseURL(channel.BaseURL))
		}
	}

	// 渠道级与别名级 override 对生图同样生效（例如固定水印开关、seed）。
	if len(override) > 0 {
		payload = util.MergeMap(payload, override)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return attempt, 0, apierr.Wrap(apierr.ErrInternal, err)
	}

	upstream := gateway.NewUpstream(c.upstreamTimeout(channel))
	resp, err := upstream.Do(ctx, http.MethodPost, url,
		gateway.BuildUpstreamHeaders(channel.Type, apiKey, vendor.FromBaseURL(channel.BaseURL)), body)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return attempt, 0, apierr.Wrap(apierr.ErrUpstreamTimeout, err)
		}
		return attempt, 0, apierr.Wrap(apierr.ErrUpstream, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageResponseBody))
	if err != nil {
		return attempt, resp.StatusCode, apierr.Wrap(apierr.ErrUpstream, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		msg := gateway.ExtractUpstreamError(channel.Type, raw, vendor.FromBaseURL(channel.BaseURL))
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return attempt, resp.StatusCode, upstreamImageError(resp.StatusCode, msg)
	}

	parsed, err := parse(raw)
	if err != nil {
		return attempt, resp.StatusCode, apierr.Wrap(apierr.ErrUpstreamInvalid, err)
	}
	parsed.Model = in.Model
	attempt.Response = parsed
	attempt.LatencyMS = time.Since(start).Milliseconds()
	return attempt, resp.StatusCode, nil
}

// upstreamImageError 把上游状态码映射为更贴切的业务错误码。
// 一律归为"上游调用失败"会让限流、鉴权失败这类可自愈问题难以分辨。
func upstreamImageError(status int, message string) error {
	msg := truncate(message, 300)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return apierr.Errorf(apierr.ErrUpstream, "上游鉴权失败: %s", msg)
	case http.StatusTooManyRequests:
		return apierr.Errorf(apierr.ErrRateLimited, "上游限流: %s", msg)
	case http.StatusNotFound:
		return apierr.Errorf(apierr.ErrModelNotFound, "上游资源不存在: %s", msg)
	case http.StatusGatewayTimeout:
		return apierr.Errorf(apierr.ErrUpstreamTimeout, "上游超时: %s", msg)
	default:
		return apierr.Errorf(apierr.ErrUpstream, "上游返回 %d: %s", status, msg)
	}
}

// isEndpointMismatch 判断上游错误是否属于"这个端点不认这个模型"。
// 这类错误换个接口通常能成功；鉴权、限流、服务端故障换接口也一样失败。
func isEndpointMismatch(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound,
		http.StatusMethodNotAllowed, http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

// toSpec 转换为协议层请求；上游模型名在构建时单独传入。
func (in ImageGenerateInput) toSpec() *spec.ImageRequest {
	return &spec.ImageRequest{
		Model:          in.Model,
		Prompt:         in.Prompt,
		NegativePrompt: in.NegativePrompt,
		N:              in.N,
		Size:           in.Size,
		Quality:        in.Quality,
		Style:          in.Style,
		ResponseFormat: in.ResponseFormat,
		User:           in.User,
		Extra:          in.Extra,
	}
}

// upstreamTimeout 渠道超时优先，其次网关默认值。
func (c *Container) upstreamTimeout(channel *model.Channel) time.Duration {
	if channel != nil && channel.TimeoutSeconds > 0 {
		return time.Duration(channel.TimeoutSeconds) * time.Second
	}
	if c.Config.Gateway.UpstreamTimeout > 0 {
		return c.Config.Gateway.UpstreamTimeout
	}
	return 300 * time.Second
}
