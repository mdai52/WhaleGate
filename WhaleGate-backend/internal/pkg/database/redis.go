package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/config"
)

// NewRedis 建立 Redis 客户端并验证连通性。
func NewRedis(cfg config.RedisConfig, lg *zap.Logger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), cfg.DialTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("连接 Redis 失败: %w", err)
	}
	if lg != nil {
		lg.Info("Redis 已连接", zap.String("addr", client.Options().Addr), zap.Int("db", cfg.DB))
	}
	return client, nil
}

// PingRedis 用于健康检查。
func PingRedis(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return fmt.Errorf("Redis 未初始化")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}
