package querycache

import (
	"context"
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
	redis  *redis.Client
	prefix string
	ttl    time.Duration
	mu     sync.Mutex
	local  map[string]localEntry
	group  singleflight.Group
}

func New(cfg config.RedisConfig) *Cache {
	cache := &Cache{prefix: cfg.Prefix, ttl: cfg.TTL, local: make(map[string]localEntry)}
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
