// Package handler 负责 HTTP 请求解析、参数校验与响应组装。
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/service"
)

// PageParams 分页参数。
type PageParams struct {
	Page     int
	PageSize int
}

// ParsePage 解析并规范分页参数。
func ParsePage(c *gin.Context) PageParams {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(constant.DefaultPageSize)))
	if page < 1 {
		page = constant.DefaultPage
	}
	if pageSize < 1 {
		pageSize = constant.DefaultPageSize
	}
	if pageSize > constant.MaxPageSize {
		pageSize = constant.MaxPageSize
	}
	return PageParams{Page: page, PageSize: pageSize}
}

// CurrentUserID 返回当前登录用户 ID，未登录返回 0。
func CurrentUserID(c *gin.Context) uint {
	v, ok := c.Get(constant.CtxUserID)
	if !ok {
		return 0
	}
	switch id := v.(type) {
	case uint:
		return id
	case float64:
		return uint(id)
	case int:
		return uint(id)
	default:
		return 0
	}
}

// CurrentRole 返回当前用户角色。
func CurrentRole(c *gin.Context) string {
	v, _ := c.Get(constant.CtxUserRole)
	role, _ := v.(string)
	return role
}

// IsAdmin 当前请求是否为管理员。
func IsAdmin(c *gin.Context) bool { return CurrentRole(c) == constant.RoleAdmin }

// groupMultiplier 取当前密钥所属分组的倍率系数；未分组或分组不可用时为 1。
//
// 按请求解析而不是折进 KeyIdentity：密钥缓存最长 24 小时，
// 折进去会让管理员调整分组倍率后长时间不生效。
// 转发（chat）与生图两条链路共用该口径，避免生图绕开分组加价。
func groupMultiplier(svc *service.Container, c *gin.Context) float64 {
	idn := currentKeyIdentity(c)
	if idn == nil {
		return 1
	}
	return svc.GroupPolicyFor(c.Request.Context(), idn.GroupTag).RatioMultiplier
}

// CurrentUsername 返回当前登录用户名。
func CurrentUsername(c *gin.Context) string {
	v, _ := c.Get(constant.CtxUsername)
	name, _ := v.(string)
	return name
}

// auditEntry 从 gin 上下文构造审计记录（IP / UA / 追踪 ID 一并留痕）。
func auditEntry(c *gin.Context, action string, status int, targetType, targetID, detail string) service.AuditEntry {
	return service.AuditEntry{
		UserID:     CurrentUserID(c),
		Username:   CurrentUsername(c),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Status:     status,
		Detail:     detail,
		IP:         c.ClientIP(),
		UserAgent:  truncateUA(c.Request.UserAgent()),
		TraceID:    c.GetString(constant.CtxTraceID),
	}
}

// auditOK / auditFail 便捷记录成功与失败。
func auditOK(svc *service.Container, c *gin.Context, action, targetType, targetID, detail string) {
	svc.Audit(auditEntry(c, action, model.AuditStatusSuccess, targetType, targetID, detail))
}

func auditFail(svc *service.Container, c *gin.Context, action, targetType, targetID, detail string) {
	svc.Audit(auditEntry(c, action, model.AuditStatusFailed, targetType, targetID, detail))
}

func truncateUA(ua string) string {
	if len(ua) > 500 {
		return ua[:500]
	}
	return ua
}
