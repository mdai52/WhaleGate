// Package database 封装 PostgreSQL 连接、Redis 连接与版本化迁移。
package database

import (
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/whalegate/whalegate/internal/config"
)

// NewPostgres 建立 GORM 数据库连接并配置连接池。
func NewPostgres(cfg config.DatabaseConfig, lg *zap.Logger) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger:                                   newGormLogger(cfg, lg),
		PrepareStmt:                              true,
		TranslateError:                           true,
		DisableForeignKeyConstraintWhenMigrating: false,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层 *sql.DB 失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("PostgreSQL ping 失败: %w", err)
	}
	return db, nil
}

func newGormLogger(cfg config.DatabaseConfig, lg *zap.Logger) gormlogger.Interface {
	level := gormlogger.Warn
	if cfg.SlowThreshold > 0 {
		level = gormlogger.Info
	}
	return gormlogger.New(&gormLogBridge{lg: lg}, gormlogger.Config{
		SlowThreshold:             cfg.SlowThreshold,
		LogLevel:                  level,
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})
}

// gormLogBridge 把 gorm 日志接入 zap。
type gormLogBridge struct {
	lg *zap.Logger
}

func (b *gormLogBridge) Printf(format string, args ...interface{}) {
	b.lg.Sugar().Infof(format, args...)
}

// PingPostgres 用于健康检查。
func PingPostgres(db *gorm.DB) error {
	if db == nil {
		return errors.New("数据库未初始化")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
