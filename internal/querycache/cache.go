package querycache

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"mcmods-cn-backend/internal/config"
)

const maxLocalEntries = 512

type localEntry struct {
	value     []byte
	expiresAt time.Time
}

type Cache struct {
	redis    *redis.Client
	prefix   string
	ttl      time.Duration
	mu       sync.Mutex
	local    map[string]localEntry
	presence map[string]time.Time
	group    singleflight.Group
}

func New(cfg config.RedisConfig) *Cache {
	cache := &Cache{
		prefix: cfg.Prefix, ttl: cfg.TTL,
		local: make(map[string]localEntry), presence: make(map[string]time.Time),
	}
	if cache.ttl <= 0 {
		cache.ttl = 2 * time.Minute
	}
	if cfg.Enabled {
		cache.redis = redis.NewClient(&redis.Options{
			Addr: cfg.Addr, Username: cfg.Username, Password: cfg.Password, DB: cfg.DB,
			DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second,
		})
	}
	return cache
}

// TouchPresence records an opaque visitor fingerprint in the short-lived
// online set. Redis provides a shared view across replicas; the bounded local
// set is both the single-process implementation and a graceful Redis fallback.
func (c *Cache) TouchPresence(ctx context.Context, visitor string, now time.Time) {
	if c == nil || visitor == "" {
		return
	}
	c.mu.Lock()
	c.presence[visitor] = now
	c.prunePresenceLocked(now.Add(-5 * time.Minute))
	c.mu.Unlock()
	if c.redis == nil {
		return
	}
	key := c.prefix + "presence:online"
	pipeline := c.redis.Pipeline()
	pipeline.ZAdd(ctx, key, redis.Z{Score: float64(now.Unix()), Member: visitor})
	pipeline.ZRemRangeByScore(ctx, key, "-inf", formatUnix(now.Add(-5*time.Minute).Unix()))
	pipeline.Expire(ctx, key, 10*time.Minute)
	_, _ = pipeline.Exec(ctx)
}

// OnlinePresenceCount returns visitors active during the preceding five
// minutes. Redis failures fall back to the local process without failing the
// administration dashboard.
func (c *Cache) OnlinePresenceCount(ctx context.Context, now time.Time) int64 {
	if c == nil {
		return 0
	}
	cutoff := now.Add(-5 * time.Minute)
	if c.redis != nil {
		key := c.prefix + "presence:online"
		pipeline := c.redis.Pipeline()
		pipeline.ZRemRangeByScore(ctx, key, "-inf", formatUnix(cutoff.Unix()))
		count := pipeline.ZCount(ctx, key, formatUnix(cutoff.Unix()), "+inf")
		if _, err := pipeline.Exec(ctx); err == nil {
			return count.Val()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prunePresenceLocked(cutoff)
	return int64(len(c.presence))
}

func (c *Cache) prunePresenceLocked(cutoff time.Time) {
	for visitor, seenAt := range c.presence {
		if seenAt.Before(cutoff) {
			delete(c.presence, visitor)
		}
	}
}

func formatUnix(value int64) string {
	return strconv.FormatInt(value, 10)
}

func (c *Cache) GetOrLoad(ctx context.Context, key string, loader func(context.Context) ([]byte, error)) ([]byte, error) {
	if value, ok := c.getLocal(key); ok {
		return value, nil
	}
	if c.redis != nil {
		if value, err := c.redis.Get(ctx, c.prefix+key).Bytes(); err == nil {
			c.setLocal(key, value)
			return value, nil
		}
	}
	loaded, err, _ := c.group.Do(key, func() (any, error) {
		if value, ok := c.getLocal(key); ok {
			return value, nil
		}
		value, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		c.setLocal(key, value)
		if c.redis != nil {
			_ = c.redis.Set(context.Background(), c.prefix+key, value, c.ttl).Err()
		}
		return value, nil
	})
	if err != nil {
		return nil, err
	}
	return loaded.([]byte), nil
}

func (c *Cache) InvalidatePrefix(ctx context.Context, prefix string) {
	c.mu.Lock()
	for key := range c.local {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.local, key)
		}
	}
	c.mu.Unlock()
	if c.redis == nil {
		return
	}
	var cursor uint64
	for {
		keys, next, err := c.redis.Scan(ctx, cursor, c.prefix+prefix+"*", 100).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = c.redis.Del(ctx, keys...).Err()
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}

func (c *Cache) getLocal(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.local[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(c.local, key)
		return nil, false
	}
	return append([]byte(nil), entry.value...), true
}

func (c *Cache) setLocal(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.local) >= maxLocalEntries {
		now := time.Now()
		for existingKey, entry := range c.local {
			if now.After(entry.expiresAt) {
				delete(c.local, existingKey)
			}
		}
		if len(c.local) >= maxLocalEntries {
			for existingKey := range c.local {
				delete(c.local, existingKey)
				break
			}
		}
	}
	c.local[key] = localEntry{value: append([]byte(nil), value...), expiresAt: time.Now().Add(c.ttl)}
}
