package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
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
	// Meta 调用日志需要的请求上下文；为 nil 时仍记录业务字段，只是缺少来源信息。
	Meta *ImageCallMeta
}

// ImageCallMeta 一次图片生成的可观测上下文，由调用方从 HTTP 上下文中提取。
type ImageCallMeta struct {
	// UserID 归属用户。
	UserID uint
	// APIKeyID 通过 API Key 调用时为密钥 ID，工作台调用为 0。
	APIKeyID uint
	// TraceID 链路追踪 ID。
	TraceID string
	// ClientIP 客户端真实 IP。
	ClientIP string
	// RequestScheme 入站协议：http / https。
	RequestScheme string
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
		c.logGeneration(in, nil, model.CallStatusFailed, apierr.CodeOf(err), err.Error())
		return nil, err
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
			c.logGeneration(in, generation, model.CallStatusSuccess, 0, "")
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
	c.logGeneration(in, lastAttempt, model.CallStatusFailed, apierr.CodeOf(lastErr), lastErr.Error())
	return nil, lastErr
}

// logGeneration 落一次图片生成的调用日志。
//
// 成功与失败都留痕：在线生图是用户直接触发的高成本操作，没有日志就无法
// 按模型、渠道、错误码排查。图片没有 token 维度的计费口径，因此用量留空、
// 点数记为 0（按张计费待后续支持）。
func (c *Container) logGeneration(
	in ImageGenerateInput,
	attempt *ImageGeneration,
	status int,
	errorCode int,
	errorMessage string,
) {
	entry := &model.CallLog{
		Model:           in.Model,
		Protocol:        gateway.ProtocolOpenAI,
		Status:          status,
		ErrorCode:       errorCode,
		ErrorMessage:    truncate(errorMessage, 500),
		UsageConfidence: model.ConfidenceEstimated,
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
		url = gateway.BuildUpstreamURL(channel.Type, channel.BaseURL, upstreamModel, false)
		parse = gateway.ParseImageChatResponse
	default:
		payload, err = gateway.BuildImageRequest(channel.Type, in.toSpec(), upstreamModel)
		if err != nil {
			return attempt, 0, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		url, err = gateway.BuildImageUpstreamURL(channel.Type, channel.BaseURL, upstreamModel)
		if err != nil {
			return attempt, 0, apierr.Wrap(apierr.ErrInvalidParam, err)
		}
		parse = func(raw []byte) (*spec.ImageResponse, error) {
			return gateway.ParseImageResponse(channel.Type, raw)
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
	resp, err := upstream.Do(ctx, http.MethodPost, url, gateway.BuildUpstreamHeaders(channel.Type, apiKey), body)
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
		msg := gateway.ExtractUpstreamError(channel.Type, raw)
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
