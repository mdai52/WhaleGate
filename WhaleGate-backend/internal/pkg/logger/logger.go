// Package logger 基于 zap 提供结构化日志与文件滚动能力。
package logger

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/whalegate/whalegate/internal/config"
)

// New 按配置构建 zap.Logger。
func New(cfg config.LogConfig) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("解析日志级别失败: %w", err)
	}

	encoder := newEncoder(cfg.Format)
	var sink zapcore.WriteSyncer
	if cfg.Output == "file" {
		if err := os.MkdirAll(filepath.Dir(cfg.File.Path), 0o755); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %w", err)
		}
		sink = zapcore.AddSync(&lumberjack.Logger{
			Filename:   cfg.File.Path,
			MaxSize:    cfg.File.MaxSize,
			MaxBackups: cfg.File.MaxBackups,
			MaxAge:     cfg.File.MaxAge,
			Compress:   cfg.File.Compress,
		})
	} else {
		sink = zapcore.AddSync(os.Stdout)
	}

	stackLevel, err := zapcore.ParseLevel(cfg.Stacktrace)
	if err != nil {
		stackLevel = zapcore.ErrorLevel
	}

	core := zapcore.NewCore(encoder, sink, level)
	opts := []zap.Option{zap.AddStacktrace(stackLevel)}
	if cfg.Caller {
		opts = append(opts, zap.AddCaller(), zap.AddCallerSkip(0))
	}
	return zap.New(core, opts...), nil
}

func newEncoder(format string) zapcore.Encoder {
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "time"
	encCfg.LevelKey = "level"
	encCfg.NameKey = "logger"
	encCfg.MessageKey = "msg"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
	encCfg.EncodeDuration = zapcore.SecondsDurationEncoder

	// 中文环境下保持人类可读，不转义非 ASCII 字符。
	encCfg.ConsoleSeparator = " "
	if format == "console" || format == "text" {
		encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		return zapcore.NewConsoleEncoder(encCfg)
	}
	return zapcore.NewJSONEncoder(encCfg)
}

// NewNop 返回一个静默 logger，便于测试。
func NewNop() *zap.Logger {
	return zap.NewNop()
}
