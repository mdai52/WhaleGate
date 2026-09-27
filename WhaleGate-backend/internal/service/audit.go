package service

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

// 审计日志异步写入参数。
const (
	auditBatchSize    = 100
	auditFlushTimeout = time.Second
)

// AuditEntry 一条审计记录。
type AuditEntry struct {
	UserID     uint
	Username   string
	Action     string
	TargetType string
	TargetID   string
	Status     int
	Detail     string
	IP         string
	UserAgent  string
	TraceID    string
}

// AuditWriter 批量异步写入审计日志，避免阻塞主流程。
type AuditWriter struct {
	ch      chan *model.AuditLog
	db      *gorm.DB
	logger  *zap.Logger
	closeMu sync.Mutex
	closed  bool
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewAuditWriter 创建并启动审计写入器。
func NewAuditWriter(db *gorm.DB, lg *zap.Logger, buffer int) *AuditWriter {
	if buffer <= 0 {
		buffer = 1024
	}
	w := &AuditWriter{ch: make(chan *model.AuditLog, buffer), db: db, logger: lg, done: make(chan struct{})}
	w.wg.Add(1)
	go w.loop()
	return w
}

func (w *AuditWriter) loop() {
	defer w.wg.Done()
	ticker := time.NewTicker(auditFlushTimeout)
	defer ticker.Stop()

	batch := make([]*model.AuditLog, 0, auditBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.db.WithContext(context.Background()).CreateInBatches(batch, len(batch)).Error; err != nil {
			if w.logger != nil {
				w.logger.Error("写入审计日志失败", zap.Int("count", len(batch)), zap.Error(err))
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-w.done:
			flush()
			w.flushRest()
			return
		case entry := <-w.ch:
			batch = append(batch, entry)
			if len(batch) >= auditBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (w *AuditWriter) flushRest() {
	for {
		select {
		case entry := <-w.ch:
			if err := w.db.WithContext(context.Background()).Create(entry).Error; err != nil && w.logger != nil {
				w.logger.Error("写入审计日志失败", zap.Error(err))
			}
		default:
			return
		}
	}
}

// Submit 提交审计记录；缓冲满时丢弃并记录告警（审计不宜阻塞业务）。
func (w *AuditWriter) Submit(entry *model.AuditLog) {
	if w == nil || entry == nil {
		return
	}
	select {
	case w.ch <- entry:
	default:
		if w.logger != nil {
			w.logger.Warn("审计缓冲已满，丢弃一条记录", zap.String("action", entry.Action))
		}
	}
}

// Close 停止写入器并落盘剩余记录。
func (w *AuditWriter) Close() {
	if w == nil {
		return
	}
	w.closeMu.Lock()
	if w.closed {
		w.closeMu.Unlock()
		return
	}
	w.closed = true
	w.closeMu.Unlock()

	close(w.done)
	w.wg.Wait()
}

// Audit 记录一条审计日志。
func (c *Container) Audit(entry AuditEntry) {
	if c.Audits == nil {
		return
	}
	status := entry.Status
	if status == 0 {
		status = model.AuditStatusSuccess
	}
	c.Audits.Submit(&model.AuditLog{
		UserID:     entry.UserID,
		Username:   truncate(entry.Username, 64),
		Action:     truncate(entry.Action, 64),
		TargetType: truncate(entry.TargetType, 64),
		TargetID:   truncate(entry.TargetID, 64),
		Status:     status,
		Detail:     truncate(entry.Detail, 500),
		IP:         truncate(entry.IP, 64),
		UserAgent:  truncate(entry.UserAgent, 500),
		TraceID:    truncate(entry.TraceID, 64),
	})
}

// ListAuditLogs 分页查询审计日志（管理端）。
func (c *Container) ListAuditLogs(ctx context.Context, f AuditFilter) ([]model.AuditLog, int64, error) {
	page, pageSize := normalizePage(f.Page, f.PageSize)
	q := c.DB.WithContext(ctx).Model(&model.AuditLog{})
	if f.UserID > 0 {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.Start != nil {
		q = q.Where("created_at >= ?", *f.Start)
	}
	if f.End != nil {
		q = q.Where("created_at <= ?", *f.End)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var list []model.AuditLog
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, total, nil
}

// AuditFilter 审计日志查询条件。
type AuditFilter struct {
	UserID   uint
	Action   string
	Start    *time.Time
	End      *time.Time
	Page     int
	PageSize int
}
