package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// AccountHandler 用户中心：第三方绑定与通行密钥管理。
type AccountHandler struct {
	svc *service.Container
}

// NewAccountHandler 创建处理器。
func NewAccountHandler(svc *service.Container) *AccountHandler {
	return &AccountHandler{svc: svc}
}

// Bindings 返回当前用户的第三方绑定与通行密钥。
func (h *AccountHandler) Bindings(c *gin.Context) {
	userID := CurrentUserID(c)
	identities, err := h.svc.ListIdentities(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if identities == nil {
		identities = []model.UserIdentity{}
	}

	passkeys, err := h.svc.ListPasskeys(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if passkeys == nil {
		passkeys = []model.WebAuthnCredential{}
	}

	response.OK(c, gin.H{
		"identities":      identities,
		"passkeys":        passkeys,
		"github_enabled":  h.svc.GitHubEnabled(),
		"passkey_enabled": h.svc.WebAuthn != nil,
	})
}

// UnbindIdentity 解除第三方绑定。
func (h *AccountHandler) UnbindIdentity(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	if err := h.svc.UnbindIdentity(c.Request.Context(), CurrentUserID(c), uint(id)); err != nil {
		auditFail(h.svc, c, model.AuditIdentityUnbind, "identity", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditIdentityUnbind, "identity", c.Param("id"), "")
	response.OK(c, gin.H{"id": id, "unbound": true})
}

// DeletePasskey 删除通行密钥。
func (h *AccountHandler) DeletePasskey(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	if err := h.svc.DeletePasskey(c.Request.Context(), CurrentUserID(c), uint(id)); err != nil {
		auditFail(h.svc, c, model.AuditPasskeyDelete, "passkey", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditPasskeyDelete, "passkey", c.Param("id"), "")
	response.OK(c, gin.H{"id": id, "deleted": true})
}

type passkeyBeginRequest struct {
	Name string `json:"name"`
}

// PasskeyRegisterBegin 开始注册通行密钥。
func (h *AccountHandler) PasskeyRegisterBegin(c *gin.Context) {
	var req passkeyBeginRequest
	_ = c.ShouldBindJSON(&req)

	user, err := h.svc.GetUserByID(c.Request.Context(), CurrentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	sessionID, options, err := h.svc.PasskeyRegistrationBegin(c.Request.Context(), user)
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 直接下发 publicKey 部分，前端可原样交给 navigator.credentials.create
	response.OK(c, gin.H{"session_id": sessionID, "options": options.Response, "name": req.Name})
}

type passkeyFinishRequest struct {
	SessionID  string          `json:"session_id" binding:"required"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential" binding:"required"`
}

// PasskeyRegisterFinish 完成通行密钥注册。
func (h *AccountHandler) PasskeyRegisterFinish(c *gin.Context) {
	var req passkeyFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	raw, err := json.Marshal(req.Credential)
	if err != nil {
		response.Fail(c, apierr.Wrap(apierr.ErrInvalidParam, err))
		return
	}

	user, err := h.svc.GetUserByID(c.Request.Context(), CurrentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	cred, err := h.svc.PasskeyRegistrationFinish(c.Request.Context(), user, req.SessionID, req.Name, raw)
	if err != nil {
		auditFail(h.svc, c, model.AuditPasskeyRegister, "passkey", "", err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditPasskeyRegister, "passkey", strconv.FormatUint(uint64(cred.ID), 10), cred.Name)
	response.Created(c, cred)
}

// GitHubAuthorize 返回 GitHub 授权地址（mode: bind 或 login）。
func (h *AccountHandler) GitHubAuthorize(c *gin.Context) {
	mode := c.DefaultQuery("mode", "login")
	if mode != "bind" && mode != "login" {
		response.Fail(c, apierr.New(apierr.ErrInvalidParam, "mode 只能为 bind 或 login"))
		return
	}
	url, err := h.svc.GitHubAuthorizeURL(c.Request.Context(), mode, CurrentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"authorize_url": url})
}

// GitHubCallback 处理 GitHub 回调：完成绑定或登录，并重定向回前端。
func (h *AccountHandler) GitHubCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	base := strings.TrimRight(h.svc.Config.Auth.FrontendBase, "/")
	if base == "" {
		base = "/"
	}

	// 缺少参数或上游报错时，带上错误原因回前端
	if code == "" {
		redirectTo(c, base+"/account?github=error&reason="+urlEscape(c.Query("error")))
		return
	}

	// 登录态由 state 决定：bind 模式需要已登录用户
	if errStr := c.Query("error"); errStr != "" {
		redirectTo(c, base+"/account?github=error&reason="+urlEscape(errStr))
		return
	}

	// 先尝试绑定（state 携带模式）
	token, err := h.svc.LoginWithGitHub(c.Request.Context(), code, state)
	if err != nil {
		// 未绑定则视为绑定流程失败
		redirectTo(c, base+"/account?github=error&reason="+urlEscape(err.Error()))
		return
	}
	redirectTo(c, base+"/login?ticket="+urlEscape(token))
}

// GitHubCallbackJSON 供前端直接用 code+state 完成绑定（已登录）。
func (h *AccountHandler) GitHubCallbackJSON(c *gin.Context) {
	var req struct {
		Code  string `json:"code" binding:"required"`
		State string `json:"state" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	identity, err := h.svc.BindGitHub(c.Request.Context(), CurrentUserID(c), req.Code, req.State)
	if err != nil {
		auditFail(h.svc, c, model.AuditIdentityBind, "identity", "github", err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditIdentityBind, "identity", "github", identity.DisplayName)
	response.OK(c, identity)
}

type ticketRequest struct {
	Ticket string `json:"ticket" binding:"required"`
}

// ExchangeTicket 用一次性票据换取登录令牌。
func (h *AccountHandler) ExchangeTicket(c *gin.Context) {
	var req ticketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	token, err := h.svc.ExchangeTicket(c.Request.Context(), req.Ticket)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"token": token})
}

// PasskeyLoginBegin 开始通行密钥登录（无需用户名）。
func (h *AccountHandler) PasskeyLoginBegin(c *gin.Context) {
	sessionID, options, err := h.svc.PasskeyLoginBegin(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 直接下发 publicKey 部分，前端可原样交给 navigator.credentials.get
	response.OK(c, gin.H{"session_id": sessionID, "options": options.Response})
}

type passkeyLoginFinishRequest struct {
	SessionID  string          `json:"session_id" binding:"required"`
	Credential json.RawMessage `json:"credential" binding:"required"`
}

// PasskeyLoginFinish 完成通行密钥登录。
func (h *AccountHandler) PasskeyLoginFinish(c *gin.Context) {
	var req passkeyLoginFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	raw, err := json.Marshal(req.Credential)
	if err != nil {
		response.Fail(c, apierr.Wrap(apierr.ErrInvalidParam, err))
		return
	}
	result, err := h.svc.PasskeyLoginFinish(c.Request.Context(), req.SessionID, raw)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, result)
}

func redirectTo(c *gin.Context, location string) {
	c.Redirect(http.StatusFound, location)
}

func urlEscape(s string) string {
	return url.QueryEscape(s)
}
