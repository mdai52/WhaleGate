package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// VideoStudioHandler 视频工作台：脚本创作、视频生成与作品库。
type VideoStudioHandler struct {
	svc *service.Container
}

// NewVideoStudioHandler 创建处理器。
func NewVideoStudioHandler(svc *service.Container) *VideoStudioHandler {
	return &VideoStudioHandler{svc: svc}
}

// Models 可用的视频模型与脚本模型：GET /api/v1/studio/video/models
func (h *VideoStudioHandler) Models(c *gin.Context) {
	models, err := h.svc.ListVideoModels(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, models)
}

// PromptLibrary 视频脚本词库：GET /api/v1/studio/video/prompts?category=&keyword=
func (h *VideoStudioHandler) PromptLibrary(c *gin.Context) {
	library, err := h.svc.ListVideoPromptLibrary(c.Request.Context(), c.Query("category"), c.Query("keyword"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, library)
}

// ---------------------------------------------------------------- 脚本

// CreateScript 创作并保存分镜脚本：POST /api/v1/studio/video/scripts
func (h *VideoStudioHandler) CreateScript(c *gin.Context) {
	var in service.VideoScriptInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	script, err := h.svc.CreateVideoScript(c.Request.Context(), CurrentUserID(c), in, newImageCallMeta(h.svc, c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, script)
}

// Scripts 分页查询脚本：GET /api/v1/studio/video/scripts
func (h *VideoStudioHandler) Scripts(c *gin.Context) {
	p := ParsePage(c)
	items, total, err := h.svc.ListVideoScripts(c.Request.Context(), CurrentUserID(c), p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, items, total, p.Page, p.PageSize)
}

// DeleteScript 删除脚本：DELETE /api/v1/studio/video/scripts/:id
func (h *VideoStudioHandler) DeleteScript(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.FailCode(c, apierr.ErrInvalidParam, "脚本 ID 非法")
		return
	}
	if err := h.svc.DeleteVideoScript(c.Request.Context(), CurrentUserID(c), uint(id), IsAdmin(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// ---------------------------------------------------------------- 作品

// Generate 提交一次视频生成：POST /api/v1/studio/video/works
//
// 视频是异步任务，这里只返回「已排队」，终态由后台推进，
// 前端轮询作品列表即可看到进度。
func (h *VideoStudioHandler) Generate(c *gin.Context) {
	var in service.VideoWorkInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	submitted, err := h.svc.SubmitVideoWork(c.Request.Context(), CurrentUserID(c), in, newImageCallMeta(h.svc, c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, submitted)
}

// Works 分页查询作品：GET /api/v1/studio/video/works
func (h *VideoStudioHandler) Works(c *gin.Context) {
	p := ParsePage(c)
	items, total, err := h.svc.ListVideoWorks(c.Request.Context(), CurrentUserID(c), p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, items, total, p.Page, p.PageSize)
}

// Work 查询单个作品（含任务状态与进度）：GET /api/v1/studio/video/works/:id
func (h *VideoStudioHandler) Work(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.FailCode(c, apierr.ErrInvalidParam, "作品 ID 非法")
		return
	}
	work, err := h.svc.GetVideoWork(c.Request.Context(), CurrentUserID(c), uint(id), IsAdmin(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, work)
}

// DeleteWork 删除作品：DELETE /api/v1/studio/video/works/:id
func (h *VideoStudioHandler) DeleteWork(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.FailCode(c, apierr.ErrInvalidParam, "作品 ID 非法")
		return
	}
	if err := h.svc.DeleteVideoWork(c.Request.Context(), CurrentUserID(c), uint(id), IsAdmin(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// WorkVideo 读取作品视频：GET /api/v1/studio/video/works/:id/videos/:index
//
// 视频以文件形式托管在服务端，需带登录态并按归属校验后回读；
// 配置了公开访问前缀时前端直连存储，不占用网关带宽。
func (h *VideoStudioHandler) WorkVideo(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.FailCode(c, apierr.ErrInvalidParam, "作品 ID 非法")
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		response.FailCode(c, apierr.ErrInvalidParam, "视频序号非法")
		return
	}

	video, err := h.svc.OpenVideoWorkFile(c.Request.Context(), CurrentUserID(c), uint(id), index, IsAdmin(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 支持 Range：播放器拖动进度条时只回传请求的字节区间。
	if err := writeVideo(c, video.Store, video.Key, video.ContentType, video.FileName); err != nil &&
		h.svc.Logger != nil {
		h.svc.Logger.Warn("回写作品视频失败",
			zap.Uint64("work_id", id), zap.Int("index", index), zap.Error(err))
	}
}

// ---------------------------------------------------------------- 对外 /v1/videos

// VideoAPIHandler OpenAI 兼容的视频生成对外入口。
type VideoAPIHandler struct {
	svc *service.Container
}

// NewVideoAPIHandler 创建处理器。
func NewVideoAPIHandler(svc *service.Container) *VideoAPIHandler {
	return &VideoAPIHandler{svc: svc}
}

// Create 提交视频生成：POST /v1/videos
//
// 返回体对齐 Sora 风格：{id, object: "video", status, model}，
// id 为作品 ID，后续轮询与取内容都用它。
func (h *VideoAPIHandler) Create(c *gin.Context) {
	body, err := readRequestBody(c, h.svc)
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}
	if len(body) == 0 {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.New(apierr.ErrBadRequest, "请求体不能为空"))
		return
	}
	req, err := gateway.ParseVideoClientRequest(body)
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.Wrap(apierr.ErrInvalidJSON, err))
		return
	}
	if req.Model == "" {
		writeRelayError(c, gateway.ProtocolOpenAI, apierr.New(apierr.ErrInvalidParam, "缺少 model"))
		return
	}

	// 密钥级策略：模型白名单与额度限制（与 chat 转发同一道闸门）
	if idn := currentKeyIdentity(c); idn != nil {
		if !idn.ModelAllowed(req.Model) {
			writeRelayError(c, gateway.ProtocolOpenAI,
				apierr.New(apierr.ErrForbidden, "该 API Key 无权调用模型 "+req.Model))
			return
		}
		if idn.QuotaExceeded() {
			writeRelayError(c, gateway.ProtocolOpenAI,
				apierr.New(apierr.ErrQuotaExceeded, "该 API Key 的额度已用尽"))
			return
		}
	}

	submitted, err := h.svc.SubmitVideoWork(c.Request.Context(), CurrentUserID(c), service.VideoWorkInput{
		Model:          req.Model,
		Prompt:         req.Prompt,
		NegativePrompt: req.NegativePrompt,
		Seconds:        req.Seconds,
		Size:           req.Size,
		AspectRatio:    req.AspectRatio,
		FPS:            req.FPS,
		Image:          req.Image,
		Extra:          req.Extra,
	}, newImageCallMeta(h.svc, c))
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":       strconv.FormatUint(uint64(submitted.WorkID), 10),
		"object":   "video",
		"status":   externalVideoStatus(submitted.Status),
		"model":    req.Model,
		"progress": 0,
	})
}

// Show 查询任务状态：GET /v1/videos/{id}
//
// id 既接受作品 ID（POST 返回的），也接受上游任务 ID，便于直接对接 Sora SDK。
func (h *VideoAPIHandler) Show(c *gin.Context) {
	work, err := h.svc.GetVideoWorkByIdentifier(
		c.Request.Context(), CurrentUserID(c), c.Param("id"), IsAdmin(c))
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}
	out := gin.H{
		"id":       strconv.FormatUint(uint64(work.ID), 10),
		"object":   "video",
		"status":   externalVideoStatusFromText(work.StatusText),
		"model":    work.Model,
		"progress": work.Progress,
		"seconds":  work.DurationSec,
	}
	if work.ErrorMessage != "" {
		out["error"] = gin.H{"message": work.ErrorMessage}
	}
	c.JSON(http.StatusOK, out)
}

// Content 取生成内容：GET /v1/videos/{id}/content
//
// 已转存的直接回读文件流；只有上游地址的（转存失败兜底）302 跳转。
func (h *VideoAPIHandler) Content(c *gin.Context) {
	work, err := h.svc.GetVideoWorkByIdentifier(
		c.Request.Context(), CurrentUserID(c), c.Param("id"), IsAdmin(c))
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}
	file, upstreamURL, err := h.svc.VideoWorkContent(
		c.Request.Context(), CurrentUserID(c), work.ID, IsAdmin(c))
	if err != nil {
		writeRelayError(c, gateway.ProtocolOpenAI, err)
		return
	}
	if upstreamURL != "" {
		c.Redirect(http.StatusFound, upstreamURL)
		return
	}
	if err := writeVideo(c, file.Store, file.Key, file.ContentType, file.FileName); err != nil && h.svc.Logger != nil {
		h.svc.Logger.Warn("回写视频内容失败", zap.Uint("work_id", work.ID), zap.Error(err))
	}
}

// externalVideoStatus 把内部状态常量映射为对外文案。
func externalVideoStatus(status int) string {
	switch status {
	case model.VideoWorkStatusQueued:
		return "queued"
	case model.VideoWorkStatusProcessing:
		return "processing"
	case model.VideoWorkStatusSuccess:
		return "completed"
	default:
		return "failed"
	}
}

func externalVideoStatusFromText(text string) string {
	switch text {
	case "queued", "processing", "completed", "failed":
		return text
	default:
		return "queued"
	}
}
