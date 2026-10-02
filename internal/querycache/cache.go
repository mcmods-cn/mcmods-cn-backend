package querycache

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"mcmods-cn-backend/internal/config"
)

const (
	maxLocalEntries          = 512
	maxLocalClaims           = 4096
	maxLocalLimits           = 8192
	maxLocalPresenceEntries  = 4096
	maxSharedPresenceEntries = 50_000
)

type localEntry struct {
	value     []byte
	expiresAt time.Time
}

type localLimit struct {
	count     int
	expiresAt time.Time
}

type localRealtimeLease struct {
	userID    int64
	sessionID string
	expiresAt time.Time
}

type RealtimeLease struct {
	cache     *Cache
	id        string
	userID    int64
	sessionID string
	ttl       time.Duration
	shared    bool
	released  atomic.Bool
}

type unreadEntry struct {
	notifications int64
	messages      int64
	epoch         int64
	expiresAt     time.Time
}

type chatPresenceEntry struct {
	conversationID int64
	expiresAt      time.Time
}

type RateLimitResult struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
	Backend    string
}

type Metrics struct {
	Requests            uint64        `json:"requests"`
	Hits                uint64        `json:"hits"`
	Misses              uint64        `json:"misses"`
	RedisHits           uint64        `json:"redisHits"`
	RedisMisses         uint64        `json:"redisMisses"`
	CacheHitRate        float64       `json:"cacheHitRate"`
	RedisHitRate        float64       `json:"redisHitRate"`
	Errors              uint64        `json:"errors"`
	Timeouts            uint64        `json:"timeouts"`
	LocalFallbacks      uint64        `json:"localFallbacks"`
	PostgresLoads       uint64        `json:"postgresLoads"`
	RateLimitFalls      uint64        `json:"rateLimitFallbacks"`
	RateLimitFailClosed uint64        `json:"rateLimitFailClosed"`
	PresenceWrites      uint64        `json:"presenceWrites"`
	UnreadRebuilds      uint64        `json:"unreadRebuilds"`
	UnreadCalibrations  uint64        `json:"unreadCalibrations"`
	UnreadDrifts        uint64        `json:"unreadDrifts"`
	AverageLatency      time.Duration `json:"averageLatency"`
	P95Latency          time.Duration `json:"p95Latency"`
}

type metricCounters struct {
	requests, hits, misses, errors, timeouts                         atomic.Uint64
	redisHits, redisMisses                                           atomic.Uint64
	localFallbacks, postgresLoads, rateLimitFallbacks                atomic.Uint64
	rateLimitFailClosed                                              atomic.Uint64
	presenceWrites, unreadRebuilds, unreadCalibrations, unreadDrifts atomic.Uint64
	latencyTotal                                                     atomic.Uint64
}

type Cache struct {
	redis              *redis.Client
	cfg                config.RedisConfig
	prefix             string
	ttl                time.Duration
	mu                 sync.Mutex
	local              map[string]localEntry
	presence           map[string]time.Time
	userPresence       map[int64]map[string]time.Time
	chatPresence       map[int64]chatPresenceEntry
	unread             map[int64]unreadEntry
	unreadEpoch        int64
	unreadSampleCursor int64
	claims             map[string]time.Time
	limits             map[string]localLimit
	realtimeLeases     map[string]localRealtimeLease
	group              singleflight.Group
	metrics            metricCounters
	latencyMu          sync.Mutex
	latencies          []time.Duration
	latencyIndex       int
}

func UserPermissionVersionKey(userID int64) string {
	return "authz:user-version:" + strconv.FormatInt(userID, 10)
}

func New(cfg config.RedisConfig) *Cache {
	basePrefix := strings.Trim(strings.TrimSpace(cfg.Prefix), ":")
	if basePrefix == "" {
		basePrefix = "mcmods"
	}
	namespace := strings.Trim(strings.TrimSpace(cfg.Namespace), ":")
	if namespace == "" {
		namespace = "development"
	}
	cache := &Cache{
		cfg: cfg, prefix: basePrefix + ":" + namespace + ":", ttl: cfg.TTL,
		local: make(map[string]localEntry), presence: make(map[string]time.Time), userPresence: make(map[int64]map[string]time.Time),
		chatPresence: make(map[int64]chatPresenceEntry), unread: make(map[int64]unreadEntry), claims: make(map[string]time.Time),
		limits: make(map[string]localLimit), realtimeLeases: make(map[string]localRealtimeLease), latencies: make([]time.Duration, 256),
	}
	if cache.ttl <= 0 {
		cache.ttl = 2 * time.Minute
	}
	if cfg.Enabled {
		cache.redis = redis.NewClient(&redis.Options{
			Addr: cfg.Addr, Username: cfg.Username, Password: cfg.Password, DB: cfg.DB,
			PoolSize: cfg.PoolSize, MinIdleConns: cfg.MinIdleConns,
			DialTimeout: cfg.DialTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout,
		})
	}
	return cache
}

func (c *Cache) Enabled() bool { return c != nil && c.redis != nil }

func (c *Cache) Config() config.RedisConfig {
	if c == nil {
		return config.RedisConfig{}
	}
	return c.cfg
}

func (c *Cache) Close() error {
	if c == nil || c.redis == nil {
		return nil
	}
	return c.redis.Close()
}

func (c *Cache) Ping(ctx context.Context) error {
	if c == nil || c.redis == nil {
		return errors.New("redis disabled")
	}
	started := time.Now()
	err := c.redis.Ping(ctx).Err()
	c.recordRedisResult(started, err)
	return err
}

func (c *Cache) Metrics() Metrics {
	if c == nil {
		return Metrics{}
	}
	requests := c.metrics.requests.Load()
	result := Metrics{
		Requests: requests, Hits: c.metrics.hits.Load(), Misses: c.metrics.misses.Load(),
		RedisHits: c.metrics.redisHits.Load(), RedisMisses: c.metrics.redisMisses.Load(),
		Errors: c.metrics.errors.Load(), Timeouts: c.metrics.timeouts.Load(),
		LocalFallbacks: c.metrics.localFallbacks.Load(), PostgresLoads: c.metrics.postgresLoads.Load(),
		RateLimitFalls: c.metrics.rateLimitFallbacks.Load(), PresenceWrites: c.metrics.presenceWrites.Load(),
		RateLimitFailClosed: c.metrics.rateLimitFailClosed.Load(),
		UnreadRebuilds:      c.metrics.unreadRebuilds.Load(),
		UnreadCalibrations:  c.metrics.unreadCalibrations.Load(), UnreadDrifts: c.metrics.unreadDrifts.Load(),
	}
	if decisions := result.Hits + result.Misses; decisions > 0 {
		result.CacheHitRate = float64(result.Hits) / float64(decisions)
	}
	if redisDecisions := result.RedisHits + result.RedisMisses; redisDecisions > 0 {
		result.RedisHitRate = float64(result.RedisHits) / float64(redisDecisions)
	}
	if requests > 0 {
		result.AverageLatency = time.Duration(c.metrics.latencyTotal.Load() / requests)
	}
	c.latencyMu.Lock()
	values := make([]time.Duration, 0, len(c.latencies))
	for _, value := range c.latencies {
		if value > 0 {
			values = append(values, value)
		}
	}
	c.latencyMu.Unlock()
	if len(values) > 0 {
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		result.P95Latency = values[(len(values)-1)*95/100]
	}
	return result
}

func (c *Cache) recordRedisResult(started time.Time, err error) {
	if c == nil {
		return
	}
	elapsed := time.Since(started)
	c.metrics.requests.Add(1)
	c.metrics.latencyTotal.Add(uint64(elapsed))
	c.latencyMu.Lock()
	c.latencies[c.latencyIndex%len(c.latencies)] = elapsed
	c.latencyIndex++
	c.latencyMu.Unlock()
	if errors.Is(err, redis.Nil) {
		return
	}
	if err != nil {
		c.metrics.errors.Add(1)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, redis.ErrClosed) {
			c.metrics.timeouts.Add(1)
		}
	}
}

func (c *Cache) redisKey(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 512 {
		return "", false
	}
	return c.prefix + key, true
}

// ConsumeRateLimit atomically consumes one slot from a bounded fixed window.
// Redis keeps the decision shared between replicas. A bounded local counter is
// used for single-process development. Multi-replica deployments set
// RateLimitFailClosed, which rejects requests during a Redis outage rather than
// multiplying the quota by the number of replicas. Callers can include Backend
// in internal diagnostics without exposing it to clients.
func (c *Cache) ConsumeRateLimit(ctx context.Context, key string, limit int, window time.Duration) RateLimitResult {
	return c.ConsumeRateLimitPolicy(ctx, key, limit, limit, window)
}

// ConsumeRateLimitPolicy uses a deliberately smaller localFallbackLimit for
// single-replica deployments when shared Redis state is unavailable. A cache
// configured with RateLimitFailClosed never enters that local branch.
func (c *Cache) ConsumeRateLimitPolicy(ctx context.Context, key string, redisLimit, localFallbackLimit int, window time.Duration) RateLimitResult {
	if c == nil || key == "" || redisLimit <= 0 || window <= 0 {
		return RateLimitResult{Allowed: true, Remaining: max(redisLimit-1, 0), Backend: "disabled"}
	}
	if localFallbackLimit <= 0 || localFallbackLimit > redisLimit {
		localFallbackLimit = redisLimit
	}
	if redisKey, valid := c.redisKey("limit:" + key); c.redis != nil && valid {
		started := time.Now()
		result, err := c.redis.Eval(ctx, `
local count=redis.call('INCR',KEYS[1])
if count==1 then redis.call('PEXPIRE',KEYS[1],ARGV[2]) end
local ttl=redis.call('PTTL',KEYS[1])
return {count,ttl}
`, []string{redisKey}, redisLimit, window.Milliseconds()).Int64Slice()
		c.recordRedisResult(started, err)
		if err == nil && len(result) == 2 {
			count := int(result[0])
			retry := time.Duration(max(result[1], 0)) * time.Millisecond
			return RateLimitResult{
				Allowed: count <= redisLimit, Remaining: max(redisLimit-count, 0), RetryAfter: retry, Backend: "redis",
			}
		}
	}
	if c.cfg.RateLimitFailClosed {
		c.metrics.rateLimitFailClosed.Add(1)
		return RateLimitResult{Allowed: false, RetryAfter: min(window, 5*time.Second), Backend: "unavailable"}
	}
	c.metrics.localFallbacks.Add(1)
	c.metrics.rateLimitFallbacks.Add(1)
	return c.consumeLocalRateLimit(key, localFallbackLimit, window)
}

func (c *Cache) consumeLocalRateLimit(key string, limit int, window time.Duration) RateLimitResult {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, exists := c.limits[key]
	if !exists || !now.Before(entry.expiresAt) {
		entry = localLimit{expiresAt: now.Add(window)}
	}
	entry.count++
	if len(c.limits) >= maxLocalLimits && !exists {
		for existingKey, existing := range c.limits {
			if !now.Before(existing.expiresAt) {
				delete(c.limits, existingKey)
			}
		}
		if len(c.limits) >= maxLocalLimits {
			for existingKey := range c.limits {
				delete(c.limits, existingKey)
				break
			}
		}
	}
	c.limits[key] = entry
	return RateLimitResult{
		Allowed: entry.count <= limit, Remaining: max(limit-entry.count, 0),
		RetryAfter: max(entry.expiresAt.Sub(now), 0), Backend: "local",
	}
}

func (c *Cache) ConsumeLocalRateLimit(key string, limit int, window time.Duration) RateLimitResult {
	if c == nil {
		return RateLimitResult{Allowed: false, Backend: "unavailable"}
	}
	c.metrics.localFallbacks.Add(1)
	c.metrics.rateLimitFallbacks.Add(1)
	return c.consumeLocalRateLimit(key, limit, window)
}

// ClaimThrottle returns true once per key and window. Redis SET NX gives all
// application replicas the same decision; the bounded local map is the
// single-process implementation and the fallback when Redis is unavailable.
func (c *Cache) ClaimThrottle(ctx context.Context, key string, window time.Duration) bool {
	if c == nil || key == "" || window <= 0 {
		return true
	}
	now := time.Now()
	if redisKey, valid := c.redisKey("throttle:" + key); c.redis != nil && valid {
		started := time.Now()
		claimed, err := c.redis.SetNX(ctx, redisKey, "1", window).Result()
		c.recordRedisResult(started, err)
		if err == nil {
			c.recordLocalClaim(key, now.Add(window), now)
			return claimed
		}
	}
	c.metrics.localFallbacks.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if expiresAt, exists := c.claims[key]; exists && now.Before(expiresAt) {
		return false
	}
	c.pruneClaimsLocked(now)
	if len(c.claims) >= maxLocalClaims {
		for existingKey := range c.claims {
			delete(c.claims, existingKey)
			break
		}
	}
	c.claims[key] = now.Add(window)
	return true
}

func (c *Cache) recordLocalClaim(key string, expiresAt, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneClaimsLocked(now)
	if len(c.claims) >= maxLocalClaims {
		for existingKey := range c.claims {
			delete(c.claims, existingKey)
			break
		}
	}
	c.claims[key] = expiresAt
}

func (c *Cache) pruneClaimsLocked(now time.Time) {
	for key, expiresAt := range c.claims {
		if !now.Before(expiresAt) {
			delete(c.claims, key)
		}
	}
}

// AcquireRealtimeLease atomically applies deployment-wide total, user and
// session connection budgets when Redis is available. The bounded local lease
// table is the exact single-process implementation and the fail-safe fallback.
func (c *Cache) AcquireRealtimeLease(ctx context.Context, userID int64, sessionID string, totalLimit, userLimit, sessionLimit int, ttl time.Duration) (*RealtimeLease, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if c == nil || userID <= 0 || sessionID == "" || len(sessionID) > 128 || totalLimit <= 0 || userLimit <= 0 || sessionLimit <= 0 || ttl <= 0 {
		return nil, false
	}
	lease := &RealtimeLease{
		cache: c, id: newRealtimeLeaseID(), userID: userID, sessionID: sessionID, ttl: ttl,
	}
	if allKey, allOK := c.redisKey("realtime:leases:all"); c.redis != nil && allOK {
		userKey, userOK := c.redisKey("realtime:leases:user:" + strconv.FormatInt(userID, 10))
		sessionKey, sessionOK := c.redisKey("realtime:leases:session:" + sessionID)
		if userOK && sessionOK {
			now := time.Now().UnixMilli()
			expiresAt := now + ttl.Milliseconds()
			started := time.Now()
			allowed, err := c.redis.Eval(ctx, `
for index,key in ipairs(KEYS) do redis.call('ZREMRANGEBYSCORE',key,'-inf',ARGV[1]) end
if redis.call('ZCARD',KEYS[1])>=tonumber(ARGV[4]) then return 0 end
if redis.call('ZCARD',KEYS[2])>=tonumber(ARGV[5]) then return 0 end
if redis.call('ZCARD',KEYS[3])>=tonumber(ARGV[6]) then return 0 end
for index,key in ipairs(KEYS) do
 redis.call('ZADD',key,ARGV[2],ARGV[3])
 redis.call('PEXPIRE',key,ARGV[7])
end
return 1
`, []string{allKey, userKey, sessionKey}, now, expiresAt, lease.id, totalLimit, userLimit, sessionLimit, max(ttl.Milliseconds()*2, int64(1))).Int()
			c.recordRedisResult(started, err)
			if err == nil {
				if allowed != 1 {
					return nil, false
				}
				lease.shared = true
				return lease, true
			}
			c.metrics.localFallbacks.Add(1)
		}
	}
	return c.acquireLocalRealtimeLease(lease, totalLimit, userLimit, sessionLimit)
}

func (c *Cache) acquireLocalRealtimeLease(lease *RealtimeLease, totalLimit, userLimit, sessionLimit int) (*RealtimeLease, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, existing := range c.realtimeLeases {
		if !now.Before(existing.expiresAt) {
			delete(c.realtimeLeases, id)
		}
	}
	if len(c.realtimeLeases) >= totalLimit {
		return nil, false
	}
	userCount, sessionCount := 0, 0
	for _, existing := range c.realtimeLeases {
		if existing.userID == lease.userID {
			userCount++
		}
		if existing.sessionID == lease.sessionID {
			sessionCount++
		}
	}
	if userCount >= userLimit || sessionCount >= sessionLimit {
		return nil, false
	}
	c.realtimeLeases[lease.id] = localRealtimeLease{userID: lease.userID, sessionID: lease.sessionID, expiresAt: now.Add(lease.ttl)}
	return lease, true
}

func (lease *RealtimeLease) Refresh(ctx context.Context) bool {
	if lease == nil || lease.cache == nil || lease.released.Load() {
		return false
	}
	if lease.shared {
		allKey, allOK := lease.cache.redisKey("realtime:leases:all")
		userKey, userOK := lease.cache.redisKey("realtime:leases:user:" + strconv.FormatInt(lease.userID, 10))
		sessionKey, sessionOK := lease.cache.redisKey("realtime:leases:session:" + lease.sessionID)
		if !allOK || !userOK || !sessionOK || lease.cache.redis == nil {
			return false
		}
		expiresAt := time.Now().Add(lease.ttl).UnixMilli()
		started := time.Now()
		refreshed, err := lease.cache.redis.Eval(ctx, `
for index,key in ipairs(KEYS) do if not redis.call('ZSCORE',key,ARGV[1]) then return 0 end end
for index,key in ipairs(KEYS) do redis.call('ZADD',key,'XX',ARGV[2],ARGV[1]); redis.call('PEXPIRE',key,ARGV[3]) end
return 1
`, []string{allKey, userKey, sessionKey}, lease.id, expiresAt, max(lease.ttl.Milliseconds()*2, int64(1))).Int()
		lease.cache.recordRedisResult(started, err)
		return err == nil && refreshed == 1
	}
	now := time.Now()
	lease.cache.mu.Lock()
	defer lease.cache.mu.Unlock()
	existing, ok := lease.cache.realtimeLeases[lease.id]
	if !ok || !now.Before(existing.expiresAt) {
		delete(lease.cache.realtimeLeases, lease.id)
		return false
	}
	existing.expiresAt = now.Add(lease.ttl)
	lease.cache.realtimeLeases[lease.id] = existing
	return true
}

func (lease *RealtimeLease) Release(ctx context.Context) {
	if lease == nil || lease.cache == nil || !lease.released.CompareAndSwap(false, true) {
		return
	}
	if lease.shared && lease.cache.redis != nil {
		allKey, allOK := lease.cache.redisKey("realtime:leases:all")
		userKey, userOK := lease.cache.redisKey("realtime:leases:user:" + strconv.FormatInt(lease.userID, 10))
		sessionKey, sessionOK := lease.cache.redisKey("realtime:leases:session:" + lease.sessionID)
		if allOK && userOK && sessionOK {
			if ctx == nil || ctx.Err() != nil {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				defer cancel()
			}
			started := time.Now()
			err := lease.cache.redis.ZRem(ctx, allKey, lease.id).Err()
			if err == nil {
				_, err = lease.cache.redis.Pipelined(ctx, func(pipeline redis.Pipeliner) error {
					pipeline.ZRem(ctx, userKey, lease.id)
					pipeline.ZRem(ctx, sessionKey, lease.id)
					return nil
				})
			}
			lease.cache.recordRedisResult(started, err)
		}
		return
	}
	lease.cache.mu.Lock()
	delete(lease.cache.realtimeLeases, lease.id)
	lease.cache.mu.Unlock()
}

func newRealtimeLeaseID() string {
	var random [16]byte
	if _, err := rand.Read(random[:]); err == nil {
		return fmt.Sprintf("%x", random[:])
	}
	return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
}

// TouchPresence records an opaque visitor fingerprint in the short-lived
// online set. Redis provides a shared view across replicas; the bounded local
// set is both the single-process implementation and a graceful Redis fallback.
func (c *Cache) TouchPresence(ctx context.Context, visitor string, now time.Time) {
	if c == nil || visitor == "" {
		return
	}
	c.mu.Lock()
	c.prunePresenceLocked(now.Add(-5 * time.Minute))
	if _, exists := c.presence[visitor]; !exists && len(c.presence) >= maxLocalPresenceEntries {
		oldestKey := ""
		var oldest time.Time
		for key, seenAt := range c.presence {
			if oldestKey == "" || seenAt.Before(oldest) {
				oldestKey, oldest = key, seenAt
			}
		}
		delete(c.presence, oldestKey)
	}
	c.presence[visitor] = now
	c.mu.Unlock()
	if c.redis == nil {
		return
	}
	key := c.prefix + "presence:online"
	started := time.Now()
	err := c.redis.Eval(ctx, `
redis.call('ZADD',KEYS[1],ARGV[1],ARGV[2])
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ARGV[3])
local count=redis.call('ZCARD',KEYS[1])
local maximum=tonumber(ARGV[4])
if count>maximum then redis.call('ZREMRANGEBYRANK',KEYS[1],0,count-maximum-1) end
redis.call('EXPIRE',KEYS[1],ARGV[5])
return 1
`, []string{key}, now.Unix(), visitor, now.Add(-5*time.Minute).Unix(), maxSharedPresenceEntries, int64((10*time.Minute)/time.Second)).Err()
	c.recordRedisResult(started, err)
	c.metrics.presenceWrites.Add(1)
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
	return c.GetOrLoadTTL(ctx, key, c.ttl, loader)
}

func (c *Cache) GetOrLoadTTL(ctx context.Context, key string, ttl time.Duration, loader func(context.Context) ([]byte, error)) ([]byte, error) {
	if ttl <= 0 {
		ttl = c.ttl
	}
	if value, ok := c.getLocal(key); ok {
		c.metrics.hits.Add(1)
		return value, nil
	}
	if redisKey, valid := c.redisKey(key); c.redis != nil && valid {
		started := time.Now()
		value, redisErr := c.redis.Get(ctx, redisKey).Bytes()
		c.recordRedisResult(started, redisErr)
		if redisErr == nil {
			c.metrics.redisHits.Add(1)
			c.metrics.hits.Add(1)
			c.setLocalTTL(key, value, ttl)
			return value, nil
		}
		if errors.Is(redisErr, redis.Nil) {
			c.metrics.redisMisses.Add(1)
		}
		if !errors.Is(redisErr, redis.Nil) {
			c.metrics.localFallbacks.Add(1)
		}
	}
	c.metrics.misses.Add(1)
	loaded, err, _ := c.group.Do(key, func() (any, error) {
		if value, ok := c.getLocal(key); ok {
			return value, nil
		}
		c.metrics.postgresLoads.Add(1)
		value, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		c.setLocalTTL(key, value, ttl)
		if redisKey, valid := c.redisKey(key); c.redis != nil && valid {
			started := time.Now()
			setErr := c.redis.Set(context.Background(), redisKey, value, ttl).Err()
			c.recordRedisResult(started, setErr)
		}
		return value, nil
	})
	if err != nil {
		return nil, err
	}
	return loaded.([]byte), nil
}

// GetSharedOrLoadTTL reads a small cross-instance coordination value without
// consulting the process-local cache. It is intended for authorization
// version pointers: a role grant written by one API instance must not be
// hidden by another instance's stale local entry. When Redis is disabled or
// unavailable the authoritative loader is used, so authorization fails closed
// to PostgreSQL rather than accepting an unverified cached version.
func (c *Cache) GetSharedOrLoadTTL(ctx context.Context, key string, ttl time.Duration, loader func(context.Context) ([]byte, error)) ([]byte, error) {
	if c == nil {
		return loader(ctx)
	}
	if ttl <= 0 {
		ttl = c.ttl
	}
	if redisKey, valid := c.redisKey(key); c.redis != nil && valid {
		started := time.Now()
		value, redisErr := c.redis.Get(ctx, redisKey).Bytes()
		c.recordRedisResult(started, redisErr)
		if redisErr == nil {
			c.metrics.redisHits.Add(1)
			c.metrics.hits.Add(1)
			return value, nil
		}
		if errors.Is(redisErr, redis.Nil) {
			c.metrics.redisMisses.Add(1)
		} else {
			c.metrics.localFallbacks.Add(1)
		}
	}
	c.metrics.misses.Add(1)
	loaded, err, _ := c.group.Do("shared:"+key, func() (any, error) {
		c.metrics.postgresLoads.Add(1)
		value, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		if redisKey, valid := c.redisKey(key); c.redis != nil && valid {
			started := time.Now()
			setErr := c.redis.Set(context.Background(), redisKey, value, ttl).Err()
			c.recordRedisResult(started, setErr)
		}
		return value, nil
	})
	if err != nil {
		return nil, err
	}
	return loaded.([]byte), nil
}

// SetShared updates Redis without creating a process-local copy. Readers of
// version pointers deliberately bypass local state so every instance observes
// the shared value on its next request.
func (c *Cache) SetShared(ctx context.Context, key string, value []byte, ttl time.Duration) bool {
	if c == nil || c.redis == nil {
		return false
	}
	if ttl <= 0 {
		ttl = c.ttl
	}
	redisKey, valid := c.redisKey(key)
	if !valid {
		return false
	}
	started := time.Now()
	err := c.redis.Set(ctx, redisKey, value, ttl).Err()
	c.recordRedisResult(started, err)
	return err == nil
}

func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	if value, ok := c.getLocal(key); ok {
		c.metrics.hits.Add(1)
		return value, true
	}
	redisKey, valid := c.redisKey(key)
	if c.redis == nil || !valid {
		c.metrics.misses.Add(1)
		return nil, false
	}
	started := time.Now()
	value, err := c.redis.Get(ctx, redisKey).Bytes()
	c.recordRedisResult(started, err)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			c.metrics.redisMisses.Add(1)
		}
		c.metrics.misses.Add(1)
		if !errors.Is(err, redis.Nil) {
			c.metrics.localFallbacks.Add(1)
		}
		return nil, false
	}
	c.metrics.redisHits.Add(1)
	c.metrics.hits.Add(1)
	c.setLocal(key, value)
	return value, true
}

func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) bool {
	if c == nil {
		return false
	}
	if ttl <= 0 {
		ttl = c.ttl
	}
	c.setLocalTTL(key, value, ttl)
	redisKey, valid := c.redisKey(key)
	if c.redis == nil || !valid {
		c.metrics.localFallbacks.Add(1)
		return false
	}
	started := time.Now()
	err := c.redis.Set(ctx, redisKey, value, ttl).Err()
	c.recordRedisResult(started, err)
	return err == nil
}

func (c *Cache) Delete(ctx context.Context, keys ...string) {
	_ = c.DeleteShared(ctx, keys...)
}

// DeleteShared removes exact coordination keys from both the local map and
// Redis and reports whether the cross-instance invalidation reached Redis.
// Callers that mutate authorization facts must not silently discard this
// result after their database transaction commits.
func (c *Cache) DeleteShared(ctx context.Context, keys ...string) error {
	if c == nil || len(keys) == 0 {
		return nil
	}
	c.mu.Lock()
	for _, key := range keys {
		delete(c.local, key)
	}
	c.mu.Unlock()
	if c.redis == nil {
		return nil
	}
	redisKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, valid := c.redisKey(key); valid {
			redisKeys = append(redisKeys, value)
		} else {
			return fmt.Errorf("invalid shared cache key %q", key)
		}
	}
	if len(redisKeys) == 0 {
		return nil
	}
	started := time.Now()
	err := c.redis.Del(ctx, redisKeys...).Err()
	c.recordRedisResult(started, err)
	return err
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
	c.setLocalTTL(key, value, c.ttl)
}

func (c *Cache) setLocalTTL(key string, value []byte, ttl time.Duration) {
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
	c.local[key] = localEntry{value: append([]byte(nil), value...), expiresAt: time.Now().Add(ttl)}
}
