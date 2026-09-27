package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// AdminHandler 管理端接口。
type AdminHandler struct {
	svc *service.Container
}

// NewAdminHandler 创建处理器。
func NewAdminHandler(svc *service.Container) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// ListUsers 分页查询用户。列表场景对邮箱脱敏，避免批量泄露个人信息。
func (h *AdminHandler) ListUsers(c *gin.Context) {
	p := ParsePage(c)
	users, total, err := h.svc.ListUsers(c.Request.Context(), c.Query("keyword"), p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	for i := range users {
		if users[i].Email != "" {
			users[i].Email = service.MaskEmail(users[i].Email)
		}
	}
	response.Page(c, users, total, p.Page, p.PageSize)
}

// ListAuditLogs 分页查询审计日志。
func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	p := ParsePage(c)
	filter := service.AuditFilter{
		Action:   c.Query("action"),
		Start:    parseTime(c, "start"),
		End:      parseTime(c, "end"),
		Page:     p.Page,
		PageSize: p.PageSize,
	}
	if v, err := strconv.ParseUint(c.Query("user_id"), 10, 64); err == nil && v > 0 {
		filter.UserID = uint(v)
	}

	list, total, err := h.svc.ListAuditLogs(c.Request.Context(), filter)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, p.Page, p.PageSize)
}

type statusRequest struct {
	Status int `json:"status" binding:"required"`
}

// SetUserStatus 启用/禁用用户。
func (h *AdminHandler) SetUserStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	var req statusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	if err := h.svc.SetUserStatus(c.Request.Context(), uint(id), req.Status); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": id, "status": req.Status})
}

type quotaRequest struct {
	// Delta 变更额度，单位为「点」，可为负数。
	Delta int64 `json:"delta" binding:"required"`
}

// ChangeQuota 调整用户额度。
func (h *AdminHandler) ChangeQuota(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	var req quotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	if req.Delta == 0 {
		response.Fail(c, apierr.New(apierr.ErrInvalidParam, "额度变更值不能为 0"))
		return
	}
	user, err := h.svc.AdjustQuota(c.Request.Context(), uint(id), req.Delta)
	if err != nil {
		auditFail(h.svc, c, model.AuditQuotaAdjust, "user", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditQuotaAdjust, "user", c.Param("id"),
		fmt.Sprintf("delta=%d", req.Delta))
	response.OK(c, user)
}

// ListRatios 分页查询模型倍率。
func (h *AdminHandler) ListRatios(c *gin.Context) {
	p := ParsePage(c)
	list, total, err := h.svc.ListRatios(c.Request.Context(), p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, p.Page, p.PageSize)
}

// UpsertRatio 新增或更新模型倍率。
func (h *AdminHandler) UpsertRatio(c *gin.Context) {
	var ratio model.ModelRatio
	if err := c.ShouldBindJSON(&ratio); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	saved, err := h.svc.UpsertRatio(c.Request.Context(), &ratio)
	if err != nil {
		auditFail(h.svc, c, model.AuditRatioUpsert, "model_ratio", ratio.Model, err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditRatioUpsert, "model_ratio", saved.Model, "")
	response.OK(c, saved)
}

// DeleteRatio 删除模型倍率。
func (h *AdminHandler) DeleteRatio(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	if err := h.svc.DeleteRatio(c.Request.Context(), uint(id)); err != nil {
		auditFail(h.svc, c, model.AuditRatioDelete, "model_ratio", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditRatioDelete, "model_ratio", c.Param("id"), "")
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// ListLogs 查询全局调用日志。
func (h *AdminHandler) ListLogs(c *gin.Context) {
	p := ParsePage(c)
	filter := service.CallLogFilter{
		Model: c.Query("model"),
		Start: parseTime(c, "start"),
		End:   parseTime(c, "end"),
		Page:  p.Page,
	}
	filter.PageSize = p.PageSize
	if v, err := strconv.ParseUint(c.Query("user_id"), 10, 64); err == nil && v > 0 {
		filter.UserID = uint(v)
	}
	if v, err := strconv.ParseUint(c.Query("api_key_id"), 10, 64); err == nil && v > 0 {
		filter.APIKeyID = uint(v)
	}
	if v, err := strconv.Atoi(c.Query("status")); err == nil {
		filter.Status = v
	}

	list, total, err := h.svc.ListCallLogs(c.Request.Context(), filter)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, p.Page, p.PageSize)
}

// CreateUser 管理端直接创建用户（可指定角色）。
func (h *AdminHandler) CreateUser(c *gin.Context) {
	var in service.RegisterInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	user, err := h.svc.Register(c.Request.Context(), in, true)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, user)
}
