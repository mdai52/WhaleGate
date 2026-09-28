// Package app 负责应用装配、启动与优雅退出。
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/redis/go-redis/v9"

	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/pkg/database"
	"github.com/whalegate/whalegate/internal/pkg/logger"
	"github.com/whalegate/whalegate/internal/pkg/oauth"
	"github.com/whalegate/whalegate/internal/router"
	"github.com/whalegate/whalegate/internal/service"
)

// App 聚合进程内全部资源。
type App struct {
	Config  *config.Config
	Logger  *zap.Logger
	DB      *gorm.DB
	RDB     *redis.Client
	Service *service.Container
	Engine  *gin.Engine
	Server  *http.Server

	// flushStop 关闭额度落库协程。
	flushStop chan struct{}
	// flushWG 等待落库协程退出。
	flushWG sync.WaitGroup
}

// Options 启动参数。
type Options struct {
	// ConfigPath 配置文件路径，为空则按默认路径搜索。
	ConfigPath string
	// Version 构建版本号。
	Version string
	// SkipDB 跳过数据库与缓存连接（仅用于单元测试或纯命令场景）。
	SkipDB bool
}

// New 装配应用，但不启动监听。
func New(opts Options) (*App, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	lg, err := logger.New(cfg.Log)
	if err != nil {
		return nil, err
	}

	// 注册配置文件中声明的 OAuth 供应商（可覆盖内置定义）
	oauth.LoadFromConfig(cfg.OAuth.Providers)

	app := &App{Config: cfg, Logger: lg}
	if opts.SkipDB {
		return app, nil
	}

	app.DB, err = database.NewPostgres(cfg.Database, lg)
	if err != nil {
		return nil, err
	}
	app.RDB, err = database.NewRedis(cfg.Redis, lg)
	if err != nil {
		return nil, err
	}
	app.Service = service.New(cfg, app.DB, app.RDB, lg)

	// 从「数据库优先、配置文件兜底」加载 TLS 配置，决定监听协议与安全响应头。
	app.Service.LoadTLSSettings(context.Background())

	app.Engine = router.NewEngine(router.Deps{
		Config:  cfg,
		DB:      app.DB,
		RDB:     app.RDB,
		Logger:  lg,
		Svc:     app.Service,
		Version: opts.Version,
	})

	app.Server = &http.Server{
		Addr:         cfg.Addr(),
		Handler:      app.Engine,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}
	return app, nil
}

// startQuotaFlusher 周期性把 Redis 中累计的额度消耗落库。
func (a *App) startQuotaFlusher() {
	if a.Service == nil || a.RDB == nil {
		return
	}
	interval := a.Config.Gateway.QuotaFlushInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	a.flushStop = make(chan struct{})
	a.flushWG.Add(1)
	go func() {
		defer a.flushWG.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-a.flushStop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := a.Service.FlushQuotaDeltas(ctx); err != nil && a.Logger != nil {
					a.Logger.Warn("额度增量落库失败", zap.Error(err))
				}
				cancel()
			}
		}
	}()
}

// RunMigrations 执行或回滚数据库迁移。
func (a *App) RunMigrations(down bool) error {
	if a.DB == nil {
		return errors.New("数据库未初始化，无法执行迁移")
	}
	dsn := a.Config.Database.MigrationDSN()
	dir := a.Config.Database.MigrationDir
	if down {
		return database.MigrateDown(dsn, dir, a.Logger)
	}
	return database.MigrateUp(dsn, dir, a.Logger)
}

// MigrationVersion 返回当前迁移版本与脏标记。
func (a *App) MigrationVersion() (uint, bool, error) {
	if a.DB == nil {
		return 0, false, errors.New("数据库未初始化，无法查询迁移版本")
	}
	return database.MigrateVersion(a.Config.Database.MigrationDSN(), a.Config.Database.MigrationDir)
}

// BootstrapAdmin 首次安装时创建管理员账号。
// 随机初始密码由 service 层生成，只通过日志输出一次（见 service.BootstrapAdmin）。
func (a *App) BootstrapAdmin() error {
	if a.Service == nil || a.DB == nil {
		return nil
	}
	if a.Config != nil && !a.Config.Security.BootstrapAdmin {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := a.Service.BootstrapAdmin(ctx); err != nil {
		return fmt.Errorf("初始化管理员账号失败: %w", err)
	}
	return nil
}

// Run 启动 HTTP 服务并阻塞，直到 stop 关闭。
func (a *App) Run(stop <-chan struct{}) error {
	if a.Server == nil {
		return errors.New("HTTP 服务未初始化")
	}

	a.startQuotaFlusher()

	errCh := make(chan error, 1)
	go func() {
		if a.Service.TLSIsEnabled() {
			a.Logger.Info("鲸闸服务启动",
				zap.String("addr", a.Server.Addr),
				zap.String("mode", a.Config.Server.Mode),
				zap.Bool("tls", true))
			// 证书由 tlsProvider 动态提供，支持运行时轮换而无需重启。
			a.Server.TLSConfig = &tls.Config{
				GetCertificate: a.Service.GetCertificate,
				MinVersion:     tls.VersionTLS12,
			}
			if err := a.Server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("HTTPS 服务异常退出: %w", err)
			}
			return
		}
		a.Logger.Info("鲸闸服务启动",
			zap.String("addr", a.Server.Addr),
			zap.String("mode", a.Config.Server.Mode),
			zap.Bool("tls", false))
		if err := a.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP 服务异常退出: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-stop:
		return a.Shutdown(context.Background())
	case <-a.Service.RestartChan():
		a.Logger.Info("配置变更需重启以应用 HTTPS 监听模式，正在重新执行进程")
		if err := a.Shutdown(context.Background()); err != nil && a.Logger != nil {
			a.Logger.Warn("重启前优雅关闭出现错误", zap.Error(err))
		}
		a.reloadSelf()
		return nil
	}
}

// reloadSelf 优雅关闭后重新执行当前二进制，使监听模式（HTTP/HTTPS）的变更生效。
// 通过 syscall.Exec 替换进程映像，沿用同一容器/PID，避免额外停机。
func (a *App) reloadSelf() {
	if os.Getenv("WG_DISABLE_SELF_RESTART") == "1" {
		if a.Logger != nil {
			a.Logger.Warn("已通过环境变量 WG_DISABLE_SELF_RESTART 禁用自重启，请手动重启服务以应用 HTTPS 变更")
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		if a.Logger != nil {
			a.Logger.Error("获取当前可执行文件失败，无法自重启", zap.Error(err))
		}
		return
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
	}
	// #nosec G204 -- 重新执行自身二进制，参数与环境均来自可信来源
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		if a.Logger != nil {
			a.Logger.Error("自重启失败，请手动重启服务", zap.Error(err))
		}
	}
}

// Shutdown 优雅退出，释放数据库、缓存与日志资源。
func (a *App) Shutdown(ctx context.Context) error {
	timeout := a.Config.Server.ShutdownTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 关闭 MCP 子进程连接，避免残留孤儿进程。
	service.ShutdownMCP()

	var errs []error
	if a.Server != nil {
		if err := a.Server.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("关闭 HTTP 服务失败: %w", err))
		}
	}

	// 停止额度落库协程并做最后一次落库，避免丢账。
	if a.flushStop != nil {
		close(a.flushStop)
		a.flushWG.Wait()
	}
	if a.Service != nil {
		if a.RDB != nil {
			if err := a.Service.FlushQuotaDeltas(shutdownCtx); err != nil {
				errs = append(errs, fmt.Errorf("额度增量落库失败: %w", err))
			}
		}
		if err := a.Service.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.RDB != nil {
		if err := a.RDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 Redis 失败: %w", err))
		}
	}
	if a.DB != nil {
		if sqlDB, err := a.DB.DB(); err == nil {
			if err := sqlDB.Close(); err != nil {
				errs = append(errs, fmt.Errorf("关闭数据库失败: %w", err))
			}
		}
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
	}
	return errors.Join(errs...)
}
