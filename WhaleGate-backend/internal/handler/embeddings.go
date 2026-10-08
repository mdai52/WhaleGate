package handler

import (
	"context"
	"encoding/json"
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

// EmbeddingsHandler OpenAI 兼容的向量生成入口：POST /v1/embeddings。
//
// 向量生成是"请求即结果"的简单转发：协议只有 model + input + 少量可选字段，
// 没有流式与工具调用。计费按 token（上游返回 usage），与 chat 同一套倍率表。
type EmbeddingsHandler struct {
	svc *service.Container
}

// NewEmbeddingsHandler 创建处理器。
func NewEmbeddingsHandler(svc *service.Container) *EmbeddingsHandler {
	return &EmbeddingsHandler{svc: svc}
}

// Embeddings 转发向量生成请求。
func (h *EmbeddingsHandler) Embeddings(c *gin.Context) {
	body, err := readRequestBody(c, h.svc)
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}
	if len(body) == 0 {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.New(apierr.ErrBadRequest, "请求体不能为空"))
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.New(apierr.ErrInvalidJSON, "请求体不是合法 JSON"))
		return
	}
	modelName, _ := payload["model"].(string)
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.New(apierr.ErrInvalidParam, "缺少 model"))
		return
	}
	if _, ok := payload["input"]; !ok {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.New(apierr.ErrInvalidParam, "缺少 input"))
		return
	}

	// 自定义字段原样保留：除 model 外全部透传，由上游裁决。
	extra := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		if k == "model" {
			continue
		}
		extra[k] = v
	}

	ctx := c.Request.Context()
	userID := CurrentUserID(c)
	selfUse := h.svc.SelfUseMode()
	start := time.Now()

	// 计费：input 长度可精确估算，先预扣再按上游 usage 结算。
	var reservation *service.Reservation
	if !selfUse {
		estimate := &spec.Usage{PromptTokens: estimateEmbeddingTokens(payload["input"]), TotalTokens: estimateEmbeddingTokens(payload["input"])}
		reservePoints, err := h.svc.PointsFor(ctx, modelName, estimate, groupMultiplier(h.svc, c))
		if err != nil {
			h.reject(c, modelName, start, selfUse, nil, err)
			return
		}
		reservation, err = h.svc.ReserveQuota(ctx, userID, reservePoints)
		if err != nil {
			h.reject(c, modelName, start, selfUse, nil, err)
			return
		}
	}

	// 密钥级策略：模型白名单与额度限制
	if idn := currentKeyIdentity(c); idn != nil {
		if !idn.ModelAllowed(modelName) {
			h.reject(c, modelName, start, selfUse, reservation, apierr.New(apierr.ErrForbidden, "该 API Key 无权调用模型 "+modelName))
			return
		}
		if idn.QuotaExceeded() {
			h.reject(c, modelName, start, selfUse, reservation, apierr.New(apierr.ErrQuotaExceeded, "该 API Key 的额度已用尽"))
			return
		}
	}

	channel, err := h.svc.SelectChannel(ctx, modelName, nil)
	if err != nil {
		h.reject(c, modelName, start, selfUse, reservation, err)
		return
	}
	upstreamModel, override := channel.ResolveModel(modelName)
	upstreamPayload := map[string]interface{}{"model": upstreamModel, "input": payload["input"]}
	for k, v := range extra {
		upstreamPayload[k] = v
	}
	if len(override) > 0 {
		upstreamPayload = util.MergeMap(upstreamPayload, override)
	}
	requestBody, err := json.Marshal(upstreamPayload)
	if err != nil {
		h.reject(c, modelName, start, selfUse, reservation, apierr.Wrap(apierr.ErrInternal, err))
		return
	}

	apiKey, err := h.svc.ChannelUpstreamKey(ctx, channel)
	if err != nil {
		h.reject(c, modelName, start, selfUse, reservation, err)
		return
	}

	upstream := gateway.NewUpstream(channelTimeout(h.svc, channel))
	url := gateway.BuildUpstreamURL(channel.Type, channel.BaseURL, upstreamModel, false)
	resp, err := upstream.Do(ctx, http.MethodPost, url, gateway.BuildUpstreamHeaders(channel.Type, apiKey), requestBody)
	if err != nil {
		h.reject(c, modelName, start, selfUse, reservation, classifyUpstreamError(err))
		return
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		h.reject(c, modelName, start, selfUse, reservation, classifyUpstreamError(err))
		return
	}
	if resp.StatusCode >= http.StatusBadRequest {
		h.reject(c, modelName, start, selfUse, reservation,
			upstreamHTTPError(channel.Type, resp.StatusCode, raw))
		return
	}

	usage := parseEmbeddingUsage(raw)
	points, err := h.svc.PointsFor(ctx, modelName, usage, groupMultiplier(h.svc, c))
	if err != nil {
		points = 0
		if h.svc.Logger != nil {
			h.svc.Logger.Warn("计算向量点数失败", zap.Error(err))
		}
	}
	if reservation != nil {
		reservation.Settle(ctx, points)
	}
	h.svc.UpdateAPIKeyUsage(ctx, currentAPIKeyID(c), usage.TotalTokens, points)

	entry := &model.CallLog{
		UserID:          userID,
		APIKeyID:        currentAPIKeyID(c),
		SelfUse:         selfUse,
		Model:           modelName,
		Protocol:        gateway.ProtocolOpenAI,
		PromptTokens:    usage.PromptTokens,
		TotalTokens:     usage.TotalTokens,
		Points:          points,
		LatencyMS:       time.Since(start).Milliseconds(),
		Status:          model.CallStatusSuccess,
		HTTPStatus:      http.StatusOK,
		UsageConfidence: model.ConfidenceReported,
		TraceID:         c.GetString(constant.CtxTraceID),
		ClientIP:        util.RealClientIP(c, h.svc.Config.Server.RealIPHeader),
		CreatedAt:       time.Now(),
	}
	entry.ChannelID = channel.ID
	entry.ChannelName = channel.Name
	entry.UpstreamModel = upstreamModel
	h.svc.Logs.Submit(entry)
	h.svc.RecordChannelSuccess(ctx, channel)

	// 上游响应原样回写：向量结果体量可能很大，重编码只会浪费内存。
	c.Data(http.StatusOK, "application/json", raw)
}

// reject 记录失败并回写错误；已有预扣时全额回补。
func (h *EmbeddingsHandler) reject(
	c *gin.Context, modelName string, start time.Time, selfUse bool,
	reservation *service.Reservation, err error,
) {
	if reservation != nil {
		releaseCtx := c.Request.Context()
		if releaseCtx.Err() != nil {
			releaseCtx = context.Background()
		}
		reservation.Release(releaseCtx)
	}
	be := apierr.From(err)
	entry := &model.CallLog{
		UserID:          CurrentUserID(c),
		APIKeyID:        currentAPIKeyID(c),
		SelfUse:         selfUse,
		Model:           modelName,
		Protocol:        gateway.ProtocolOpenAI,
		Status:          model.CallStatusFailed,
		HTTPStatus:      be.HTTP,
		ErrorCode:       be.Code,
		ErrorMessage:    truncateText(err.Error(), 500),
		LatencyMS:       time.Since(start).Milliseconds(),
		UsageConfidence: model.ConfidenceEstimated,
		TraceID:         c.GetString(constant.CtxTraceID),
		ClientIP:        util.RealClientIP(c, h.svc.Config.Server.RealIPHeader),
		CreatedAt:       time.Now(),
	}
	h.svc.Logs.Submit(entry)
	writeRelayError(c, gateway.ProtocolOpenAI, err)
}

// estimateEmbeddingTokens 粗略估算输入 token：约 4 字节 ≈ 1 token。
func estimateEmbeddingTokens(input interface{}) int {
	total := 0
	switch typed := input.(type) {
	case string:
		total = len(typed)
	case []interface{}:
		for _, item := range typed {
			switch text := item.(type) {
			case string:
				total += len(text)
			case []interface{}:
				// token 数组（int）形式的输入直接按元素数计。
				total += len(text) * 4
			}
		}
	case []float64:
		total = len(typed) * 4
	}
	if total < 1 {
		return 1
	}
	return total / 4
}

// parseEmbeddingUsage 从上游响应中提取用量；缺失时返回零值。
func parseEmbeddingUsage(raw []byte) *spec.Usage {
	var resp struct {
		Usage *struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Usage == nil {
		return &spec.Usage{}
	}
	return &spec.Usage{
		PromptTokens: resp.Usage.PromptTokens,
		TotalTokens:  resp.Usage.TotalTokens,
	}
}
