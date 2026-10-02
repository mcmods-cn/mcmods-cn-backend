package querycache

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type UnreadSummary struct {
	Notifications int64 `json:"notifications"`
	Messages      int64 `json:"messages"`
}

func (summary UnreadSummary) Total() int64 { return summary.Notifications + summary.Messages }

func (c *Cache) TouchUserPresence(ctx context.Context, userID int64, sessionHash string, now time.Time, ttl time.Duration) {
	if c == nil || userID <= 0 || sessionHash == "" {
		return
	}
	if ttl <= 0 {
		ttl = c.cfg.PresenceTTL
	}
	if ttl <= 0 {
		ttl = 150 * time.Second
	}
	c.metrics.presenceWrites.Add(1)
	c.mu.Lock()
	entries := c.pruneUserPresenceLocked(now.Add(-ttl))
	if _, exists := c.userPresence[userID][sessionHash]; !exists && entries >= maxLocalPresenceEntries {
		// This is only the bounded, approximate local fallback. Redis remains
		// authoritative and authentication/session lifetime is not changed.
		var oldestUser int64
		var oldestSession string
		var oldest time.Time
		for candidateUser, candidates := range c.userPresence {
			for candidateSession, seenAt := range candidates {
				if oldestUser == 0 || seenAt.Before(oldest) {
					oldestUser, oldestSession, oldest = candidateUser, candidateSession, seenAt
				}
			}
		}
		delete(c.userPresence[oldestUser], oldestSession)
		if len(c.userPresence[oldestUser]) == 0 {
			delete(c.userPresence, oldestUser)
		}
	}
	sessions := c.userPresence[userID]
	if sessions == nil {
		sessions = make(map[string]time.Time)
		c.userPresence[userID] = sessions
	}
	sessions[sessionHash] = now
	c.mu.Unlock()
	if c.redis == nil || !c.cfg.PresenceEnabled {
		c.metrics.localFallbacks.Add(1)
		return
	}
	userKey, userValid := c.redisKey("presence:user:" + strconv.FormatInt(userID, 10) + ":sessions")
	globalKey, globalValid := c.redisKey("presence:users")
	if !userValid || !globalValid {
		return
	}
	started := time.Now()
	_, err := c.redis.Eval(ctx, `
redis.call('ZADD',KEYS[1],ARGV[1],ARGV[2])
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ARGV[3])
redis.call('PEXPIRE',KEYS[1],ARGV[4])
redis.call('ZADD',KEYS[2],ARGV[1],ARGV[5])
redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',ARGV[3])
redis.call('PEXPIRE',KEYS[2],ARGV[4])
return 1
`, []string{userKey, globalKey}, now.UnixMilli(), sessionHash, now.Add(-ttl).UnixMilli(), (ttl * 2).Milliseconds(), userID).Result()
	c.recordRedisResult(started, err)
}

func (c *Cache) RemoveUserPresence(ctx context.Context, userID int64, sessionHash string, now time.Time, ttl time.Duration) {
	if c == nil || userID <= 0 || sessionHash == "" {
		return
	}
	if ttl <= 0 {
		ttl = c.cfg.PresenceTTL
	}
	if ttl <= 0 {
		ttl = 150 * time.Second
	}
	c.mu.Lock()
	if sessions := c.userPresence[userID]; sessions != nil {
		delete(sessions, sessionHash)
		if len(sessions) == 0 {
			delete(c.userPresence, userID)
		}
	}
	c.mu.Unlock()
	if c.redis == nil || !c.cfg.PresenceEnabled {
		return
	}
	userKey, userValid := c.redisKey("presence:user:" + strconv.FormatInt(userID, 10) + ":sessions")
	globalKey, globalValid := c.redisKey("presence:users")
	if !userValid || !globalValid {
		return
	}
	started := time.Now()
	_, err := c.redis.Eval(ctx, `
redis.call('ZREM',KEYS[1],ARGV[1])
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ARGV[2])
local newest=redis.call('ZREVRANGE',KEYS[1],0,0,'WITHSCORES')
if #newest==0 then redis.call('ZREM',KEYS[2],ARGV[3]) else redis.call('ZADD',KEYS[2],newest[2],ARGV[3]) end
return #newest
`, []string{userKey, globalKey}, sessionHash, now.Add(-ttl).UnixMilli(), userID).Result()
	c.recordRedisResult(started, err)
}

func (c *Cache) UsersOnline(ctx context.Context, userIDs []int64, now time.Time, ttl time.Duration) map[int64]bool {
	result := make(map[int64]bool, len(userIDs))
	if c == nil || len(userIDs) == 0 {
		return result
	}
	if ttl <= 0 {
		ttl = c.cfg.PresenceTTL
	}
	if ttl <= 0 {
		ttl = 150 * time.Second
	}
	if c.redis != nil && c.cfg.PresenceEnabled {
		key, valid := c.redisKey("presence:users")
		if valid {
			members := make([]string, len(userIDs))
			for index, userID := range userIDs {
				members[index] = strconv.FormatInt(userID, 10)
			}
			started := time.Now()
			scores, err := c.redis.ZMScore(ctx, key, members...).Result()
			c.recordRedisResult(started, err)
			if err == nil {
				cutoff := float64(now.Add(-ttl).UnixMilli())
				for index, score := range scores {
					result[userIDs[index]] = score >= cutoff
				}
				return result
			}
		}
	}
	c.metrics.localFallbacks.Add(1)
	cutoff := now.Add(-ttl)
	c.mu.Lock()
	c.pruneUserPresenceLocked(cutoff)
	for _, userID := range userIDs {
		for _, seenAt := range c.userPresence[userID] {
			if !seenAt.Before(cutoff) {
				result[userID] = true
				break
			}
		}
	}
	c.mu.Unlock()
	return result
}

func (c *Cache) pruneUserPresenceLocked(cutoff time.Time) int {
	entries := 0
	for userID, sessions := range c.userPresence {
		for session, seenAt := range sessions {
			if seenAt.Before(cutoff) {
				delete(sessions, session)
			}
		}
		if len(sessions) == 0 {
			delete(c.userPresence, userID)
		}
		entries += len(sessions)
	}
	return entries
}

func (c *Cache) TouchChatPresence(ctx context.Context, userID, conversationID int64, ttl time.Duration) {
	if c == nil || userID <= 0 || conversationID <= 0 {
		return
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	now := time.Now()
	expiresAt := now.Add(ttl)
	c.mu.Lock()
	var oldestUser int64
	var oldestExpiry time.Time
	for candidateUser, entry := range c.chatPresence {
		if !now.Before(entry.expiresAt) {
			delete(c.chatPresence, candidateUser)
			continue
		}
		if oldestUser == 0 || entry.expiresAt.Before(oldestExpiry) {
			oldestUser, oldestExpiry = candidateUser, entry.expiresAt
		}
	}
	if _, exists := c.chatPresence[userID]; !exists && len(c.chatPresence) >= maxLocalPresenceEntries {
		delete(c.chatPresence, oldestUser)
	}
	c.chatPresence[userID] = chatPresenceEntry{conversationID: conversationID, expiresAt: expiresAt}
	c.mu.Unlock()
	if c.redis == nil || !c.cfg.PresenceEnabled {
		c.metrics.localFallbacks.Add(1)
		return
	}
	key, valid := c.redisKey("chat-presence:user:" + strconv.FormatInt(userID, 10))
	if !valid {
		return
	}
	started := time.Now()
	err := c.redis.Set(ctx, key, conversationID, ttl).Err()
	c.recordRedisResult(started, err)
}

func (c *Cache) ChatPresence(ctx context.Context, userID int64) (int64, bool) {
	if c == nil || userID <= 0 {
		return 0, false
	}
	if c.redis != nil && c.cfg.PresenceEnabled {
		key, valid := c.redisKey("chat-presence:user:" + strconv.FormatInt(userID, 10))
		if valid {
			started := time.Now()
			value, err := c.redis.Get(ctx, key).Int64()
			c.recordRedisResult(started, err)
			if err == nil {
				return value, true
			}
			if err != nil && !errors.Is(err, redis.Nil) {
				c.metrics.localFallbacks.Add(1)
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.chatPresence[userID]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(c.chatPresence, userID)
		return 0, false
	}
	return entry.conversationID, true
}

func (c *Cache) LoadUnread(ctx context.Context, userID int64, loader func(context.Context) (UnreadSummary, error)) (UnreadSummary, error) {
	if c == nil || userID <= 0 {
		return loader(ctx)
	}
	epoch := c.currentUnreadEpoch(ctx)
	now := time.Now()
	c.mu.Lock()
	if entry, ok := c.unread[userID]; ok && now.Before(entry.expiresAt) && entry.epoch == epoch {
		c.mu.Unlock()
		c.metrics.hits.Add(1)
		return UnreadSummary{Notifications: entry.notifications, Messages: entry.messages}, nil
	}
	c.mu.Unlock()
	key, valid := c.redisKey("unread:user:" + strconv.FormatInt(userID, 10))
	if c.redis != nil && c.cfg.UnreadCounterEnabled && valid {
		started := time.Now()
		fields, err := c.redis.HGetAll(ctx, key).Result()
		c.recordRedisResult(started, err)
		if err == nil && len(fields) > 0 {
			storedEpoch, epochErr := strconv.ParseInt(fields["epoch"], 10, 64)
			notifications, notificationErr := strconv.ParseInt(fields["notifications"], 10, 64)
			messages, messageErr := strconv.ParseInt(fields["messages"], 10, 64)
			if epochErr == nil && notificationErr == nil && messageErr == nil && storedEpoch == epoch {
				c.metrics.redisHits.Add(1)
				value := UnreadSummary{Notifications: max(notifications, 0), Messages: max(messages, 0)}
				c.storeLocalUnread(userID, value, epoch, c.cfg.UnreadTTL)
				c.metrics.hits.Add(1)
				return value, nil
			}
		}
		if err == nil && len(fields) == 0 {
			c.metrics.redisMisses.Add(1)
		}
	}
	c.metrics.misses.Add(1)
	c.metrics.postgresLoads.Add(1)
	c.metrics.unreadRebuilds.Add(1)
	value, err := loader(ctx)
	if err != nil {
		return UnreadSummary{}, err
	}
	value.Notifications = max(value.Notifications, 0)
	value.Messages = max(value.Messages, 0)
	c.SetUnread(ctx, userID, value, epoch)
	return value, nil
}

// ReconcileUnread compares a cached derivative with PostgreSQL truth and
// replaces drifted values. A cache miss is rebuilt but is not counted as drift.
func (c *Cache) ReconcileUnread(ctx context.Context, userID int64, truth UnreadSummary) (bool, error) {
	if c == nil || userID <= 0 {
		return false, nil
	}
	c.metrics.unreadCalibrations.Add(1)
	loadedTruth := false
	current, err := c.LoadUnread(ctx, userID, func(context.Context) (UnreadSummary, error) {
		loadedTruth = true
		return truth, nil
	})
	if err != nil || loadedTruth {
		return false, err
	}
	drifted := current.Notifications != truth.Notifications || current.Messages != truth.Messages
	if !drifted {
		return false, nil
	}
	c.metrics.unreadDrifts.Add(1)
	c.InvalidateUnread(ctx, userID)
	_, err = c.LoadUnread(ctx, userID, func(context.Context) (UnreadSummary, error) { return truth, nil })
	return true, err
}

// UnreadReconciliationCandidates returns a rotating, bounded sample of live
// derivatives that this process has actually served. Expired entries are not
// useful calibration targets and are removed instead of causing a PostgreSQL
// walk over every account in the system.
func (c *Cache) UnreadReconciliationCandidates(limit int) []int64 {
	if c == nil || limit <= 0 {
		return nil
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]int64, 0, min(limit, len(c.unread)))
	for userID, entry := range c.unread {
		if !now.Before(entry.expiresAt) {
			delete(c.unread, userID)
			continue
		}
		ids = append(ids, userID)
	}
	if len(ids) == 0 {
		c.unreadSampleCursor = 0
		return nil
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	start := sort.Search(len(ids), func(index int) bool { return ids[index] > c.unreadSampleCursor })
	if start == len(ids) {
		start = 0
	}
	count := min(limit, len(ids))
	sample := make([]int64, 0, count)
	for offset := range count {
		sample = append(sample, ids[(start+offset)%len(ids)])
	}
	c.unreadSampleCursor = sample[len(sample)-1]
	return sample
}

func (c *Cache) SetUnread(ctx context.Context, userID int64, value UnreadSummary, epoch int64) {
	if c == nil || userID <= 0 {
		return
	}
	if epoch < 0 {
		epoch = 0
	}
	ttl := c.cfg.UnreadTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	value.Notifications = max(value.Notifications, 0)
	value.Messages = max(value.Messages, 0)
	c.storeLocalUnread(userID, value, epoch, ttl)
	if c.redis == nil || !c.cfg.UnreadCounterEnabled {
		return
	}
	key, valid := c.redisKey("unread:user:" + strconv.FormatInt(userID, 10))
	if !valid {
		return
	}
	started := time.Now()
	pipe := c.redis.Pipeline()
	pipe.HSet(ctx, key, "notifications", value.Notifications, "messages", value.Messages, "epoch", epoch)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	c.recordRedisResult(started, err)
}

func (c *Cache) AdjustUnread(ctx context.Context, userID int64, field string, delta int64) {
	if c == nil || userID <= 0 || (field != "notifications" && field != "messages") || delta == 0 {
		return
	}
	epoch := c.currentUnreadEpoch(ctx)
	c.mu.Lock()
	if entry, ok := c.unread[userID]; ok && time.Now().Before(entry.expiresAt) && entry.epoch == epoch {
		if field == "notifications" {
			entry.notifications = max(entry.notifications+delta, 0)
		} else {
			entry.messages = max(entry.messages+delta, 0)
		}
		c.unread[userID] = entry
	}
	c.mu.Unlock()
	if c.redis == nil || !c.cfg.UnreadCounterEnabled {
		return
	}
	key, valid := c.redisKey("unread:user:" + strconv.FormatInt(userID, 10))
	if !valid {
		return
	}
	ttl := c.cfg.UnreadTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	started := time.Now()
	_, err := c.redis.Eval(ctx, `
if redis.call('EXISTS',KEYS[1])==0 then return -1 end
local value=redis.call('HINCRBY',KEYS[1],ARGV[1],ARGV[2])
if value<0 then value=0 redis.call('HSET',KEYS[1],ARGV[1],0) end
redis.call('HSET',KEYS[1],'epoch',ARGV[3])
redis.call('PEXPIRE',KEYS[1],ARGV[4])
return value
`, []string{key}, field, delta, epoch, ttl.Milliseconds()).Result()
	c.recordRedisResult(started, err)
}

func (c *Cache) InvalidateUnread(ctx context.Context, userID int64) {
	if c == nil || userID <= 0 {
		return
	}
	c.mu.Lock()
	delete(c.unread, userID)
	c.mu.Unlock()
	c.Delete(ctx, "unread:user:"+strconv.FormatInt(userID, 10))
}

func (c *Cache) BumpUnreadEpoch(ctx context.Context) int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	c.unreadEpoch++
	epoch := c.unreadEpoch
	c.mu.Unlock()
	if c.redis == nil || !c.cfg.UnreadCounterEnabled {
		return epoch
	}
	key, valid := c.redisKey("unread:global-epoch")
	if !valid {
		return epoch
	}
	started := time.Now()
	value, err := c.redis.Incr(ctx, key).Result()
	c.recordRedisResult(started, err)
	if err == nil {
		return value
	}
	return epoch
}

func (c *Cache) currentUnreadEpoch(ctx context.Context) int64 {
	if c.redis != nil && c.cfg.UnreadCounterEnabled {
		key, valid := c.redisKey("unread:global-epoch")
		if valid {
			started := time.Now()
			value, err := c.redis.Get(ctx, key).Int64()
			c.recordRedisResult(started, err)
			if err == nil {
				return value
			}
			if errors.Is(err, redis.Nil) {
				started = time.Now()
				setErr := c.redis.SetNX(ctx, key, 0, 0).Err()
				c.recordRedisResult(started, setErr)
				return 0
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unreadEpoch
}

func (c *Cache) storeLocalUnread(userID int64, value UnreadSummary, epoch int64, ttl time.Duration) {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.unread) >= maxLocalEntries {
		now := time.Now()
		for id, entry := range c.unread {
			if now.After(entry.expiresAt) {
				delete(c.unread, id)
			}
		}
		if len(c.unread) >= maxLocalEntries {
			for id := range c.unread {
				delete(c.unread, id)
				break
			}
		}
	}
	c.unread[userID] = unreadEntry{notifications: value.Notifications, messages: value.Messages, epoch: epoch, expiresAt: time.Now().Add(ttl)}
}
