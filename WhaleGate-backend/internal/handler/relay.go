package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/util"
	"github.com/whalegate/whalegate/internal/service"
)

// RelayHandler 协议入口：负责把外部协议转换为统一格式、选路、转发并回写响应。
type RelayHandler struct {
	svc *service.Container
}

// NewRelayHandler 创建处理器。
func NewRelayHandler(svc *service.Container) *RelayHandler {
	return &RelayHandler{svc: svc}
}

// maxRequestBody 最大请求体（8MB），避免超大 payload 打爆内存。
const maxRequestBody = 8 << 20

// maxResponseBody 非流式响应最大读取量（32MB）。
const maxResponseBody = 32 << 20

// ListModels 汇总所有启用渠道支持的模型，以 OpenAI 格式返回。
func (h *RelayHandler) ListModels(c *gin.Context) {
	list, err := h.svc.EnabledChannels(c.Request.Context())
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}

	seen := make(map[string]struct{})
	models := make([]map[string]interface{}, 0, 32)
	for _, ch := range list {
		// 别名 fork 出的模型名同样对外暴露。
		for _, name := range ch.ExposedModels() {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			models = append(models, map[string]interface{}{
				"id":       name,
				"object":   "model",
				"created":  ch.CreatedAt.Unix(),
				"owned_by": ch.Name,
			})
		}
	}
	c.JSON(http.StatusOK, map[string]interface{}{"object": "list", "data": models})
}

// ChatCompletions OpenAI 兼容入口（含 stream）。
func (h *RelayHandler) ChatCompletions(c *gin.Context) {
	h.relay(c, gateway.ProtocolOpenAI, "", false)
}

// GeminiNative Gemini 原生入口：/v1beta/models/{model}:generateContent
// 与 :streamGenerateContent（alt=sse）。
func (h *RelayHandler) GeminiNative(c *gin.Context) {
	modelName, action := parseGeminiPath(c.Param("fullpath"))
	if modelName == "" {
		writeRelayError(c, gateway.ProtocolGemini, apierr.New(apierr.ErrInvalidParam, "缺少模型名"))
		return
	}
	if action != geminiActionGenerateContent && action != geminiActionStreamGenerateContent {
		writeRelayError(c, gateway.ProtocolGemini,
			apierr.Errorf(apierr.ErrInvalidParam, "不支持的动作: %s", action))
		return
	}
	h.relay(c, gateway.ProtocolGemini, modelName, action == geminiActionStreamGenerateContent)
}

const (
	geminiActionGenerateContent       = "generateContent"
	geminiActionStreamGenerateContent = "streamGenerateContent"
)

// parseGeminiPath 从形如 /gemini-2.0-flash:streamGenerateContent 的路径中解析模型与动作。
func parseGeminiPath(path string) (modelName, action string) {
	path = strings.TrimPrefix(path, "/")
	if idx := strings.LastIndex(path, ":"); idx >= 0 {
		return path[:idx], path[idx+1:]
	}
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[:idx], path[idx+1:]
	}
	return path, ""
}

// relay 完成一次请求转发：解析 -> 选路 -> 转发（含重试）-> 回写。
func (h *RelayHandler) relay(c *gin.Context, clientProtocol string, pathModel string, forceStream bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRequestBody))
	if err != nil {
		writeRelayError(c, clientProtocol, apierr.Wrap(apierr.ErrBadRequest, err))
		return
	}
	if len(body) == 0 {
		writeRelayError(c, clientProtocol, apierr.New(apierr.ErrBadRequest, "请求体不能为空"))
		return
	}

	req, err := gateway.ParseClientRequest(clientProtocol, body, pathModel)
	if err != nil {
		writeRelayError(c, clientProtocol, err)
		return
	}
	if req.Model == "" {
		writeRelayError(c, clientProtocol, apierr.New(apierr.ErrInvalidParam, "缺少 model"))
		return
	}
	if forceStream {
		req.Stream = true
	}

	// 计费：先按估算用量预扣额度，余额不足直接拒绝。
	// 自用模式下跳过预扣，调用免费。
	ctx := c.Request.Context()
	userID := CurrentUserID(c)
	selfUse := h.svc.SelfUseMode()

	var reservation *service.Reservation
	if !selfUse {
		estimate := h.svc.EstimateUsage(req)
		reservePoints, err := h.svc.PointsFor(ctx, req.Model, estimate)
		if err != nil {
			writeRelayError(c, clientProtocol, err)
			return
		}
		reservation, err = h.svc.ReserveQuota(ctx, userID, reservePoints)
		if err != nil {
			reservation = nil
			writeRelayError(c, clientProtocol, err)
			return
		}
	}

	// Skills 与 MCP 注入：解析后只做一次，重试不会重复注入。
	autoExecute, maxRounds := h.applyEnhancements(c, req)

	start := time.Now()
	attempts := 1 + h.svc.Config.Gateway.MaxRetries
	exclude := make(map[uint]struct{}, attempts)

	var lastErr error
	var toolRounds int
	for attempt := 0; attempt < attempts; attempt++ {
		channel, err := h.svc.SelectChannel(ctx, req.Model, exclude)
		if err != nil {
			// 已有真实失败原因时保留它，避免被"无可用渠道"掩盖。
			if lastErr == nil {
				lastErr = err
			}
			break
		}

		var result *relayOutcome
		if autoExecute {
			result, toolRounds, err = h.forwardWithToolLoop(ctx, c, clientProtocol, channel, req, maxRounds)
		} else {
			result, err = h.forward(ctx, c, clientProtocol, channel, req)
		}
		if err == nil {
			h.svc.RecordChannelSuccess(ctx, channel)
			logRelaySuccess(h.svc, channel, req, result, start)
			h.settleAndLog(c, clientProtocol, req, channel, result, reservation, start,
				model.CallStatusSuccess, c.Writer.Status(), 0, "", selfUse, toolRounds)
			return
		}

		h.svc.RecordChannelFailure(ctx, channel, err.Error())
		lastErr = err
		exclude[channel.ID] = struct{}{}

		// 客户端已断开或错误不可重试时立即终止。
		if ctx.Err() != nil || !isRetryable(err) {
			break
		}
	}

	if ctx.Err() != nil {
		// 客户端断开：全额回补，不重复写响应。
		if reservation != nil {
			reservation.Release(context.Background())
		}
		return
	}

	h.settleAndLog(c, clientProtocol, req, nil, nil, reservation, start,
		model.CallStatusFailed, 0, apierr.CodeOf(lastErr), errorMessage(lastErr), selfUse, toolRounds)
	writeRelayError(c, clientProtocol, lastErr)
}

// settleAndLog 结算额度并异步落调用日志。
func (h *RelayHandler) settleAndLog(
	c *gin.Context,
	clientProtocol string,
	req *spec.UnifiedRequest,
	channel *model.Channel,
	outcome *relayOutcome,
	reservation *service.Reservation,
	start time.Time,
	status int,
	httpStatus int,
	errorCode int,
	errorMessage string,
	selfUse bool,
	toolRounds int,
) {
	usage, confidence := resolveUsage(h.svc, req, outcome)

	points, err := h.svc.PointsFor(c.Request.Context(), req.Model, usage)
	if err != nil {
		points = 0
		if h.svc.Logger != nil {
			h.svc.Logger.Warn("计算点数失败", zap.Error(err))
		}
	}
	if reservation != nil {
		if status == model.CallStatusSuccess {
			reservation.Settle(c.Request.Context(), points)
		} else {
			// 失败全额回补。
			reservation.Release(c.Request.Context())
			points = 0
		}
	}
	// 自用模式：reservation 为 nil，不扣费；points 仍记录"应扣点数"用于成本分析。

	// 密钥使用统计（成功才计入）
	if status == model.CallStatusSuccess {
		h.svc.UpdateAPIKeyUsage(c.Request.Context(), currentAPIKeyID(c), usage.TotalTokens)
	}

	entry := &model.CallLog{
		UserID:           CurrentUserID(c),
		APIKeyID:         currentAPIKeyID(c),
		SelfUse:          selfUse,
		Model:            req.Model,
		Protocol:         clientProtocol,
		Stream:           req.Stream,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		TotalTokens:      usage.TotalTokens,
		ToolRounds:       toolRounds,
		Points:           points,
		LatencyMS:        time.Since(start).Milliseconds(),
		Status:           status,
		HTTPStatus:       httpStatus,
		ErrorCode:        errorCode,
		ErrorMessage:     truncateText(errorMessage, 500),
		UsageConfidence:  confidence,
		TraceID:          c.GetString(constant.CtxTraceID),
		ClientIP:         c.ClientIP(),
		CreatedAt:        time.Now(),
	}
	if channel != nil {
		entry.ChannelID = channel.ID
		entry.ChannelName = channel.Name
	}
	if outcome != nil {
		entry.FirstTokenMS = outcome.FirstToken.Milliseconds()
		entry.UpstreamModel = outcome.UpstreamModel
		if outcome.Params != nil {
			entry.ParamsApplied = strings.Join(outcome.Params.Applied, ",")
			entry.ParamsDropped = strings.Join(outcome.Params.Dropped, ",")
		}
	}

	h.svc.Logs.Submit(entry)
}

// resolveUsage 决定计量口径：优先上游上报，缺失时按请求参数估算。
func resolveUsage(svc *service.Container, req *spec.UnifiedRequest, outcome *relayOutcome) (*spec.Usage, string) {
	if outcome != nil && outcome.Usage != nil &&
		(outcome.Usage.PromptTokens > 0 || outcome.Usage.CompletionTokens > 0) {
		u := *outcome.Usage
		u.Normalize()
		return &u, model.ConfidenceReported
	}
	return svc.EstimateUsage(req), model.ConfidenceEstimated
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return truncateText(err.Error(), 500)
}

// currentAPIKeyID 读取鉴权中间件写入的密钥 ID。
func currentAPIKeyID(c *gin.Context) uint {
	v, ok := c.Get(constant.CtxAPIKeyID)
	if !ok {
		return 0
	}
	switch id := v.(type) {
	case uint:
		return id
	case int:
		return uint(id)
	case float64:
		return uint(id)
	default:
		return 0
	}
}

// relayOutcome 一次转发的结果摘要。
type relayOutcome struct {
	Usage      *gateway.Usage
	FirstToken time.Duration
	// UpstreamModel 经别名/映射改写后实际发往上游的模型名。
	UpstreamModel string
	// Params 自定义参数的分配结果。
	Params *gateway.ParamReport
}

// 参数分配结果回显响应头，便于客户端排查。
const (
	headerParamsApplied = "X-WG-Params-Applied"
	headerParamsDropped = "X-WG-Params-Dropped"
)

// forward 向指定渠道发起一次请求并回写响应。
func (h *RelayHandler) forward(
	ctx context.Context,
	c *gin.Context,
	clientProtocol string,
	channel *model.Channel,
	req *gateway.UnifiedRequest,
) (*relayOutcome, error) {
	// 凭证优先：绑定 OAuth 凭证时使用其令牌（过期自动续期），否则使用静态 api_key
	apiKey, err := h.svc.ChannelUpstreamKey(ctx, channel)
	if err != nil {
		return nil, err
	}

	// 模型别名改写：一个上游模型可暴露为多个对外模型名，并携带专属参数注入。
	upstreamModel, override := channel.ResolveModel(req.Model)
	upstreamReq := *req
	upstreamReq.Model = upstreamModel

	payload, err := gateway.BuildUpstreamRequest(channel.Type, &upstreamReq)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}

	// 自定义参数自动识别与分配：按上游声明的能力写入对应协议字段，不支持的丢弃。
	report := gateway.ApplyParams(payload, channel.Type, gateway.CollectParams(req), channel.Schema())
	if len(report.Applied) > 0 {
		c.Header(headerParamsApplied, strings.Join(report.Applied, ","))
	}
	if len(report.Dropped) > 0 {
		c.Header(headerParamsDropped, strings.Join(report.Dropped, ","))
	}
	if len(report.Dropped) > 0 && h.svc.Logger != nil {
		h.svc.Logger.Info("部分自定义参数未被上游支持",
			zap.String("model", req.Model), zap.Strings("dropped", report.Dropped))
	}

	if len(override) > 0 {
		// override-raw：强制追加 tools 或改写任意字段，优先级最高
		payload = util.MergeMap(payload, override)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	upstreamReqModel := upstreamReq.Model
	url := gateway.BuildUpstreamURL(channel.Type, channel.BaseURL, upstreamReqModel, req.Stream)
	headerName, headerValue := gateway.BuildUpstreamAuthHeader(channel.Type, apiKey)
	headers := map[string]string{headerName: headerValue}

	upstream := gateway.NewUpstream(channelTimeout(h.svc, channel))
	if req.Stream {
		return h.forwardStream(ctx, c, clientProtocol, channel, upstream, url, headers, body, upstreamReq.Model, report)
	}
	return h.forwardOnce(ctx, c, clientProtocol, channel, upstream, url, headers, body, upstreamReq.Model, report)
}

func (h *RelayHandler) forwardOnce(
	ctx context.Context,
	c *gin.Context,
	clientProtocol string,
	channel *model.Channel,
	upstream *gateway.Upstream,
	url string,
	headers map[string]string,
	body []byte,
	upstreamModel string,
	report gateway.ParamReport,
) (*relayOutcome, error) {
	resp, err := upstream.Do(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, classifyUpstreamError(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, classifyUpstreamError(err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, upstreamHTTPError(channel.Type, resp.StatusCode, raw)
	}

	unified, err := gateway.ParseUpstreamResponse(channel.Type, raw)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrUpstreamInvalid, err)
	}
	unified.Model = reqModel(channel, unified.Model)
	gateway.WriteClientResponse(clientProtocol, c, unified)
	return &relayOutcome{Usage: unified.Usage, UpstreamModel: upstreamModel, Params: &report}, nil
}

func (h *RelayHandler) forwardStream(
	ctx context.Context,
	c *gin.Context,
	clientProtocol string,
	channel *model.Channel,
	upstream *gateway.Upstream,
	url string,
	headers map[string]string,
	body []byte,
	upstreamModel string,
	report gateway.ParamReport,
) (*relayOutcome, error) {
	resp, err := upstream.DoStream(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, classifyUpstreamError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, upstreamHTTPError(channel.Type, resp.StatusCode, raw)
	}

	gateway.WriteClientStreamHeader(clientProtocol, c)
	flusher, _ := c.Writer.(http.Flusher)

	scanner := gateway.NewSSEScanner(resp.Body)
	usage := &gateway.Usage{}
	var firstToken time.Duration
	start := time.Now()

	for {
		if err := ctx.Err(); err != nil {
			return &relayOutcome{Usage: usage, FirstToken: firstToken, UpstreamModel: upstreamModel, Params: &report}, nil
		}
		payload, err := scanner.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if ctx.Err() == nil {
				_ = gateway.WriteClientStreamError(clientProtocol, c.Writer, "上游流中断")
			}
			break
		}

		chunk, err := gateway.ParseUpstreamStreamEvent(channel.Type, payload)
		if err != nil || chunk == nil {
			continue
		}
		if chunk.IsEmpty() && chunk.Usage == nil {
			continue
		}

		if firstToken == 0 && chunk.Choices != nil {
			firstToken = time.Since(start)
		}
		usage.Add(chunk.Usage)

		chunk.Model = reqModel(channel, chunk.Model)
		if err := gateway.WriteClientStreamChunk(clientProtocol, c.Writer, chunk); err != nil {
			break
		}
		if flusher != nil {
			flusher.Flush()
		}
	}

	_ = gateway.WriteClientStreamTail(clientProtocol, c.Writer)
	if flusher != nil {
		flusher.Flush()
	}
	return &relayOutcome{Usage: usage, FirstToken: firstToken, UpstreamModel: upstreamModel, Params: &report}, nil
}

// ---------------------------------------------------------------- 辅助

func reqModel(channel *model.Channel, upstreamModel string) string {
	if upstreamModel != "" {
		return upstreamModel
	}
	return channel.Name
}

func channelTimeout(svc *service.Container, channel *model.Channel) time.Duration {
	if channel.TimeoutSeconds > 0 {
		return time.Duration(channel.TimeoutSeconds) * time.Second
	}
	if svc.Config.Gateway.UpstreamTimeout > 0 {
		return svc.Config.Gateway.UpstreamTimeout
	}
	return 300 * time.Second
}

func classifyUpstreamError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apierr.Wrap(apierr.ErrUpstreamTimeout, err)
	}
	return apierr.Wrap(apierr.ErrUpstream, err)
}

func upstreamHTTPError(channelType string, status int, raw []byte) error {
	msg := gateway.ExtractUpstreamError(channelType, raw)
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return apierr.Errorf(apierr.ErrUpstream, "上游鉴权失败: %s", truncateText(msg, 300))
	case http.StatusTooManyRequests:
		return apierr.Errorf(apierr.ErrRateLimited, "上游限流: %s", truncateText(msg, 300))
	case http.StatusNotFound:
		return apierr.Errorf(apierr.ErrModelNotFound, "上游资源不存在: %s", truncateText(msg, 300))
	case http.StatusGatewayTimeout:
		return apierr.Errorf(apierr.ErrUpstreamTimeout, "上游超时: %s", truncateText(msg, 300))
	default:
		return apierr.Errorf(apierr.ErrUpstream, "上游返回 %d: %s", status, truncateText(msg, 300))
	}
}

// isRetryable 判定错误是否值得切换渠道重试。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	be := apierr.From(err)
	switch be.Code {
	case apierr.CodeModelNotFound, apierr.CodeInvalidParam, apierr.CodeBadRequest, apierr.CodeInvalidJSON:
		return false
	default:
		return true
	}
}

// writeRelayError 统一输出转发错误。
func writeRelayError(c *gin.Context, clientProtocol string, err error) {
	if err == nil {
		return
	}
	be := apierr.From(err)
	gateway.WriteClientError(clientProtocol, c, be.HTTP, be.Message)
}

func logRelaySuccess(svc *service.Container, channel *model.Channel, req *gateway.UnifiedRequest, outcome *relayOutcome, start time.Time) {
	if svc.Logger == nil {
		return
	}
	fields := []zap.Field{
		zap.Uint("channel_id", channel.ID),
		zap.String("channel", channel.Name),
		zap.String("model", req.Model),
		zap.Bool("stream", req.Stream),
		zap.Duration("latency", time.Since(start)),
	}
	if outcome != nil && outcome.Usage != nil {
		fields = append(fields,
			zap.Int("prompt_tokens", outcome.Usage.PromptTokens),
			zap.Int("completion_tokens", outcome.Usage.CompletionTokens),
			zap.Duration("first_token", outcome.FirstToken),
		)
	}
	svc.Logger.Info("转发完成", fields...)
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
