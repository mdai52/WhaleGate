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

// maxResponseBody 非流式响应最大读取量（32MB）。
const maxResponseBody = 32 << 20

// readRequestBody 按 server.body_limit 读取请求体。
//
// 上限必须取自配置：写死 8MB 时，管理员把 body_limit 放宽到 32MB 之后
// 请求会先通过中间件、再被这里截断，得到一个被切坏的 JSON 与一句看不懂的解析错误。
func readRequestBody(c *gin.Context, svc *service.Container) ([]byte, error) {
	limit := svc.BodyLimitBytes()
	// 多读 1 字节用于识别"恰好超限"，否则无法区分刚好等于上限与超限。
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrBadRequest, err)
	}
	if int64(len(body)) > limit {
		return nil, apierr.Errorf(apierr.ErrBadRequest, "请求体超过 %d 字节上限", limit)
	}
	return body, nil
}

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

// Messages Anthropic Messages API 入站入口：POST /v1/messages
//
// 供 Claude Code、CCSwitch 这类只认 /v1/messages 的客户端接入。
// 请求体按 Anthropic 协议解析后归一为统一格式，再按渠道协议转发，
// 因此这些客户端可以调用任意渠道上的模型，不受上游协议限制。
func (h *RelayHandler) Messages(c *gin.Context) {
	h.relay(c, gateway.ProtocolAnthropic, "", false)
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
	body, err := readRequestBody(c, h.svc)
	if err != nil {
		writeRelayError(c, clientProtocol, err)
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
	start := time.Now()

	var reservation *service.Reservation
	if !selfUse {
		estimate := h.svc.EstimateUsage(req)
		reservePoints, err := h.svc.PointsFor(ctx, req.Model, estimate, h.groupMultiplier(c))
		if err != nil {
			h.reject(c, clientProtocol, req, start, selfUse, nil, err)
			return
		}
		reservation, err = h.svc.ReserveQuota(ctx, userID, reservePoints)
		if err != nil {
			h.reject(c, clientProtocol, req, start, selfUse, nil, err)
			return
		}
	}

	// Skills 与 MCP 注入：解析后只做一次，重试不会重复注入。
	autoExecute, maxRounds := h.applyEnhancements(c, req)

	// 密钥级策略：模型白名单与额度限制
	if idn := currentKeyIdentity(c); idn != nil {
		if !idn.ModelAllowed(req.Model) {
			h.reject(c, clientProtocol, req, start, selfUse, reservation,
				apierr.New(apierr.ErrForbidden, "该 API Key 无权调用模型 "+req.Model))
			return
		}
		if idn.QuotaExceeded() {
			h.reject(c, clientProtocol, req, start, selfUse, reservation,
				apierr.New(apierr.ErrQuotaExceeded, "该 API Key 的额度已用尽"))
			return
		}
	}

	attempts := 1 + h.svc.Config.Gateway.MaxRetries
	if attempts < 1 {
		attempts = 1
	}
	exclude := make(map[uint]struct{}, attempts)
	backoff := h.svc.Config.Gateway.RetryBackoff

	var lastErr error
	var toolRounds int
	// streamed 标记是否已经向客户端写出过流式响应：此时不能再改用 JSON 错误覆盖。
	streamed := false
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

		// 客户端断开与渠道健康度无关：不记失败、不重试，直接全额回补。
		if ctx.Err() != nil {
			if reservation != nil {
				reservation.Release(context.Background())
			}
			return
		}

		// 流式响应头已写出：错误只能留在流里，换渠道重发会追加出第二份流。
		var committed *streamCommittedError
		if errors.As(err, &committed) {
			streamed = true
			if committed.cause != nil {
				err = committed.cause
			}
		}
		retryable := isRetryable(err)
		// 客户端自身造成的错误（参数非法、上下文超长等）不该计入渠道健康度：
		// 否则一次坏请求会让所有渠道各记一次失败，达到阈值后把全部渠道一起熔断。
		if streamed || retryable {
			h.svc.RecordChannelFailure(ctx, channel, err.Error())
		}
		lastErr = err
		exclude[channel.ID] = struct{}{}

		// 响应已提交或错误不可重试时立即终止。
		if streamed || !retryable {
			break
		}
		// 退避后再试：刚报错的上游通常需要一点恢复时间，立刻重试只会再撞一次。
		if backoff > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
		}
	}

	h.settleAndLog(c, clientProtocol, req, nil, nil, reservation, start,
		model.CallStatusFailed, 0, apierr.CodeOf(lastErr), errorMessage(lastErr), selfUse, toolRounds)
	// 流式失败的原因已经写进流里，不能再往响应体追加 JSON。
	if !streamed {
		writeRelayError(c, clientProtocol, lastErr)
	}
}

// reject 记录一次「进入转发前就被拒绝」的请求：回补预扣、落调用日志并回写错误。
//
// 余额不足、密钥无权调用模型、密钥额度用尽这类拒绝如果不留痕，
// 用户在调用历史里查不到被拒原因，只能看到一个空列表。
func (h *RelayHandler) reject(
	c *gin.Context,
	clientProtocol string,
	req *gateway.UnifiedRequest,
	start time.Time,
	selfUse bool,
	reservation *service.Reservation,
	err error,
) {
	if reservation != nil {
		reservation.Release(settleContext(c))
	}
	h.settleAndLog(c, clientProtocol, req, nil, nil, nil, start,
		model.CallStatusFailed, apierr.From(err).HTTP, apierr.CodeOf(err),
		errorMessage(err), selfUse, 0)
	writeRelayError(c, clientProtocol, err)
}

// settleContext 返回结算与落日志使用的上下文。
//
// 客户端提前断开时 gin 会取消请求上下文，直接沿用会让 Redis 结算脚本与数据库写入全部失败，
// 预扣的额度既没结算也没回补（用户余额凭空消失），因此回退到根上下文。
// 与 service 层生图 / 视频结算的 context.Background() 回退口径一致。
func settleContext(c *gin.Context) context.Context {
	if ctx := c.Request.Context(); ctx.Err() == nil {
		return ctx
	}
	return context.Background()
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
	// 结算、密钥统计与调用日志都要写 Redis / 数据库。客户端断开时请求上下文已取消，
	// 直接沿用会让预扣既没有结算也没有回补——用户余额凭空消失。因此回退到根上下文。
	settleCtx := settleContext(c)

	usage, confidence := resolveUsage(h.svc, req, outcome)

	points, err := h.svc.PointsFor(settleCtx, req.Model, usage, h.groupMultiplier(c))
	if err != nil {
		// 不能置 0：Settle 的语义是"多退少补"，置 0 等于把这一次调用全额退成免费。
		// 算不出实际点数时按预扣值结算，宁可少退也不白送。
		points = 0
		if reservation != nil {
			points = reservation.Reserved
		}
		if h.svc.Logger != nil {
			h.svc.Logger.Warn("计算点数失败，按预扣值结算", zap.Error(err))
		}
	}
	if reservation != nil {
		if status == model.CallStatusSuccess {
			reservation.Settle(settleCtx, points)
		} else {
			// 失败全额回补。
			reservation.Release(settleCtx)
			points = 0
		}
	}
	// 自用模式：reservation 为 nil，不扣费；points 仍记录"应扣点数"用于成本分析。

	// 密钥使用统计（成功才计入）
	if status == model.CallStatusSuccess {
		h.svc.UpdateAPIKeyUsage(settleCtx, currentAPIKeyID(c), usage.TotalTokens, points)
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
		ClientIP:         util.RealClientIP(c, h.svc.Config.Server.RealIPHeader),
		RequestScheme:    util.RequestScheme(c),
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

// currentKeyIdentity 读取鉴权中间件注入的密钥身份（含白名单与额度配置）。
func currentKeyIdentity(c *gin.Context) *service.KeyIdentity {
	v, ok := c.Get(constant.CtxAPIKey)
	if !ok {
		return nil
	}
	idn, _ := v.(*service.KeyIdentity)
	return idn
}

// groupMultiplier 取当前密钥所属分组的倍率系数；未分组或分组不可用时为 1。
//
// 按请求解析而不是折进 KeyIdentity：密钥缓存最长 24 小时，
// 折进去会让管理员调整分组倍率后长时间不生效。
func (h *RelayHandler) groupMultiplier(c *gin.Context) float64 {
	return groupMultiplier(h.svc, c)
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
	passthrough := gateway.ResolvePassthrough(channel.Schema(), h.svc.ParamPassthrough())
	report := gateway.ApplyParams(payload, channel.Type, gateway.CollectParams(req), channel.Schema(), passthrough)
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
	headers := gateway.BuildUpstreamHeaders(channel.Type, apiKey)

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

// streamCommittedError 标记「流式响应头已写出」之后的失败。
//
// 此时错误只能写进流里，既不能再改用 JSON 错误覆盖响应，
// 也不能换渠道重发——重发会在同一个响应体里追加第二份流。
// 上层据此只做结算与收尾，不再重试、不再改写响应。
type streamCommittedError struct{ cause error }

func (e *streamCommittedError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return "上游流中断"
}

func (e *streamCommittedError) Unwrap() error { return e.cause }

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
	// 按协议选择解码器：Anthropic 的工具参数分片下发，解码器需贯穿整个流。
	decoder := gateway.NewStreamDecoder(channel.Type)
	// 客户端侧同样需要状态：Anthropic 入站的 SSE 是严格有序的事件序列。
	encoder := gateway.NewClientStreamEncoder(clientProtocol, c.Writer)
	usage := &gateway.Usage{}
	var firstToken time.Duration
	start := time.Now()

	// fail 收尾一条已经开始的流：把错误写进流里，并以 committed 标记返回，
	// 让上层按失败结算而不是当成一次成功调用。
	fail := func(cause error) (*relayOutcome, error) {
		_ = encoder.WriteError(errorMessage(cause))
		_ = encoder.Close()
		if flusher != nil {
			flusher.Flush()
		}
		return &relayOutcome{Usage: usage, FirstToken: firstToken, UpstreamModel: upstreamModel, Params: &report},
			&streamCommittedError{cause: cause}
	}

	for {
		if err := ctx.Err(); err != nil {
			// 客户端断开：响应已无从写起，交给上层全额回补。
			return &relayOutcome{Usage: usage, FirstToken: firstToken, UpstreamModel: upstreamModel, Params: &report}, err
		}
		payload, err := scanner.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// 上游连接中断：内容不完整，按失败结算。
			return fail(apierr.Wrap(apierr.ErrUpstream, err))
		}

		chunk, err := decoder.Decode(payload)
		if err != nil {
			// 上游在流里返回的 error 事件（overloaded / rate_limit…）或无法解析的块。
			// 不能 continue 吞掉，否则客户端只收到一个空流，网关却记一次成功。
			return fail(apierr.Wrap(apierr.ErrUpstream, err))
		}
		if chunk == nil {
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
		if err := encoder.Write(chunk); err != nil {
			return fail(apierr.Wrap(apierr.ErrUpstream, err))
		}
		if flusher != nil {
			flusher.Flush()
		}
	}

	_ = encoder.Close()
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
//
// 明细优先：上游返回的错误（"上游返回 400: {...}"）、参数被丢弃的原因等
// 都在 Detail 里，调用方需要看到真实原因才能自行修正；
// 只有 Detail 为空的内部错误（如网络异常）才回退到统一文案，
// 避免把内部实现细节暴露出去。
func writeRelayError(c *gin.Context, clientProtocol string, err error) {
	if err == nil {
		return
	}
	be := apierr.From(err)
	message := be.Message
	if be.Detail != "" {
		message = be.Detail
	}
	gateway.WriteClientError(clientProtocol, c, be.HTTP, message)
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
