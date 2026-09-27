package handler

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/redis/go-redis/v9"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/database"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// SystemHandler 健康检查、运行时信息与首次安装。
type SystemHandler struct {
	db     *gorm.DB
	rdb    *redis.Client
	logger *zap.Logger
	svc    *service.Container
	start  time.Time
	// Version 由构建时注入。
	Version string
}

// NewSystemHandler 创建处理器。
func NewSystemHandler(db *gorm.DB, rdb *redis.Client, lg *zap.Logger, version string, svc *service.Container) *SystemHandler {
	return &SystemHandler{db: db, rdb: rdb, logger: lg, svc: svc, start: time.Now(), Version: version}
}

// Status 系统初始化状态（公开）：前端据此决定是否进入安装向导。
func (h *SystemHandler) Status(c *gin.Context) {
	if h.svc == nil {
		response.Fail(c, apierr.New(apierr.ErrInternal, "服务未就绪"))
		return
	}
	status, err := h.svc.SystemStatus(c.Request.Context(), h.Version)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, status)
}

// Install 首次安装：创建管理员账号并直接登录。
func (h *SystemHandler) Install(c *gin.Context) {
	if h.svc == nil {
		response.Fail(c, apierr.New(apierr.ErrInternal, "服务未就绪"))
		return
	}
	var in service.InstallInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, err)
		return
	}
	result, err := h.svc.Install(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, result)
}

// Health 存活探针，不依赖外部组件。
func (h *SystemHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": h.Version,
		"uptime":  int64(time.Since(h.start).Seconds()),
	})
}

// Ready 就绪探针，检查数据库与缓存。
func (h *SystemHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{}
	healthy := true

	if err := database.PingPostgres(h.db); err != nil {
		checks["postgres"] = err.Error()
		healthy = false
	} else {
		checks["postgres"] = "ok"
	}
	if err := database.PingRedis(ctx, h.rdb); err != nil {
		checks["redis"] = err.Error()
		healthy = false
	} else {
		checks["redis"] = "ok"
	}

	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"status": boolToStatus(healthy), "checks": checks})
}

// Info 返回运行时信息。
func (h *SystemHandler) Info(c *gin.Context) {
	response.OK(c, gin.H{
		"version":    h.Version,
		"go":         runtime.Version(),
		"goroutines": runtime.NumGoroutine(),
		"uptime":     int64(time.Since(h.start).Seconds()),
	})
}

func boolToStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "unavailable"
}
