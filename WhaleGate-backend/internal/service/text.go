// 工作台内部使用的文本生成能力：一次非流式的 chat 补全。
//
// 脚本创作这类功能由网关自己发起（而非转发客户端请求），需要一个
// 服务层的「调模型拿文本」入口。它复用转发链路的选路、协议适配与响应归一，
// 但不涉及客户端协议转换——输入输出都是统一格式。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/util"
)

// ---------------------------------------------------------------- 错误归类

// classifyTextError 网络层错误归类：超时单列，便于前端与日志区分。
func classifyTextError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apierr.Wrap(apierr.ErrUpstreamTimeout, err)
	}
	return apierr.Wrap(apierr.ErrUpstream, err)
}

// upstreamTextHTTPError 把上游状态码映射为更贴切的业务错误码。
func upstreamTextHTTPError(status int, message string) error {
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

// retryableTextError 判定是否值得换渠道重试：参数与模型类错误换渠道也一样失败。
func retryableTextError(err error) bool {
	be := apierr.From(err)
	switch be.Code {
	case apierr.CodeModelNotFound, apierr.CodeInvalidParam, apierr.CodeBadRequest, apierr.CodeInvalidJSON:
		return false
	default:
		return true
	}
}

// maxTextResponseBody 文本补全响应最大读取量（8MB）。
const maxTextResponseBody = 8 << 20

// TextCompleteInput 一次文本补全的输入。
type TextCompleteInput struct {
	// Model 对外模型名。
	Model string
	// Messages 完整对话；至少包含一条 user 消息。
	Messages []spec.Message
	// Temperature 采样温度，<=0 时不下发。
	Temperature *float64
	// MaxTokens 输出上限，<=0 时不下发（由上游决定）。
	MaxTokens int
	// Stop 停止词。
	Stop []string
}

// TextCompleteResult 一次文本补全的结果。
type TextCompleteResult struct {
	// Text 模型输出的正文。
	Text string
	// Usage token 用量，上游未上报时为估算值。
	Usage *spec.Usage
	// ChannelID / ChannelName 实际处理的渠道，用于日志与排查。
	ChannelID   uint
	ChannelName string
	// UpstreamModel 经别名/映射改写后实际发往上游的模型名。
	UpstreamModel string
	// LatencyMS 端到端耗时。
	LatencyMS int64
}

// CompleteText 选路并调用上游完成一次文本生成，失败时切换渠道重试。
//
// 与转发链路的差别：不做额度预扣/结算（由调用方按自己的口径处理），
// 也不回写 HTTP 响应——调用方拿到的只是一段文本。
func (c *Container) CompleteText(ctx context.Context, in TextCompleteInput) (*TextCompleteResult, error) {
	if in.Model == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "缺少 model")
	}
	if len(in.Messages) == 0 {
		return nil, apierr.New(apierr.ErrInvalidParam, "缺少消息内容")
	}

	req := &spec.UnifiedRequest{
		Model:       in.Model,
		Messages:    in.Messages,
		Temperature: in.Temperature,
		Stop:        in.Stop,
	}
	if in.MaxTokens > 0 {
		req.MaxTokens = &in.MaxTokens
	}

	start := time.Now()
	attempts := 1 + c.Config.Gateway.MaxRetries
	if attempts < 1 {
		attempts = 1
	}
	exclude := make(map[uint]struct{}, attempts)
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		channel, err := c.SelectChannel(ctx, in.Model, exclude)
		if err != nil {
			if lastErr == nil {
				lastErr = err
			}
			break
		}

		result, err := c.completeTextOnce(ctx, channel, req, start)
		if err == nil {
			c.RecordChannelSuccess(ctx, channel)
			return result, nil
		}
		c.RecordChannelFailure(ctx, channel, err.Error())
		exclude[channel.ID] = struct{}{}
		lastErr = err
		if ctx.Err() != nil || !retryableTextError(err) {
			break
		}
	}
	if lastErr == nil {
		lastErr = apierr.New(apierr.ErrNoChannel, "模型 "+in.Model+" 暂无可用渠道")
	}
	return nil, lastErr
}

func (c *Container) completeTextOnce(
	ctx context.Context,
	channel *model.Channel,
	req *spec.UnifiedRequest,
	start time.Time,
) (*TextCompleteResult, error) {
	apiKey, err := c.ChannelUpstreamKey(ctx, channel)
	if err != nil {
		return nil, err
	}
	upstreamModel, override := channel.ResolveModel(req.Model)
	upstreamReq := *req
	upstreamReq.Model = upstreamModel

	payload, err := gateway.BuildUpstreamRequest(channel.Type, &upstreamReq)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if len(override) > 0 {
		payload = util.MergeMap(payload, override)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	url := gateway.BuildUpstreamURL(channel.Type, channel.BaseURL, upstreamModel, false)
	upstream := gateway.NewUpstream(c.upstreamTimeout(channel))
	resp, err := upstream.Do(ctx, http.MethodPost, url, gateway.BuildUpstreamHeaders(channel.Type, apiKey), body)
	if err != nil {
		return nil, classifyTextError(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTextResponseBody))
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrUpstream, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		msg := gateway.ExtractUpstreamError(channel.Type, raw)
		if msg == "" {
			msg = string(raw)
		}
		return nil, upstreamTextHTTPError(resp.StatusCode, msg)
	}

	unified, err := gateway.ParseUpstreamResponse(channel.Type, raw)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrUpstreamInvalid, err)
	}
	usage := unified.Usage
	if usage == nil {
		usage = &spec.Usage{}
	}
	usage.Normalize()
	return &TextCompleteResult{
		// 思考模型的正文可能在 reasoning_content 里，这里取能用的那一份。
		Text:          unified.OutputText(),
		Usage:         usage,
		ChannelID:     channel.ID,
		ChannelName:   channel.Name,
		UpstreamModel: upstreamModel,
		LatencyMS:     time.Since(start).Milliseconds(),
	}, nil
}
