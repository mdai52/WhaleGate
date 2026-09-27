// Package cache 提供进程内 TTL 缓存，用于降低 Redis 访问压力（如 API Key 解析）。
package cache

import (
	"sync"
	"time"
)

const defaultShards = 32

type entry struct {
	value    interface{}
	expireAt int64
}

type shard struct {
	mu      sync.RWMutex
	items   map[string]entry
	maxSize int
}

// TTLCache 是分片的高并发 TTL 缓存。
type TTLCache struct {
	shards   []*shard
	ttl      time.Duration
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewTTLCache 创建缓存，ttl 为条目的固定存活时间，maxSize 为总容量上限（0 表示不限）。
func NewTTLCache(ttl time.Duration, maxSize int) *TTLCache {
	per := 0
	if maxSize > 0 {
		per = maxSize/defaultShards + 1
	}
	c := &TTLCache{
		shards: make([]*shard, defaultShards),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	for i := range c.shards {
		c.shards[i] = &shard{items: make(map[string]entry), maxSize: per}
	}
	go c.cleanup()
	return c
}

func (c *TTLCache) shardOf(key string) *shard {
	return c.shards[hashString(key)%uint32(len(c.shards))]
}

// Get 读取未过期的条目。
func (c *TTLCache) Get(key string) (interface{}, bool) {
	s := c.shardOf(key)
	now := time.Now().UnixNano()

	s.mu.RLock()
	e, ok := s.items[key]
	s.mu.RUnlock()
	if !ok || e.expireAt <= now {
		return nil, false
	}
	return e.value, true
}

// Set 写入条目。
func (c *TTLCache) Set(key string, value interface{}) {
	s := c.shardOf(key)
	expireAt := time.Now().Add(c.ttl).UnixNano()

	s.mu.Lock()
	if s.maxSize > 0 && len(s.items) >= s.maxSize {
		// 容量不足时先清理过期项，仍不足则整片清空，保证写入成功。
		now := time.Now().UnixNano()
		for k, v := range s.items {
			if v.expireAt <= now {
				delete(s.items, k)
			}
		}
		if len(s.items) >= s.maxSize {
			s.items = make(map[string]entry, s.maxSize)
		}
	}
	s.items[key] = entry{value: value, expireAt: expireAt}
	s.mu.Unlock()
}

// SetWithTTL 写入带独立过期时间的条目。
func (c *TTLCache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	s := c.shardOf(key)
	s.mu.Lock()
	s.items[key] = entry{value: value, expireAt: time.Now().Add(ttl).UnixNano()}
	s.mu.Unlock()
}

// Delete 删除条目。
func (c *TTLCache) Delete(key string) {
	s := c.shardOf(key)
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()
}

// Len 返回当前条目总数（含未清理的过期项）。
func (c *TTLCache) Len() int {
	var n int
	for _, s := range c.shards {
		s.mu.RLock()
		n += len(s.items)
		s.mu.RUnlock()
	}
	return n
}

// Close 停止后台清理协程。
func (c *TTLCache) Close() {
	c.stopOnce.Do(func() { close(c.stopCh) })
}

func (c *TTLCache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			now := time.Now().UnixNano()
			for _, s := range c.shards {
				s.mu.Lock()
				for k, v := range s.items {
					if v.expireAt <= now {
						delete(s.items, k)
					}
				}
				s.mu.Unlock()
			}
		}
	}
}

// hashString 使用 FNV-1a，避免引入额外依赖。
func hashString(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	var h uint32 = offset32
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}
