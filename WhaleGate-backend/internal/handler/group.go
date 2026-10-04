package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// GroupHandler 分组管理接口。
//
// 读接口对登录用户开放（密钥表单需要选择分组），
// 增删改仅管理员可用，注册在 /api/v1/admin/groups 下。
type GroupHandler struct {
	svc *service.Container
}

// NewGroupHandler 构造分组处理器。
func NewGroupHandler(svc *service.Container) *GroupHandler {
	return &GroupHandler{svc: svc}
}

// Options 列出启用中的分组：GET /api/v1/groups
//
// 返回倍率与限流配置，便于用户在创建密钥时就知道该分组的实际策略。
func (h *GroupHandler) Options(c *gin.Context) {
	groups, err := h.svc.ListEnabledGroups(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, groups)
}

// List 列出全部分组（含已禁用）：GET /api/v1/admin/groups
func (h *GroupHandler) List(c *gin.Context) {
	groups, err := h.svc.ListGroups(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, groups)
}

// Create 新建分组：POST /api/v1/admin/groups
func (h *GroupHandler) Create(c *gin.Context) {
	var in service.GroupInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	group, err := h.svc.CreateGroup(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditGroupUpsert, "key_group", strconv.FormatUint(uint64(group.ID), 10), group.Name)
	response.Created(c, group)
}

// Update 更新分组策略：PUT /api/v1/admin/groups/:id
func (h *GroupHandler) Update(c *gin.Context) {
	id, ok := groupID(c)
	if !ok {
		return
	}
	var in service.GroupInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	group, err := h.svc.UpdateGroup(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditGroupUpsert, "key_group", strconv.FormatUint(uint64(group.ID), 10), group.Name)
	response.OK(c, group)
}

// Delete 删除分组：DELETE /api/v1/admin/groups/:id
func (h *GroupHandler) Delete(c *gin.Context) {
	id, ok := groupID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteGroup(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditGroupDelete, "key_group", strconv.FormatUint(uint64(id), 10), "")
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// groupID 解析路径中的分组 ID。
func groupID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, apierr.New(apierr.ErrInvalidParam, "分组 ID 非法"))
		return 0, false
	}
	return uint(id), true
}
