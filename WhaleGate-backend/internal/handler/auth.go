package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// AuthHandler 注册、登录、当前用户等接口。
type AuthHandler struct {
	svc *service.Container
}

// NewAuthHandler 创建处理器。
func NewAuthHandler(svc *service.Container) *AuthHandler {
	return &AuthHandler{svc: svc}
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// Register godoc
// @Summary 用户注册
// @Tags 认证
// @Accept json
// @Produce json
// @Success 201 {object} model.User
// @Router /api/v1/auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var in service.RegisterInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	if !h.svc.Config.Security.AllowRegistration && !IsAdmin(c) {
		auditFail(h.svc, c, model.AuditRegister, "user", in.Username, "注册已关闭")
		response.Fail(c, service.ErrRegistrationDisabled)
		return
	}

	user, err := h.svc.Register(c.Request.Context(), in, IsAdmin(c))
	if err != nil {
		auditFail(h.svc, c, model.AuditRegister, "user", in.Username, err.Error())
		response.Fail(c, err)
		return
	}
	entry := auditEntry(c, model.AuditRegister, model.AuditStatusSuccess, "user",
		strconv.FormatUint(uint64(user.ID), 10), user.Username)
	entry.UserID = user.ID
	entry.Username = user.Username
	h.svc.Audit(entry)
	response.Created(c, user)
}

// Login 登录并签发令牌。
func (h *AuthHandler) Login(c *gin.Context) {
	var in service.LoginInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	result, err := h.svc.Login(c.Request.Context(), in)
	if err != nil {
		auditFail(h.svc, c, model.AuditLogin, "user", in.Account, err.Error())
		response.Fail(c, err)
		return
	}
	// 登录接口是公开路由，上下文里没有用户信息，需用登录结果补全
	entry := auditEntry(c, model.AuditLogin, model.AuditStatusSuccess, "user",
		strconv.FormatUint(uint64(result.User.ID), 10), result.User.Username)
	entry.UserID = result.User.ID
	entry.Username = result.User.Username
	h.svc.Audit(entry)
	response.OK(c, result)
}

// Me 返回当前登录用户。
func (h *AuthHandler) Me(c *gin.Context) {
	user, err := h.svc.GetUserByID(c.Request.Context(), CurrentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, user)
}

// Logout 登出。令牌无状态，服务端仅回执，客户端需自行丢弃令牌。
func (h *AuthHandler) Logout(c *gin.Context) {
	auditOK(h.svc, c, model.AuditLogout, "user", strconv.FormatUint(uint64(CurrentUserID(c)), 10), "")
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// ChangePassword 修改当前账号密码（首次登录后强制改密时调用）。
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	token, err := h.svc.ChangePassword(c.Request.Context(), CurrentUserID(c), req.OldPassword, req.NewPassword)
	if err != nil {
		auditFail(h.svc, c, model.AuditChangePassword, "user", strconv.FormatUint(uint64(CurrentUserID(c)), 10), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditChangePassword, "user", strconv.FormatUint(uint64(CurrentUserID(c)), 10), "")
	response.OK(c, gin.H{"changed": true, "token": token})
}
