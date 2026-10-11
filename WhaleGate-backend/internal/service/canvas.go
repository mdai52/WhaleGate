package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/canvas"
	"github.com/whalegate/whalegate/internal/pkg/storage"
)

// ---------------------------------------------------------------- 配置兜底

// canvasLimits 画布尺寸与规模上限；未配置时用画布包的兜底值。
func (c *Container) canvasLimits() canvas.Limits {
	cfg := c.Config.Studio
	return canvas.Limits{
		MaxDimension:  cfg.CanvasMaxDimension,
		MaxPixels:     int64(cfg.CanvasMaxPixels),
		MaxNodes:      cfg.CanvasMaxNodes,
		MaxImageBytes: int64(cfg.CanvasMaxImageBytes),
		MaxScale:      cfg.CanvasMaxScale,
	}
}

// canvasWorkers 渲染并发数：默认取 CPU 核数与 4 的较小值。
//
// 渲染是纯 CPU 活，超过核数不会更快，只会让每张画布的完成时间一起变长。
func (c *Container) canvasWorkers() int {
	n := c.Config.Studio.CanvasWorkers
	if n <= 0 {
		n = runtime.NumCPU()
		if n > 4 {
			n = 4
		}
	}
	if n < 1 {
		n = 1
	}
	return n
}

func (c *Container) canvasQueueSize() int {
	if n := c.Config.Studio.CanvasQueueSize; n > 0 {
		return n
	}
	return 64
}

// canvasTaskTimeout 单个导出任务的最长执行时间。
func (c *Container) canvasTaskTimeout() time.Duration {
	if d := c.Config.Studio.CanvasTaskTimeout; d > 0 {
		return d
	}
	return 120 * time.Second
}

// canvasPerUserInflight 单用户同时在队（排队+渲染中）的导出任务上限。
const canvasPerUserInflight = 3

// ---------------------------------------------------------------- 导出队列

// canvasQueue 导出任务队列。
//
// 之所以要显式排队而不是像视频那样「来一个起一个 goroutine」：
// 渲染是本机 CPU 与内存密集操作，任务之间没有等待外部 IO 的空隙，
// 无上限并发只会让所有任务一起变慢，并在内存峰值上叠加。
// 有界队列 + 固定 worker 能把内存峰值压到「并发数 × 单张画布」这个可预期的量级。
type canvasQueue struct {
	ch      chan uint
	workers int
	once    sync.Once
}

var (
	canvasQueueMu    sync.Mutex
	canvasQueueInst  *canvasQueue
	canvasStopSignal chan struct{}
)

// startCanvasWorkers 启动导出 worker，全程只生效一次。
func (c *Container) StartCanvasWorkers() {
	canvasQueueMu.Lock()
	defer canvasQueueMu.Unlock()
	if canvasQueueInst != nil {
		return
	}
	q := &canvasQueue{ch: make(chan uint, c.canvasQueueSize()), workers: c.canvasWorkers()}
	canvasQueueInst = q
	canvasStopSignal = make(chan struct{})
	for i := 0; i < q.workers; i++ {
		go c.canvasWorker(q.ch, canvasStopSignal)
	}
	if c.Logger != nil {
		c.Logger.Info("画布导出 worker 已启动",
			zap.Int("workers", q.workers), zap.Int("queue_size", cap(q.ch)))
	}
}

// canvasWorker 逐个消费导出任务。
func (c *Container) canvasWorker(ch <-chan uint, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case id := <-ch:
			c.runCanvasExport(id)
		}
	}
}

// enqueueCanvasExport 把任务放进队列；队列满时直接拒绝，
// 让调用方立刻知道「现在忙」，而不是无限期排队。
func (c *Container) enqueueCanvasExport(id uint) error {
	canvasQueueMu.Lock()
	q := canvasQueueInst
	canvasQueueMu.Unlock()
	if q == nil {
		return apierr.New(apierr.ErrInternal, "导出队列未就绪")
	}
	select {
	case q.ch <- id:
		return nil
	default:
		return apierr.New(apierr.ErrRateLimited, "导出队列已满，请稍后再试")
	}
}

// runCanvasExport 执行一次导出：渲染 → 转存 → 落状态。
func (c *Container) runCanvasExport(id uint) {
	ctx, cancel := context.WithTimeout(context.Background(), c.canvasTaskTimeout())
	defer cancel()

	var exp model.CanvasExport
	if err := c.DB.WithContext(ctx).First(&exp, id).Error; err != nil {
		return
	}
	started := time.Now()
	c.markCanvasExport(&exp, model.CanvasExportStatusProcessing, 10, "", nil)

	var cv model.Canvas
	if err := c.DB.WithContext(ctx).First(&cv, exp.CanvasID).Error; err != nil {
		c.failCanvasExport(&exp, "画布不存在或已被删除", started)
		return
	}

	var scene canvas.Scene
	if err := json.Unmarshal([]byte(cv.Payload), &scene); err != nil {
		c.failCanvasExport(&exp, "画布内容已损坏，无法解析", started)
		return
	}
	scene.Width, scene.Height = cv.Width, cv.Height
	scene.Background = cv.Background

	format, scale, err := canvas.ValidateExport(exp.Format, exp.Scale, c.canvasLimits())
	if err != nil {
		c.failCanvasExport(&exp, err.Error(), started)
		return
	}

	res, err := canvas.Render(scene, canvas.Options{
		Format:   format,
		Scale:    scale,
		Quality:  c.Config.Studio.CanvasJPEGQuality,
		FontPath: c.Config.Studio.CanvasFontPath,
		Limits:   c.canvasLimits(),
	})
	if err != nil {
		c.failCanvasExport(&exp, err.Error(), started)
		return
	}
	// 渲染结果立刻交给存储，之后 res.Data 交给 GC：
	// 攒着不放会让「并发数 × 画布大小」的内存一直占着。
	c.markCanvasExport(&exp, model.CanvasExportStatusProcessing, 70, "", nil)

	key := storage.JoinKey("canvas", fmt.Sprint(exp.CanvasID), fmt.Sprintf("%d%s", exp.ID, canvas.Ext(format)))
	store := c.studioStore.Current()
	if err := store.Put(ctx, storage.Object{
		Key:         key,
		ContentType: res.MimeType,
		Data:        res.Data,
	}); err != nil {
		c.failCanvasExport(&exp, "导出结果转存失败: "+err.Error(), started)
		return
	}

	now := time.Now()
	exp.Status = model.CanvasExportStatusSuccess
	exp.Progress = 100
	exp.StorageKind = store.Kind()
	exp.StorageKey = key
	exp.Bytes = int64(len(res.Data))
	exp.Width, exp.Height = res.Width, res.Height
	exp.ErrorMessage = ""
	exp.LatencyMS = int(now.Sub(started).Milliseconds())
	exp.StartedAt = &started
	exp.FinishedAt = &now
	if err := c.saveCanvasExport(&exp); err != nil && c.Logger != nil {
		c.Logger.Warn("更新导出状态失败", zap.Uint("id", exp.ID), zap.Error(err))
	}
	c.logCanvasExport(&exp, model.CallStatusSuccess, "", takeCanvasMeta(exp.ID))
}

// markCanvasExport 更新任务状态与进度。
func (c *Container) markCanvasExport(exp *model.CanvasExport, status, progress int, errMsg string, at *time.Time) {
	exp.Status = status
	exp.Progress = progress
	if errMsg != "" {
		exp.ErrorMessage = truncate(errMsg, 500)
	}
	if at != nil {
		exp.StartedAt = at
	}
	_ = c.saveCanvasExport(exp)
}

func (c *Container) failCanvasExport(exp *model.CanvasExport, reason string, started time.Time) {
	now := time.Now()
	exp.Status = model.CanvasExportStatusFailed
	exp.Progress = 100
	exp.ErrorMessage = truncate(reason, 500)
	exp.LatencyMS = int(now.Sub(started).Milliseconds())
	exp.StartedAt = &started
	exp.FinishedAt = &now
	if err := c.saveCanvasExport(exp); err != nil && c.Logger != nil {
		c.Logger.Warn("标记导出失败时写库失败", zap.Uint("id", exp.ID), zap.Error(err))
	}
	c.logCanvasExport(exp, model.CallStatusFailed, reason, takeCanvasMeta(exp.ID))
}

func (c *Container) saveCanvasExport(exp *model.CanvasExport) error {
	return c.DB.WithContext(context.Background()).Model(exp).
		Select("status", "progress", "storage_kind", "storage_key", "bytes",
			"width", "height", "error_message", "latency_ms", "started_at", "finished_at").
		Updates(exp).Error
}

// ---------------------------------------------------------------- 画布 CRUD

// ListCanvases 分页列出当前用户的画布。
func (c *Container) ListCanvases(ctx context.Context, userID uint, page, pageSize int) ([]model.Canvas, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.Canvas{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var list []model.Canvas
	if err := q.Order("updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, total, nil
}

// GetCanvas 读取画布，校验归属。
func (c *Container) GetCanvas(ctx context.Context, userID, id uint) (*model.Canvas, error) {
	var cv model.Canvas
	if err := c.DB.WithContext(ctx).Where("user_id = ?", userID).First(&cv, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	return &cv, nil
}

// CreateCanvas 新建画布。
func (c *Container) CreateCanvas(ctx context.Context, userID uint, in CanvasInput) (*model.Canvas, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "未命名画布"
	}
	if len(name) > 128 {
		return nil, apierr.New(apierr.ErrInvalidParam, "画布名称不能超过 128 个字符")
	}
	payload, err := normalizeCanvasPayload(in.Payload, in.Width, in.Height, in.Background)
	if err != nil {
		return nil, err
	}
	if err := validateCanvasSize(in.Width, in.Height, c.canvasLimits()); err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	now := time.Now()
	cv := &model.Canvas{
		UserID:     userID,
		Name:       name,
		Width:      in.Width,
		Height:     in.Height,
		Background: in.Background,
		Payload:    payload,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := c.DB.WithContext(ctx).Create(cv).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return cv, nil
}

// UpdateCanvas 保存画布内容与尺寸。
func (c *Container) UpdateCanvas(ctx context.Context, userID, id uint, in CanvasInput) (*model.Canvas, error) {
	cv, err := c.GetCanvas(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		if len(name) > 128 {
			return nil, apierr.New(apierr.ErrInvalidParam, "画布名称不能超过 128 个字符")
		}
		cv.Name = name
	}
	if in.Width > 0 {
		cv.Width = in.Width
	}
	if in.Height > 0 {
		cv.Height = in.Height
	}
	if in.Background != "" {
		cv.Background = in.Background
	}
	if err := validateCanvasSize(cv.Width, cv.Height, c.canvasLimits()); err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if in.Payload != "" {
		payload, err := normalizeCanvasPayload(in.Payload, cv.Width, cv.Height, cv.Background)
		if err != nil {
			return nil, err
		}
		cv.Payload = payload
	}
	cv.Version++
	cv.UpdatedAt = time.Now()
	if err := c.DB.WithContext(ctx).Save(cv).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return cv, nil
}

// DeleteCanvas 删除画布与其导出记录，并清理已转存的导出文件。
func (c *Container) DeleteCanvas(ctx context.Context, userID, id uint) error {
	cv, err := c.GetCanvas(ctx, userID, id)
	if err != nil {
		return err
	}
	var exports []model.CanvasExport
	_ = c.DB.WithContext(ctx).Where("canvas_id = ?", id).Find(&exports).Error
	if err := c.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("canvas_id = ?", id).Delete(&model.CanvasExport{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Canvas{}, id).Error
	}); err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	// 存储清理失败只记日志：画布记录已删，残留对象不影响使用。
	for _, exp := range exports {
		if exp.StorageKey == "" {
			continue
		}
		if err := c.studioStore.For(exp.StorageKind).Delete(ctx, exp.StorageKey); err != nil && c.Logger != nil {
			c.Logger.Warn("删除导出文件失败", zap.String("key", exp.StorageKey), zap.Error(err))
		}
	}
	_ = cv
	return nil
}

// ---------------------------------------------------------------- 导出任务

// SubmitCanvasExport 提交导出任务；队列满时返回限流错误。
// 画布导出在调用历史里的标识。
const (
	// CanvasCallModel 对外模型名：画布导出不经过模型，但调用历史需要一个可筛选的标识。
	CanvasCallModel = "canvas"
	// CanvasCallProtocol 调用类型标识，用于与 openai / gemini 等区分。
	CanvasCallProtocol = "canvas"
)

// SubmitCanvasExport 提交导出任务；队列满时返回限流错误。
//
// meta 是调用上下文（用户、密钥、IP、trace）：导出异步完成，
// 真正落日志时已经没有 HTTP 上下文可用，只能在这里先记一份。
func (c *Container) SubmitCanvasExport(ctx context.Context, userID, canvasID uint, format string, scale int, meta *CallMeta) (*model.CanvasExport, error) {
	if _, err := c.GetCanvas(ctx, userID, canvasID); err != nil {
		return nil, err
	}
	// 每用户同时在队（排队+渲染中）的任务数上限：渲染是 CPU/内存密集操作，
	// 队列容量有限，不限制的话单个用户可以刷满队列饿死其他人。
	// 上万人并发的场景下，这是除「队列满直接拒绝」之外的第二道公平性闸门。
	var inflight int64
	if err := c.DB.WithContext(ctx).Model(&model.CanvasExport{}).
		Where("user_id = ? AND status IN ?", userID,
			[]int{model.CanvasExportStatusQueued, model.CanvasExportStatusProcessing}).
		Count(&inflight).Error; err == nil && inflight >= canvasPerUserInflight {
		return nil, apierr.Errorf(apierr.ErrRateLimited,
			"已有 %d 个导出任务在排队或渲染中，请等待完成后再提交", canvasPerUserInflight)
	}
	// 校验失败时 format / scale 会返回零值，先留一份原始入参用于记录：
	// 日志里出现 "@0x" 这种空壳看不出用户到底请求了什么。
	requestedFormat, requestedScale := format, scale
	format, scale, err := canvas.ValidateExport(format, scale, c.canvasLimits())
	if err != nil {
		// 参数不合法也要留痕：这类失败往往来自前端传错，调用历史里能看到才好排查。
		if requestedFormat == "" {
			requestedFormat = canvas.FormatPNG
		}
		if requestedScale <= 0 {
			requestedScale = 1
		}
		c.logCanvasExport(&model.CanvasExport{
			CanvasID: canvasID, UserID: userID, Format: requestedFormat, Scale: requestedScale,
		}, model.CallStatusFailed, err.Error(), meta)
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	now := time.Now()
	exp := &model.CanvasExport{
		CanvasID: canvasID,
		UserID:   userID,
		Format:   format,
		Scale:    scale,
		Status:   model.CanvasExportStatusQueued,
		QueuedAt: now,
	}
	if err := c.DB.WithContext(ctx).Create(exp).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	rememberCanvasMeta(exp.ID, meta)
	// 先落库再入队：worker 拿到 ID 时记录一定已经可见。
	if err := c.enqueueCanvasExport(exp.ID); err != nil {
		// 入队失败时把记录一并标成失败，避免前端轮询一个永远不会推进的任务。
		exp.Status = model.CanvasExportStatusFailed
		exp.ErrorMessage = truncate(err.Error(), 500)
		finished := time.Now()
		exp.FinishedAt = &finished
		_ = c.saveCanvasExport(exp)
		c.logCanvasExport(exp, model.CallStatusFailed, err.Error(), takeCanvasMeta(exp.ID))
		return nil, err
	}
	return exp, nil
}

// logCanvasExport 把一次导出写进调用历史。
//
// 画布导出不扣点（纯本机渲染，没有上游成本），但必须可查：
// 谁在什么时候导了多大的图、耗时多少、成功还是失败。
// token 一律为 0——这次调用确实没有 token，留空比填估算值更诚实；
// 真正的计量单位是像素数（对应渲染开销）与字节数（对应存储开销）。
func (c *Container) logCanvasExport(exp *model.CanvasExport, status int, errMsg string, meta *CallMeta) {
	if exp == nil || c.Logs == nil {
		return
	}
	entry := &model.CallLog{
		Model:         CanvasCallModel,
		UpstreamModel: fmt.Sprintf("%s@%dx", exp.Format, exp.Scale),
		Protocol:      CanvasCallProtocol,
		Status:        status,
		HTTPStatus:    http.StatusOK,
		ErrorMessage:  truncate(errMsg, 500),
		LatencyMS:     int64(exp.LatencyMS),
		// Points 保持 0：本机渲染不计费，这里只做记录不做扣减。
		CreatedAt: time.Now(),
	}
	if status == model.CallStatusSuccess {
		entry.CanvasCount = 1
		entry.CanvasPixels = int64(exp.Width) * int64(exp.Height)
		entry.CanvasBytes = exp.Bytes
	} else {
		entry.HTTPStatus = http.StatusInternalServerError
	}
	if meta != nil {
		meta.applyTo(entry)
	}
	if entry.UserID == 0 {
		entry.UserID = exp.UserID
	}
	c.Logs.Submit(entry)
}

// ---------------------------------------------------------------- 调用上下文暂存

// pendingMeta 提交时暂存的调用上下文，导出完成后取走。
//
// 之所以要暂存：worker 在请求之外执行，那时 HTTP 上下文已经销毁。
// 进程重启会丢掉暂存项，此时日志只缺 IP 与 trace，用户与时间仍然准确。
var (
	pendingMetaMu sync.Mutex
	pendingMeta   = map[uint]pendingMetaEntry{}
)

type pendingMetaEntry struct {
	meta *CallMeta
	at   time.Time
}

func rememberCanvasMeta(id uint, meta *CallMeta) {
	if meta == nil {
		return
	}
	pendingMetaMu.Lock()
	defer pendingMetaMu.Unlock()
	// 顺手清掉超期条目：任务失败或进程异常都可能留下不再被取走的项。
	if len(pendingMeta) > 256 {
		deadline := time.Now().Add(-5 * time.Minute)
		for key, item := range pendingMeta {
			if item.at.Before(deadline) {
				delete(pendingMeta, key)
			}
		}
	}
	pendingMeta[id] = pendingMetaEntry{meta: meta, at: time.Now()}
}

// takeCanvasMeta 取出并移除暂存上下文。
func takeCanvasMeta(id uint) *CallMeta {
	pendingMetaMu.Lock()
	defer pendingMetaMu.Unlock()
	item, ok := pendingMeta[id]
	if !ok {
		return nil
	}
	delete(pendingMeta, id)
	return item.meta
}

// ListCanvasExports 列出导出任务，顺带收口超期的挂起任务。
func (c *Container) ListCanvasExports(ctx context.Context, userID uint, page, pageSize int) ([]model.CanvasExport, int64, error) {
	c.abandonStaleCanvasExports(ctx, userID)
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.CanvasExport{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var list []model.CanvasExport
	if err := q.Order("queued_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, total, nil
}

// GetCanvasExport 读取单个导出任务。
func (c *Container) GetCanvasExport(ctx context.Context, userID, id uint) (*model.CanvasExport, error) {
	var exp model.CanvasExport
	if err := c.DB.WithContext(ctx).Where("user_id = ?", userID).First(&exp, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	return &exp, nil
}

// CanvasExportFile 打开导出结果的读取流。
func (c *Container) CanvasExportFile(ctx context.Context, userID, id uint) (*WorkImage, error) {
	exp, err := c.GetCanvasExport(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if exp.Status != model.CanvasExportStatusSuccess || exp.StorageKey == "" {
		return nil, apierr.New(apierr.ErrNotFound, "导出尚未完成")
	}
	reader, err := c.studioStore.For(exp.StorageKind).Open(ctx, exp.StorageKey)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	mime := canvas.MimeType(exp.Format)
	return &WorkImage{
		Reader:      reader,
		ContentType: mime,
		FileName:    fmt.Sprintf("whalegate-canvas-%d-%d%s", exp.CanvasID, exp.ID, canvas.Ext(exp.Format)),
	}, nil
}

// CanvasExportURL 导出结果的公开直链，未配置公开地址时为空（走网关代理回读）。
func (c *Container) CanvasExportURL(exp *model.CanvasExport) string {
	if exp.StorageKey == "" {
		return ""
	}
	return c.studioStore.For(exp.StorageKind).PublicURL(exp.StorageKey)
}

// abandonStaleCanvasExports 把超时仍挂起的任务判为失败。
//
// worker 只存在于当前进程：进程重启后遗留的排队任务永远不会被推进，
// 查询时统一收口，避免前端一直轮询一个死任务。
func (c *Container) abandonStaleCanvasExports(ctx context.Context, userID uint) {
	deadline := time.Now().Add(-c.canvasTaskTimeout() * 2)
	err := c.DB.WithContext(ctx).Model(&model.CanvasExport{}).
		Where("user_id = ? AND status IN ? AND queued_at < ?",
			userID,
			[]int{model.CanvasExportStatusQueued, model.CanvasExportStatusProcessing},
			deadline).
		Updates(map[string]interface{}{
			"status":        model.CanvasExportStatusFailed,
			"progress":      100,
			"error_message": "导出超时或进程重启，任务已取消",
			"finished_at":   time.Now(),
		}).Error
	if err != nil && c.Logger != nil {
		c.Logger.Warn("收口超期导出任务失败", zap.Error(err))
	}
}

// ---------------------------------------------------------------- 入参

// CanvasInput 画布的新建与保存入参。
type CanvasInput struct {
	Name       string `json:"name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Background string `json:"background"`
	// Payload 画布内容（JSON 文本或对象），节点列表在其中。
	Payload string `json:"payload"`
}

// normalizeCanvasPayload 校验画布内容并把尺寸/背景补齐后写回。
//
// 尺寸与背景以画布记录为准（它们要参与列表展示），
// payload 里若也带了同名字段，以记录为准覆盖，避免两处不一致。
func normalizeCanvasPayload(payload string, width, height int, background string) (string, error) {
	if strings.TrimSpace(payload) == "" {
		payload = `{"nodes":[]}`
	}
	var scene canvas.Scene
	if err := json.Unmarshal([]byte(payload), &scene); err != nil {
		return "", apierr.New(apierr.ErrInvalidParam, "画布内容不是合法 JSON")
	}
	if width > 0 {
		scene.Width = width
	}
	if height > 0 {
		scene.Height = height
	}
	if background != "" {
		scene.Background = background
	}
	out, err := json.Marshal(scene)
	if err != nil {
		return "", apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	return string(out), nil
}

func validateCanvasSize(width, height int, limits canvas.Limits) error {
	return canvas.Scene{Width: width, Height: height}.Validate(limits)
}
