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

// 异步写日志参数。
const (
	logBatchSize    = 200
	logFlushTimeout = 500 * time.Millisecond
)

// CallLogWriter 批量异步写入调用日志，避免阻塞转发主流程。
type CallLogWriter struct {
	ch      chan *model.CallLog
	db      *gorm.DB
	logger  *zap.Logger
	closeMu sync.Mutex
	closed  bool
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewCallLogWriter 创建并启动写入器，buffer 为通道缓冲大小。
func NewCallLogWriter(db *gorm.DB, lg *zap.Logger, buffer int) *CallLogWriter {
	if buffer <= 0 {
		buffer = 1024
	}
	w := &CallLogWriter{
		ch:     make(chan *model.CallLog, buffer),
		db:     db,
		logger: lg,
		done:   make(chan struct{}),
	}
	w.wg.Add(1)
	go w.loop()
	return w
}

// Submit 提交一条日志；通道满时丢弃并记录告警，绝不阻塞请求。
func (w *CallLogWriter) Submit(log *model.CallLog) {
	if w == nil || log == nil {
		return
	}
	select {
	case w.ch <- log:
	default:
		if w.logger != nil {
			w.logger.Warn("调用日志缓冲已满，丢弃一条记录", zap.String("trace_id", log.TraceID))
		}
	}
}

// Close 停止写入器并落盘剩余日志。
func (w *CallLogWriter) Close() {
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
	w.flush(context.Background(), w.drain())
}

func (w *CallLogWriter) loop() {
	defer w.wg.Done()

	ticker := time.NewTicker(logFlushTimeout)
	defer ticker.Stop()

	batch := make([]*model.CallLog, 0, logBatchSize)
	for {
		select {
		case <-w.done:
			// 退出前先落盘手中未刷新的批次，避免关停丢日志。
			w.flush(context.Background(), batch)
			return
		case entry := <-w.ch:
			batch = append(batch, entry)
			if len(batch) >= logBatchSize {
				w.flush(context.Background(), batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(context.Background(), batch)
				batch = batch[:0]
			}
		}
	}
}

// drain 取出通道中剩余的日志（Close 时使用）。
func (w *CallLogWriter) drain() []*model.CallLog {
	var rest []*model.CallLog
	for {
		select {
		case entry := <-w.ch:
			rest = append(rest, entry)
		default:
			return rest
		}
	}
}

func (w *CallLogWriter) flush(ctx context.Context, batch []*model.CallLog) {
	if len(batch) == 0 {
		return
	}
	if err := w.db.WithContext(ctx).CreateInBatches(batch, len(batch)).Error; err != nil {
		if w.logger != nil {
			w.logger.Error("写入调用日志失败", zap.Int("count", len(batch)), zap.Error(err))
		}
	}
}

// ---------------------------------------------------------------- 查询

// CallLogFilter 调用日志查询条件。
type CallLogFilter struct {
	// UserID 为 0 表示不限（管理端）。
	UserID   uint
	APIKeyID uint
	Model    string
	Status   int
	Start    *time.Time
	End      *time.Time
	Page     int
	PageSize int
}

// ListCallLogs 分页查询调用日志。
func (c *Container) ListCallLogs(ctx context.Context, f CallLogFilter) ([]model.CallLog, int64, error) {
	page, pageSize := normalizePage(f.Page, f.PageSize)
	q := c.DB.WithContext(ctx).Model(&model.CallLog{})
	if f.UserID > 0 {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.APIKeyID > 0 {
		q = q.Where("api_key_id = ?", f.APIKeyID)
	}
	if f.Model != "" {
		q = q.Where("model = ?", f.Model)
	}
	if f.Status > 0 {
		q = q.Where("status = ?", f.Status)
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
	var list []model.CallLog
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, total, nil
}

// UsagePoint 按天聚合的用量点。
type UsagePoint struct {
	Date             string `json:"date"`
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	ReasoningTokens  int64  `json:"reasoning_tokens"`
	Points           int64  `json:"points"`
}

// UsageSummary 查询按天聚合的用量，用于门户图表。
func (c *Container) UsageSummary(ctx context.Context, userID uint, days int) ([]UsagePoint, error) {
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}

	rows := make([]UsagePoint, 0, days)
	q := c.DB.WithContext(ctx).Model(&model.CallLog{}).
		Select("to_char(created_at, 'YYYY-MM-DD') AS date, "+
			"COUNT(*) AS requests, "+
			"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, "+
			"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, "+
			"COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens, "+
			"COALESCE(SUM(points), 0) AS points").
		Where("created_at >= now() - (? * interval '1 day')", days)
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Group("date").Order("date ASC").Scan(&rows).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return rows, nil
}
