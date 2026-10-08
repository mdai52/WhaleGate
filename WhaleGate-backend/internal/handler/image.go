package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/util"
	"github.com/whalegate/whalegate/internal/service"
)

// ImageHandler 对外文生图入口（OpenAI 兼容 /v1/images/generations）。
type ImageHandler struct {
	svc *service.Container
}

// NewImageHandler 创建处理器。
func NewImageHandler(svc *service.Container) *ImageHandler {
	return &ImageHandler{svc: svc}
}

// Generations 文生图：解析 → 密钥策略校验 → 选路转发 → 按 OpenAI 格式回写。
//
// 与 chat 转发一样走「预扣 → 多退少补」：按请求张数预扣额度，
// 成功后按实际出图张数结算，失败全额回补。计费在 service.GenerateImage
// 内完成，工作台入口与之共用一份口径。
func (h *ImageHandler) Generations(c *gin.Context) {
	body, err := readRequestBody(c, h.svc)
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}
	if len(body) == 0 {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.New(apierr.ErrBadRequest, "请求体不能为空"))
		return
	}

	req, err := gateway.ParseImageClientRequest(body)
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.Wrap(apierr.ErrInvalidJSON, err))
		return
	}
	if req.Model == "" {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.New(apierr.ErrInvalidParam, "缺少 model"))
		return
	}

	// 密钥级策略：模型白名单与额度限制
	if idn := currentKeyIdentity(c); idn != nil {
		if !idn.ModelAllowed(req.Model) {
			writeRelayError(c, gateway.ProtocolOpenAI,
				apierr.New(apierr.ErrForbidden, "该 API Key 无权调用模型 "+req.Model))
			return
		}
		if idn.QuotaExceeded() {
			writeRelayError(c, gateway.ProtocolOpenAI,
				apierr.New(apierr.ErrQuotaExceeded, "该 API Key 的额度已用尽"))
			return
		}
	}

	generation, err := h.svc.GenerateImage(c.Request.Context(), service.ImageGenerateInput{
		Model:           req.Model,
		Prompt:          req.Prompt,
		NegativePrompt:  req.NegativePrompt,
		N:               req.N,
		Size:            req.Size,
		Quality:         req.Quality,
		Style:           req.Style,
		ResponseFormat:  req.ResponseFormat,
		User:            req.User,
		Extra:           req.Extra,
		GroupMultiplier: groupMultiplier(h.svc, c),
		// 计费与日志由 GenerateImage 统一完成，两个入口共用一份口径。
		Meta: newImageCallMeta(h.svc, c),
	})
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}
	c.JSON(http.StatusOK, gateway.BuildImageClientResponse(generation.Response))
}

// newImageCallMeta 从请求上下文中提取调用日志所需的来源信息。
func newImageCallMeta(svc *service.Container, c *gin.Context) *service.ImageCallMeta {
	return &service.ImageCallMeta{
		UserID:        CurrentUserID(c),
		APIKeyID:      currentAPIKeyID(c),
		TraceID:       c.GetString(constant.CtxTraceID),
		ClientIP:      util.RealClientIP(c, svc.Config.Server.RealIPHeader),
		RequestScheme: util.RequestScheme(c),
	}
}
