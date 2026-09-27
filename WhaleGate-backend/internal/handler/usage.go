package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// UsageHandler 用户门户的用量与调用历史接口。
type UsageHandler struct {
	svc *service.Container
}

// NewUsageHandler 创建处理器。
func NewUsageHandler(svc *service.Container) *UsageHandler {
	return &UsageHandler{svc: svc}
}

// MyLogs 当前用户的调用历史。管理员可传 user_id 查看他人。
func (h *UsageHandler) MyLogs(c *gin.Context) {
	userID := CurrentUserID(c)
	if IsAdmin(c) {
		if v, err := strconv.ParseUint(c.Query("user_id"), 10, 64); err == nil && v > 0 {
			userID = uint(v)
		}
	}

	filter := service.CallLogFilter{
		UserID:   userID,
		Model:    c.Query("model"),
		Start:    parseTime(c, "start"),
		End:      parseTime(c, "end"),
		PageSize: 0,
	}
	if v, err := strconv.Atoi(c.Query("status")); err == nil {
		filter.Status = v
	}
	p := ParsePage(c)
	filter.Page, filter.PageSize = p.Page, p.PageSize

	list, total, err := h.svc.ListCallLogs(c.Request.Context(), filter)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, p.Page, p.PageSize)
}

// Summary 按天聚合的用量，供门户图表使用。
func (h *UsageHandler) Summary(c *gin.Context) {
	days, err := strconv.Atoi(c.DefaultQuery("days", "7"))
	if err != nil || days <= 0 {
		days = 7
	}

	userID := CurrentUserID(c)
	if IsAdmin(c) {
		if v, err := strconv.ParseUint(c.Query("user_id"), 10, 64); err == nil && v > 0 {
			userID = uint(v)
		}
	}

	rows, err := h.svc.UsageSummary(c.Request.Context(), userID, days)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, rows)
}

// parseTime 解析 RFC3339 时间参数，非法或缺失返回 nil。
func parseTime(c *gin.Context, key string) *time.Time {
	raw := c.Query(key)
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &parsed
}
