package handler

import (
	"encoding/json"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// CanvasHandler 画布工作台（用户端）：画布 CRUD 与异步导出。
type CanvasHandler struct {
	svc *service.Container
}

// NewCanvasHandler 构造处理器。
func NewCanvasHandler(svc *service.Container) *CanvasHandler {
	return &CanvasHandler{svc: svc}
}

// canvasInput 解析画布入参；payload 允许传对象，这里统一取原始文本。
func canvasInput(c *gin.Context) (service.CanvasInput, bool) {
	var raw struct {
		Name       string      `json:"name"`
		Width      int         `json:"width"`
		Height     int         `json:"height"`
		Background string      `json:"background"`
		Payload    interface{} `json:"payload"`
	}
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.Fail(c, bindError(err))
		return service.CanvasInput{}, false
	}
	in := service.CanvasInput{
		Name:       raw.Name,
		Width:      raw.Width,
		Height:     raw.Height,
		Background: raw.Background,
	}
	if raw.Payload != nil {
		if text, ok := raw.Payload.(string); ok {
			in.Payload = text
		} else if body, err := json.Marshal(raw.Payload); err == nil {
			in.Payload = string(body)
		}
	}
	return in, true
}

// List 分页列出画布。
func (h *CanvasHandler) List(c *gin.Context) {
	p := ParsePage(c)
	userID := CurrentUserID(c)
	if userID == 0 {
		response.Fail(c, apierr.New(apierr.ErrUnauthorized, "请先登录"))
		return
	}
	list, total, err := h.svc.ListCanvases(c.Request.Context(), userID, p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, p.Page, p.PageSize)
}

// Create 新建画布。
func (h *CanvasHandler) Create(c *gin.Context) {
	userID := CurrentUserID(c)
	if userID == 0 {
		response.Fail(c, apierr.New(apierr.ErrUnauthorized, "请先登录"))
		return
	}
	in, ok := canvasInput(c)
	if !ok {
		return
	}
	// 未指定尺寸时给一个常用默认值，避免建出 0×0 的画布。
	if in.Width <= 0 {
		in.Width = 1280
	}
	if in.Height <= 0 {
		in.Height = 720
	}
	if in.Background == "" {
		in.Background = "#FFFFFF"
	}
	cv, err := h.svc.CreateCanvas(c.Request.Context(), userID, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, cv)
}

// Get 读取单张画布。
func (h *CanvasHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	cv, err := h.svc.GetCanvas(c.Request.Context(), CurrentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"id": cv.ID, "name": cv.Name, "width": cv.Width, "height": cv.Height,
		"background": cv.Background, "version": cv.Version,
		"payload":    cv.PayloadJSON(),
		"created_at": cv.CreatedAt, "updated_at": cv.UpdatedAt,
	})
}

// Update 保存画布。
func (h *CanvasHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := canvasInput(c)
	if !ok {
		return
	}
	cv, err := h.svc.UpdateCanvas(c.Request.Context(), CurrentUserID(c), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"id": cv.ID, "name": cv.Name, "width": cv.Width, "height": cv.Height,
		"background": cv.Background, "version": cv.Version,
		"payload": cv.PayloadJSON(), "updated_at": cv.UpdatedAt,
	})
}

// Delete 删除画布。
func (h *CanvasHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteCanvas(c.Request.Context(), CurrentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// SubmitExport 提交导出任务；队列满时返回限流错误，由前端提示稍后重试。
func (h *CanvasHandler) SubmitExport(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		Format string `json:"format"`
		Scale  int    `json:"scale"`
	}
	_ = c.ShouldBindJSON(&req)
	// 复用生图的上下文提取：导出是异步完成的，日志要在提交时先把来源记下来。
	exp, err := h.svc.SubmitCanvasExport(c.Request.Context(), CurrentUserID(c), id,
		req.Format, req.Scale, newImageCallMeta(h.svc, c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, h.toExportView(exp))
}

// ListExports 列出导出任务（前端轮询这个接口看进度）。
func (h *CanvasHandler) ListExports(c *gin.Context) {
	p := ParsePage(c)
	list, total, err := h.svc.ListCanvasExports(c.Request.Context(), CurrentUserID(c), p.Page, p.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	views := make([]gin.H, 0, len(list))
	for i := range list {
		views = append(views, h.toExportView(&list[i]))
	}
	response.Page(c, views, total, p.Page, p.PageSize)
}

// GetExport 读取单个导出任务。
func (h *CanvasHandler) GetExport(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	exp, err := h.svc.GetCanvasExport(c.Request.Context(), CurrentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, h.toExportView(exp))
}

// Download 下载导出结果。
func (h *CanvasHandler) Download(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	file, err := h.svc.CanvasExportFile(c.Request.Context(), CurrentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer file.Reader.Close()
	c.Header("Content-Type", file.ContentType)
	c.Header("Content-Disposition", "attachment; filename=\""+file.FileName+"\"")
	c.DataFromReader(200, -1, file.ContentType, file.Reader, nil)
}

// toExportView 组装导出任务视图：能直连存储时给直链，否则给需鉴权的网关地址。
func (h *CanvasHandler) toExportView(exp *model.CanvasExport) gin.H {
	view := gin.H{
		"id":         exp.ID,
		"canvas_id":  exp.CanvasID,
		"format":     exp.Format,
		"scale":      exp.Scale,
		"status":     exp.Status,
		"progress":   exp.Progress,
		"width":      exp.Width,
		"height":     exp.Height,
		"bytes":      exp.Bytes,
		"latency_ms": exp.LatencyMS,
		"queued_at":  exp.QueuedAt,
	}
	if exp.ErrorMessage != "" {
		view["error_message"] = exp.ErrorMessage
	}
	if public := h.svc.CanvasExportURL(exp); public != "" {
		view["url"] = public
	} else if exp.Status == model.CanvasExportStatusSuccess {
		view["url"] = "/api/v1/studio/canvas-exports/" + strconv.FormatUint(uint64(exp.ID), 10) + "/content"
	}
	return view
}

// pathID 解析路径上的 ID，非法时直接写回错误响应。
func pathID(c *gin.Context) (uint, bool) {
	raw, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || raw == 0 {
		response.Fail(c, errInvalidID)
		return 0, false
	}
	return uint(raw), true
}
