package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// MCPHandler MCP 服务管理（管理端）。
type MCPHandler struct {
	svc *service.Container
}

// NewMCPHandler 构造处理器。
func NewMCPHandler(svc *service.Container) *MCPHandler {
	return &MCPHandler{svc: svc}
}

// List 列出全部 MCP 服务。
func (h *MCPHandler) List(c *gin.Context) {
	list, err := h.svc.ListMCPServers(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Create 新增 MCP 服务。
func (h *MCPHandler) Create(c *gin.Context) {
	var in service.MCPServerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, err)
		return
	}
	s, err := h.svc.CreateMCPServer(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditSettingsUpdate, "mcp", strconv.FormatUint(uint64(s.ID), 10), "创建 "+s.Name)
	response.Created(c, s)
}

// Update 更新 MCP 服务。
func (h *MCPHandler) Update(c *gin.Context) {
	rawID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || rawID == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	id := uint(rawID)
	var in service.MCPServerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, err)
		return
	}
	s, err := h.svc.UpdateMCPServer(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditSettingsUpdate, "mcp", c.Param("id"), "更新配置")
	response.OK(c, s)
}

// Delete 删除 MCP 服务。
func (h *MCPHandler) Delete(c *gin.Context) {
	rawID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || rawID == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	id := uint(rawID)
	if err := h.svc.DeleteMCPServer(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditSettingsUpdate, "mcp", c.Param("id"), "删除")
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// Connect 连接并拉取工具列表。
func (h *MCPHandler) Connect(c *gin.Context) {
	rawID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || rawID == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	id := uint(rawID)
	s, err := h.svc.ConnectMCP(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditSettingsUpdate, "mcp", c.Param("id"), "连接成功，工具 "+strconv.Itoa(s.ToolCount))
	response.OK(c, s)
}

// Tools 列出对外暴露的工具（含路由信息）。
func (h *MCPHandler) Tools(c *gin.Context) {
	tools, err := h.svc.ListMCPTools(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tools)
}

// Call 调试用：直接调用一个工具。
func (h *MCPHandler) Call(c *gin.Context) {
	var req struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, err)
		return
	}
	text, _, err := h.svc.CallMCPTool(c.Request.Context(), req.Name, req.Arguments)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"result": text})
}
