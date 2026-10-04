package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// PlaygroundHandler 参数调试接口。
//
// 走登录态而非 API Key：目的是让用户在拿到密钥前就能先试清楚参数怎么传，
// 且预览不向上游发起请求，因此不消耗额度、不计入调用日志。
type PlaygroundHandler struct {
	svc *service.Container
}

// NewPlaygroundHandler 构造参数调试处理器。
func NewPlaygroundHandler(svc *service.Container) *PlaygroundHandler {
	return &PlaygroundHandler{svc: svc}
}

// Models 列出可用于调试的模型：GET /api/v1/playground/models
func (h *PlaygroundHandler) Models(c *gin.Context) {
	models, err := h.svc.ListPlaygroundModels(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, models)
}

// Preview 预览参数分配结果：POST /api/v1/playground/params/preview
func (h *PlaygroundHandler) Preview(c *gin.Context) {
	var in service.ParamPreviewInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	out, err := h.svc.PreviewParams(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}
