package handler

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// CredentialHandler 认证文件（凭证）管理接口。
type CredentialHandler struct {
	svc *service.Container
}

// NewCredentialHandler 创建处理器。
func NewCredentialHandler(svc *service.Container) *CredentialHandler {
	return &CredentialHandler{svc: svc}
}

// List 分页列出凭证，支持关键字 / 状态 / 供应商筛选。
func (h *CredentialHandler) List(c *gin.Context) {
	p := ParsePage(c)
	filter := service.ListCredentialsFilter{
		Keyword:  c.Query("keyword"),
		Provider: c.Query("provider"),
	}
	if v, err := strconv.Atoi(c.Query("status")); err == nil {
		filter.Status = v
	}

	list, total, err := h.svc.ListCredentials(c.Request.Context(), filter, p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, p.Page, p.PageSize)
}

// Rename 重命名凭证。
func (h *CredentialHandler) Rename(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	cred, err := h.svc.RenameCredential(c.Request.Context(), uint(id), req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditCredentialImport, "credential", c.Param("id"), "重命名为 "+cred.Name)
	response.OK(c, cred)
}

// Export 下载认证文件（按供应商输出标准格式 JSON）。
func (h *CredentialHandler) Export(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	fileName, payload, err := h.svc.ExportCredential(c.Request.Context(), uint(id))
	if err != nil {
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditCredentialImport, "credential", c.Param("id"), "导出认证文件")
	c.Header("Content-Disposition", `attachment; filename="`+fileName+`"`)
	c.Data(http.StatusOK, "application/json", payload)
}

// maxCredentialFile 上传认证文件的大小上限（1MB）。
const maxCredentialFile = 1 << 20

// Import 上传认证文件导入凭证。
func (h *CredentialHandler) Import(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, apierr.New(apierr.ErrInvalidParam, "请通过 file 字段上传认证文件"))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Fail(c, apierr.Wrap(apierr.ErrInvalidParam, err))
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxCredentialFile))
	if err != nil {
		response.Fail(c, apierr.Wrap(apierr.ErrInvalidParam, err))
		return
	}

	cred, err := h.svc.ImportCredentialFile(
		c.Request.Context(), fileHeader.Filename, content, c.PostForm("name"))
	if err != nil {
		auditFail(h.svc, c, model.AuditCredentialImport, "credential", fileHeader.Filename, err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditCredentialImport, "credential", strconv.FormatUint(uint64(cred.ID), 10), cred.Provider)
	response.Created(c, cred)
}

type credentialStatusRequest struct {
	Status int `json:"status" binding:"required"`
}

// SetStatus 启用/停用凭证。
func (h *CredentialHandler) SetStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	var req credentialStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	if err := h.svc.SetCredentialStatus(c.Request.Context(), uint(id), req.Status); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": id, "status": req.Status})
}

// Refresh 手动续期凭证。
func (h *CredentialHandler) Refresh(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	cred, err := h.svc.RefreshCredential(c.Request.Context(), uint(id))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cred)
}

// Delete 删除凭证。
func (h *CredentialHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	if err := h.svc.DeleteCredential(c.Request.Context(), uint(id)); err != nil {
		auditFail(h.svc, c, model.AuditCredentialDelete, "credential", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditCredentialDelete, "credential", c.Param("id"), "")
	response.OK(c, gin.H{"id": id, "deleted": true})
}
