package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// SkillHandler 技能管理（管理端）。
type SkillHandler struct {
	svc *service.Container
}

// NewSkillHandler 构造处理器。
func NewSkillHandler(svc *service.Container) *SkillHandler {
	return &SkillHandler{svc: svc}
}

// List 分页列出技能。
func (h *SkillHandler) List(c *gin.Context) {
	p := ParsePage(c)
	list, total, err := h.svc.ListSkills(c.Request.Context(), c.Query("keyword"), p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, p.Page, p.PageSize)
}

// Scan 扫描技能目录并入库。
func (h *SkillHandler) Scan(c *gin.Context) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = c.ShouldBindJSON(&req)
	count, err := h.svc.ScanSkills(c.Request.Context(), req.Dir)
	if err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditSettingsUpdate, "skill", "scan", "扫描并导入 "+strconv.Itoa(count)+" 个技能")
	response.OK(c, gin.H{"imported": count})
}

// Toggle 启停技能。
func (h *SkillHandler) Toggle(c *gin.Context) {
	rawID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || rawID == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	id := uint(rawID)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, err)
		return
	}
	skill, err := h.svc.ToggleSkill(c.Request.Context(), id, req.Enabled)
	if err != nil {
		response.Fail(c, err)
		return
	}
	state := "停用"
	if req.Enabled {
		state = "启用"
	}
	auditOK(h.svc, c, model.AuditSettingsUpdate, "skill", c.Param("id"), state)
	response.OK(c, skill)
}

// Delete 删除技能。
func (h *SkillHandler) Delete(c *gin.Context) {
	rawID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || rawID == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	id := uint(rawID)
	if err := h.svc.DeleteSkill(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditSettingsUpdate, "skill", c.Param("id"), "删除")
	response.OK(c, gin.H{"id": id, "deleted": true})
}
