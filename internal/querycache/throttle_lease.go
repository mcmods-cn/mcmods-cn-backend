package querycache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// ThrottleLease permits a caller to release a failed attempt without deleting
// another caller's claim after expiry, eviction or concurrent replacement.
// Successful callers leave the reservation in place until its normal expiry.
type ThrottleLease struct {
	cache *Cache
	key   string
	token string
}

func (c *Cache) ClaimThrottleLease(ctx context.Context, key string, window time.Duration) (*ThrottleLease, bool, error) {
	if c == nil || key == "" || window <= 0 {
		return &ThrottleLease{}, true, nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, false, err
	}
	token := hex.EncodeToString(random[:])
	now := time.Now()
	lease := &ThrottleLease{cache: c, key: key, token: token}
	if redisKey, valid := c.redisKey("throttle:" + key); c.redis != nil && valid {
		started := time.Now()
		claimed, err := c.redis.SetNX(ctx, redisKey, token, window).Result()
		c.recordRedisResult(started, err)
		if err == nil {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.pruneClaimsLocked(now)
			if claimed {
				c.storeThrottleLeaseLocked(key, token, now.Add(window))
				return lease, true, nil
			}
			// Preserve an in-flight local owner's token when a duplicate request
			// observes the same shared reservation.
			if expiry, exists := c.claims[key]; !exists || !now.Before(expiry) {
				c.storeThrottleLeaseLocked(key, "", now.Add(window))
			}
			return nil, false, nil
		}
	}
	c.metrics.localFallbacks.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneClaimsLocked(now)
	if expiry, exists := c.claims[key]; exists && now.Before(expiry) {
		return nil, false, nil
	}
	c.storeThrottleLeaseLocked(key, token, now.Add(window))
	return lease, true, nil
}

func (c *Cache) storeThrottleLeaseLocked(key, token string, expiry time.Time) {
	if _, exists := c.claims[key]; !exists && len(c.claims) >= maxLocalClaims {
		for existing := range c.claims {
			delete(c.claims, existing)
			delete(c.claimOwners, existing)
			break
		}
	}
	c.claims[key] = expiry
	c.claimOwners[key] = token
}

func (lease *ThrottleLease) Release(ctx context.Context) error {
	if lease == nil || lease.cache == nil {
		return nil
	}
	c := lease.cache
	c.mu.Lock()
	if c.claimOwners[lease.key] == lease.token {
		delete(c.claims, lease.key)
		delete(c.claimOwners, lease.key)
	}
	c.mu.Unlock()
	if key, valid := c.redisKey("throttle:" + lease.key); c.redis != nil && valid {
		started := time.Now()
		_, err := c.redis.Eval(ctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{key}, lease.token).Result()
		c.recordRedisResult(started, err)
		return err
	}
	return nil
}
