package cache

import (
	"testing"
	"time"
)

func TestTTLCacheExpire(t *testing.T) {
	c := NewTTLCache(20*time.Millisecond, 0)
	defer c.Close()

	c.Set("k", "v")
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Fatal("写入后应能读取")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("过期条目不应可读")
	}
}

func TestTTLCacheDelete(t *testing.T) {
	c := NewTTLCache(time.Minute, 0)
	defer c.Close()
	c.Set("k", 1)
	c.Delete("k")
	if _, ok := c.Get("k"); ok {
		t.Fatal("删除后不应可读")
	}
}

func TestTTLCacheSetWithTTL(t *testing.T) {
	c := NewTTLCache(time.Hour, 0)
	defer c.Close()
	c.SetWithTTL("short", "v", 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if _, ok := c.Get("short"); ok {
		t.Fatal("独立 TTL 未生效")
	}
}

func TestTTLCacheSharding(t *testing.T) {
	c := NewTTLCache(time.Minute, 1000)
	defer c.Close()
	for i := 0; i < 500; i++ {
		c.Set(string(rune('a'+i%26))+itoa(i), i)
	}
	if c.Len() != 500 {
		t.Fatalf("条目数错误: %d", c.Len())
	}
}

func TestTTLCacheCapacity(t *testing.T) {
	c := NewTTLCache(time.Minute, 64)
	defer c.Close()
	for i := 0; i < 10000; i++ {
		c.Set(itoa(i), i)
	}
	// 32 分片，每片上限 maxSize/32+1 = 3，故总上限为 96。
	if c.Len() > 96 {
		t.Fatalf("容量未受限: %d", c.Len())
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf []byte
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	return string(buf)
}
