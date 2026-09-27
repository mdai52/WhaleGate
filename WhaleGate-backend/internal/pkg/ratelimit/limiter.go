// Package ratelimit 基于 Redis 实现分布式滑动窗口限流与并发控制。
package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// slidingWindowScript 原子地维护 ZSET 滑动窗口：
// KEYS[1]=窗口键；ARGV[1]=当前毫秒时间戳；ARGV[2]=窗口毫秒；ARGV[3]=配额；ARGV[4]=唯一成员。
// 返回 {是否放行(0/1), 当前窗口内请求数}。
const slidingWindowScript = `
local key    = KEYS[1]
local now    = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit  = tonumber(ARGV[3])
local member = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local used = redis.call('ZCARD', key)
if used < limit then
  redis.call('ZADD', key, now, member)
  redis.call('PEXPIRE', key, window)
  return {1, used + 1}
end
return {0, used}
`

// Limiter 提供限流能力。
type Limiter struct {
	rdb    *redis.Client
	prefix string
	script *redis.Script
}

// New 创建限流器，prefix 用于隔离不同集群的键空间。
func New(rdb *redis.Client, prefix string) *Limiter {
	if prefix == "" {
		prefix = "wg"
	}
	return &Limiter{
		rdb:    rdb,
		prefix: prefix,
		script: redis.NewScript(slidingWindowScript),
	}
}

// Result 限流判定结果。
type Result struct {
	Allowed    bool
	Used       int
	Limit      int
	ResetAt    time.Time
	RetryAfter time.Duration
}

// Allow 判定一次请求是否放行。limit <= 0 表示不限流。
func (l *Limiter) Allow(ctx context.Context, name string, limit int, window time.Duration) (Result, error) {
	if limit <= 0 {
		return Result{Allowed: true, Limit: limit}, nil
	}
	if window <= 0 {
		window = time.Minute
	}

	now := time.Now()
	key := fmt.Sprintf("%s:rl:%s", l.prefix, name)
	member := strconv.FormatInt(now.UnixNano(), 10) + "-" + uuid.NewString()

	raw, err := l.script.Run(ctx, l.rdb, []string{key},
		now.UnixMilli(), int(window/time.Millisecond), limit, member).Result()
	if err != nil {
		return Result{}, fmt.Errorf("执行限流脚本失败: %w", err)
	}

	values, ok := raw.([]interface{})
	if !ok || len(values) < 2 {
		return Result{}, fmt.Errorf("限流脚本返回异常: %v", raw)
	}
	allowed, _ := strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
	used, _ := strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)

	return Result{
		Allowed:    allowed == 1,
		Used:       int(used),
		Limit:      limit,
		ResetAt:    now.Add(window),
		RetryAfter: window,
	}, nil
}

// Acquire 占用一个并发额度，返回释放函数。limit <= 0 表示不限制。
func (l *Limiter) Acquire(ctx context.Context, name string, limit int, ttl time.Duration) (func(), error) {
	if limit <= 0 {
		return func() {}, nil
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	key := fmt.Sprintf("%s:conc:%s", l.prefix, name)

	pipe := l.rdb.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.PExpire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("占用并发额度失败: %w", err)
	}
	n, err := incr.Result()
	if err != nil {
		return nil, fmt.Errorf("读取并发计数失败: %w", err)
	}
	if n > int64(limit) {
		pipe := l.rdb.Pipeline()
		pipe.Decr(ctx, key)
		pipe.PExpire(ctx, key, ttl)
		_, _ = pipe.Exec(ctx)
		return nil, ErrConcurrencyExceeded
	}

	var released bool
	return func() {
		if released {
			return
		}
		released = true
		pipe := l.rdb.Pipeline()
		pipe.Decr(ctx, key)
		pipe.PExpire(ctx, key, ttl)
		_, _ = pipe.Exec(context.Background())
	}, nil
}
