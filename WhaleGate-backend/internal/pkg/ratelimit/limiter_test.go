package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestLimiter(t *testing.T) *Limiter {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return New(rdb, "test")
}

func TestAllowWithinLimit(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		res, err := l.Allow(ctx, "k1", 5, time.Minute)
		if err != nil {
			t.Fatalf("第 %d 次限流判定出错: %v", i, err)
		}
		if !res.Allowed {
			t.Fatalf("第 %d 次不应被限流", i)
		}
	}
}

func TestAllowExceedLimit(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()

	var last Result
	for i := 0; i < 6; i++ {
		res, err := l.Allow(ctx, "k2", 5, time.Minute)
		if err != nil {
			t.Fatalf("限流判定出错: %v", err)
		}
		last = res
	}
	if last.Allowed {
		t.Fatal("超出配额后应被拒绝")
	}
	if last.Used != 5 {
		t.Fatalf("窗口内计数错误: %d", last.Used)
	}
}

func TestAllowUnlimited(t *testing.T) {
	l := newTestLimiter(t)
	for i := 0; i < 10; i++ {
		res, err := l.Allow(context.Background(), "k3", 0, time.Minute)
		if err != nil || !res.Allowed {
			t.Fatalf("limit<=0 时应放行: %v", err)
		}
	}
}

func TestSlidingWindowRecovers(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	l := New(rdb, "test")
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := l.Allow(ctx, "k4", 2, 200*time.Millisecond); err != nil {
			t.Fatalf("限流判定出错: %v", err)
		}
	}
	if res, _ := l.Allow(ctx, "k4", 2, 200*time.Millisecond); res.Allowed {
		t.Fatal("第 3 次应被拒绝")
	}

	mr.FastForward(300 * time.Millisecond)
	if res, _ := l.Allow(ctx, "k4", 2, 200*time.Millisecond); !res.Allowed {
		t.Fatal("窗口滑出后应恢复放行")
	}
}

func TestConcurrencyAcquireRelease(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()

	release, err := l.Acquire(ctx, "c1", 1, time.Minute)
	if err != nil {
		t.Fatalf("首次占用失败: %v", err)
	}
	if _, err := l.Acquire(ctx, "c1", 1, time.Minute); err != ErrConcurrencyExceeded {
		t.Fatalf("第二次占用应超限，实际 %v", err)
	}
	release()
	if _, err := l.Acquire(ctx, "c1", 1, time.Minute); err != nil {
		t.Fatalf("释放后应可再次占用: %v", err)
	}
}

func TestConcurrencyUnlimited(t *testing.T) {
	l := newTestLimiter(t)
	for i := 0; i < 5; i++ {
		release, err := l.Acquire(context.Background(), "c2", 0, time.Minute)
		if err != nil {
			t.Fatalf("limit<=0 时不应限制: %v", err)
		}
		release()
	}
}
