package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/oauth"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// OAuthHandler OAuth 授权流程接口（管理端）。
type OAuthHandler struct {
	svc *service.Container
}

// NewOAuthHandler 创建处理器。
func NewOAuthHandler(svc *service.Container) *OAuthHandler {
	return &OAuthHandler{svc: svc}
}

// Providers 列出可用的 OAuth 供应商。
func (h *OAuthHandler) Providers(c *gin.Context) {
	response.OK(c, oauth.List())
}

type startRequest struct {
	// RedirectURI 可选回调地址；为空时使用"粘贴授权码"模式。
	RedirectURI string `json:"redirect_uri"`
}

// Start 发起 OAuth 授权。
func (h *OAuthHandler) Start(c *gin.Context) {
	provider, ok := oauth.Get(c.Param("provider"))
	if !ok {
		response.Fail(c, apierr.New(apierr.ErrNotFound, "未知的 OAuth 供应商"))
		return
	}
	var req startRequest
	_ = c.ShouldBindJSON(&req)

	result, err := h.svc.OAuth.StartAuthorization(provider, req.RedirectURI)
	if err != nil {
		response.Fail(c, apierr.Wrap(apierr.ErrInvalidParam, err))
		return
	}

	response.OK(c, gin.H{
		"provider":         provider.ID,
		"name":             provider.Name,
		"flow":             string(provider.Flow),
		"state":            result.State,
		"authorize_url":    result.AuthorizeURL,
		"verification_url": result.VerificationURL,
		"user_code":        result.UserCode,
		"expires_in":       result.ExpiresIn,
	})
}

type completeRequest struct {
	State string `json:"state" binding:"required"`
	Code  string `json:"code"`
	Name  string `json:"name"`
}

// Complete 授权码流程：用粘贴的授权码换取令牌并保存凭证。
func (h *OAuthHandler) Complete(c *gin.Context) {
	providerID := c.Param("provider")
	var req completeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}

	set, err := h.svc.OAuth.CompleteAuthorization(c.Request.Context(), req.State, req.Code)
	if err != nil {
		response.Fail(c, convertOAuthError(err))
		return
	}
	cred, err := h.svc.CreateCredentialFromOAuth(c.Request.Context(), providerID, set, req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cred)
}

type pollRequest struct {
	State string `json:"state" binding:"required"`
	Name  string `json:"name"`
}

// Poll 设备码流程：轮询授权结果。
func (h *OAuthHandler) Poll(c *gin.Context) {
	providerID := c.Param("provider")
	var req pollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}

	set, err := h.svc.OAuth.PollDevice(c.Request.Context(), req.State)
	if err != nil {
		if errors.Is(err, oauth.ErrPending) {
			c.JSON(202, gin.H{"code": 0, "message": "等待用户授权", "data": gin.H{"pending": true}})
			return
		}
		response.Fail(c, convertOAuthError(err))
		return
	}

	cred, err := h.svc.CreateCredentialFromOAuth(c.Request.Context(), providerID, set, req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cred)
}

func convertOAuthError(err error) error {
	switch {
	case errors.Is(err, oauth.ErrPending):
		return apierr.New(apierr.ErrOK, "等待用户授权")
	case errors.Is(err, oauth.ErrUnknownState):
		return apierr.New(apierr.ErrInvalidParam, "授权会话已过期，请重新发起")
	case errors.Is(err, oauth.ErrAccessDenied):
		return apierr.New(apierr.ErrForbidden, "用户取消了授权")
	default:
		return apierr.Wrap(apierr.ErrUpstream, err)
	}
}
