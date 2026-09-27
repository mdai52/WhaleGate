package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// ChannelHandler 渠道管理接口（管理端）。
type ChannelHandler struct {
	svc *service.Container
}

// NewChannelHandler 创建处理器。
func NewChannelHandler(svc *service.Container) *ChannelHandler {
	return &ChannelHandler{svc: svc}
}

// channelView 对外展示的渠道信息，永不返回上游密钥。
type channelView struct {
	*model.Channel
	APIKeyMasked string `json:"api_key_masked"`
}

func (h *ChannelHandler) view(ch *model.Channel) channelView {
	masked := ""
	plain, err := h.svc.DecryptChannelKey(ch)
	if err == nil && plain != "" {
		masked = maskSecret(plain)
	}
	return channelView{Channel: ch, APIKeyMasked: masked}
}

// Detect 自动探测上游：只需地址与密钥，返回协议类型、模型列表与能力。
func (h *ChannelHandler) Detect(c *gin.Context) {
	var in service.DetectInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.DetectChannel(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Catalog 查询全局模型目录（供渠道/倍率配置下拉选择）。
func (h *ChannelHandler) Catalog(c *gin.Context) {
	p := ParsePage(c)
	filter := service.CatalogFilter{
		Keyword:      c.Query("keyword"),
		ProviderType: c.Query("provider_type"),
		Capability:   c.Query("capability"),
	}
	list, total, err := h.svc.ListModelCatalog(c.Request.Context(), filter, p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, p.Page, p.PageSize)
}

// Create 新建渠道。
func (h *ChannelHandler) Create(c *gin.Context) {
	var in service.ChannelInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	ch, err := h.svc.CreateChannel(c.Request.Context(), in)
	if err != nil {
		auditFail(h.svc, c, model.AuditChannelCreate, "channel", "", err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditChannelCreate, "channel", strconv.FormatUint(uint64(ch.ID), 10), ch.Name)
	response.Created(c, h.view(ch))
}

// List 分页列出渠道。
func (h *ChannelHandler) List(c *gin.Context) {
	p := ParsePage(c)
	list, total, err := h.svc.ListChannels(c.Request.Context(), p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	views := make([]channelView, 0, len(list))
	for i := range list {
		views = append(views, h.view(&list[i]))
	}
	response.Page(c, views, total, p.Page, p.PageSize)
}

// Get 查询渠道详情。
func (h *ChannelHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	ch, err := h.svc.GetChannel(c.Request.Context(), uint(id))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, h.view(ch))
}

// Update 更新渠道。
func (h *ChannelHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	var in service.ChannelInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	ch, err := h.svc.UpdateChannel(c.Request.Context(), uint(id), in)
	if err != nil {
		auditFail(h.svc, c, model.AuditChannelUpdate, "channel", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditChannelUpdate, "channel", c.Param("id"), ch.Name)
	response.OK(c, h.view(ch))
}

// Delete 删除渠道。
func (h *ChannelHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	if err := h.svc.DeleteChannel(c.Request.Context(), uint(id)); err != nil {
		auditFail(h.svc, c, model.AuditChannelDelete, "channel", c.Param("id"), err.Error())
		response.Fail(c, err)
		return
	}
	auditOK(h.svc, c, model.AuditChannelDelete, "channel", c.Param("id"), "")
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// Test 连通性测试：用渠道密钥向上游发起一次最小请求。
func (h *ChannelHandler) Test(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errInvalidID)
		return
	}
	ch, err := h.svc.GetChannel(c.Request.Context(), uint(id))
	if err != nil {
		response.Fail(c, err)
		return
	}
	result, err := h.svc.TestChannel(c.Request.Context(), ch)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, result)
}

// maskSecret 脱敏上游密钥，仅保留末 4 位。
func maskSecret(secret string) string {
	const visible = 4
	if len(secret) <= visible {
		return "****"
	}
	return "****" + secret[len(secret)-visible:]
}
