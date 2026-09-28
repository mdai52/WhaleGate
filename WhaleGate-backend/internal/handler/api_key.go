package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// APIKeyHandler 用户侧密钥管理接口。
type APIKeyHandler struct {
	svc *service.Container
}

// NewAPIKeyHandler 创建处理器。
func NewAPIKeyHandler(svc *service.Container) *APIKeyHandler {
	return &APIKeyHandler{svc: svc}
}

// keyID 解析路径中的密钥 ID。
func keyID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return 0, false
	}
	return uint(id), true
}

// Create 创建密钥，返回一次性明文。
func (h *APIKeyHandler) Create(c *gin.Context) {
	var in service.CreateKeyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}

	userID := CurrentUserID(c)
	if IsAdmin(c) && c.Query("user_id") != "" {
		if v, err := strconv.ParseUint(c.Query("user_id"), 10, 64); err == nil && v > 0 {
			userID = uint(v)
		}
	}

	key, plain, err := h.svc.CreateAPIKey(c.Request.Context(), userID, in)
	if err != nil {
		auditFail(h.svc, c, model.AuditKeyCreate, "api_key", "", err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditKeyCreate, "api_key", strconv.FormatUint(uint64(key.ID), 10), key.Name)

	type result struct {
		*model.APIKey
		Key string `json:"key"`
	}
	response.Created(c, result{APIKey: key, Key: plain})
}

// List 分页列出密钥。
func (h *APIKeyHandler) List(c *gin.Context) {
	p := ParsePage(c)
	userID := CurrentUserID(c)
	if IsAdmin(c) && c.Query("user_id") != "" {
		if v, err := strconv.ParseUint(c.Query("user_id"), 10, 64); err == nil && v > 0 {
			userID = uint(v)
		}
	} else if IsAdmin(c) && c.Query("all") == "1" {
		userID = 0
	}

	keys, total, err := h.svc.ListAPIKeys(c.Request.Context(), userID, p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, keys, total, p.Page, p.PageSize)
}

// Revoke 吊销密钥。
func (h *APIKeyHandler) Revoke(c *gin.Context) {
	id, ok := keyID(c)
	if !ok {
		return
	}
	userID := CurrentUserID(c)
	if IsAdmin(c) {
		userID = 0
	}
	if err := h.svc.RevokeAPIKey(c.Request.Context(), userID, id); err != nil {
		auditFail(h.svc, c, model.AuditKeyRevoke, "api_key", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditKeyRevoke, "api_key", c.Param("id"), "")
	response.OK(c, gin.H{"id": id, "revoked": true})
}

// Delete 删除密钥。
func (h *APIKeyHandler) Delete(c *gin.Context) {
	id, ok := keyID(c)
	if !ok {
		return
	}
	userID := CurrentUserID(c)
	if IsAdmin(c) {
		userID = 0
	}
	if err := h.svc.DeleteAPIKey(c.Request.Context(), userID, id); err != nil {
		auditFail(h.svc, c, model.AuditKeyDelete, "api_key", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditKeyDelete, "api_key", c.Param("id"), "")
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// Update 更新密钥配置（名称、速率、额度、IP/模型白名单、有效期、状态等）。
func (h *APIKeyHandler) Update(c *gin.Context) {
	id, ok := keyID(c)
	if !ok {
		return
	}
	var in service.UpdateKeyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	userID := CurrentUserID(c)
	if IsAdmin(c) {
		userID = 0
	}
	key, err := h.svc.UpdateAPIKey(c.Request.Context(), userID, id, in)
	if err != nil {
		auditFail(h.svc, c, model.AuditKeyUpdate, "api_key", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditKeyUpdate, "api_key", c.Param("id"), key.Name)
	response.OK(c, key)
}
