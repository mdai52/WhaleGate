package handler

import (
	"io"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// StudioHandler 绘图工作台：提示词词库、在线生成与作品库。
type StudioHandler struct {
	svc *service.Container
}

// NewStudioHandler 创建处理器。
func NewStudioHandler(svc *service.Container) *StudioHandler {
	return &StudioHandler{svc: svc}
}

// Models 查询可用的生图模型：GET /api/v1/studio/models
func (h *StudioHandler) Models(c *gin.Context) {
	models, err := h.svc.ListImageModels(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, models)
}

// Prompts 查询提示词词库：GET /api/v1/studio/prompts?category=&keyword=
func (h *StudioHandler) Prompts(c *gin.Context) {
	library, err := h.svc.ListPromptLibrary(c.Request.Context(), c.Query("category"), c.Query("keyword"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, library)
}

// Generate 在线生成图片并落库为作品：POST /api/v1/studio/images
func (h *StudioHandler) Generate(c *gin.Context) {
	var in service.ImageWorkInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	work, err := h.svc.CreateImageWork(c.Request.Context(), CurrentUserID(c), in, newImageCallMeta(h.svc, c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, work)
}

// EnhancePrompt 用对话模型把简短想法扩写成生图提示词：POST /api/v1/studio/enhance-prompt
func (h *StudioHandler) EnhancePrompt(c *gin.Context) {
	var in service.PromptEnhanceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	text, err := h.svc.EnhancePrompt(c.Request.Context(), CurrentUserID(c), in, newImageCallMeta(h.svc, c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"prompt": text})
}

// Works 分页查询作品库：GET /api/v1/studio/works
func (h *StudioHandler) Works(c *gin.Context) {
	p := ParsePage(c)
	items, total, err := h.svc.ListImageWorks(c.Request.Context(), CurrentUserID(c), p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, items, total, p.Page, p.PageSize)
}

// DeleteWork 删除作品：DELETE /api/v1/studio/works/:id
func (h *StudioHandler) DeleteWork(c *gin.Context) {
	workID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.FailCode(c, apierr.ErrInvalidParam, "作品 ID 非法")
		return
	}
	if err := h.svc.DeleteImageWork(c.Request.Context(), CurrentUserID(c), uint(workID), IsAdmin(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": workID, "deleted": true})
}

// WorkImage 读取作品图片：GET /api/v1/studio/works/:id/images/:index
// 图片以文件形式托管在服务端，需带登录态并按归属校验后回读。
func (h *StudioHandler) WorkImage(c *gin.Context) {
	workID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.FailCode(c, apierr.ErrInvalidParam, "作品 ID 非法")
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		response.FailCode(c, apierr.ErrInvalidParam, "图片序号非法")
		return
	}

	image, err := h.svc.ImageWorkFile(c.Request.Context(), CurrentUserID(c), uint(workID), index, IsAdmin(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer image.Reader.Close()

	// 作品图片不可变，允许浏览器私有缓存，避免作品库翻页时重复拉取。
	c.Header("Cache-Control", "private, max-age=86400")
	c.Header("Content-Type", image.ContentType)
	c.Header("Content-Disposition", `inline; filename="`+image.FileName+`"`)
	if _, err := io.Copy(c.Writer, image.Reader); err != nil && h.svc.Logger != nil {
		h.svc.Logger.Warn("回写作品图片失败",
			zap.Uint64("work_id", workID), zap.Int("index", index), zap.Error(err))
	}
}
