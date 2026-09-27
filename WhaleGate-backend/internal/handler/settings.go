package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// SettingsHandler 全局设置（仅管理员）。
type SettingsHandler struct {
	svc *service.Container
}

// NewSettingsHandler 构造设置处理器。
func NewSettingsHandler(svc *service.Container) *SettingsHandler {
	return &SettingsHandler{svc: svc}
}

// Get 读取全局设置。
func (h *SettingsHandler) Get(c *gin.Context) {
	settings, err := h.svc.GetSettings(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, settings)
}

// Update 更新全局设置。
func (h *SettingsHandler) Update(c *gin.Context) {
	var in service.Settings
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, err)
		return
	}

	before, _ := h.svc.GetSettings(c.Request.Context())
	updated, err := h.svc.UpdateSettings(c.Request.Context(), CurrentUserID(c), in)
	if err != nil {
		response.Fail(c, err)
		return
	}

	if before != nil && before.SelfUseMode != updated.SelfUseMode {
		state := "关闭"
		if updated.SelfUseMode {
			state = "开启"
		}
		auditOK(h.svc, c, model.AuditSettingsUpdate, "settings", "self_use_mode", "自用模式"+state)
	}

	response.OK(c, updated)
}
