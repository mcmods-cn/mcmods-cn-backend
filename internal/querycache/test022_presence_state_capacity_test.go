package querycache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestTEST022UserPresenceFallbackBoundsTotalSessionEntries(t *testing.T) {
	for _, manyUsers := range []bool{true, false} {
		t.Run(fmt.Sprintf("many-users-%v", manyUsers), func(t *testing.T) {
			cache := New(config.RedisConfig{})
			ctx := context.Background()
			now := time.Now().UTC()
			for index := range maxLocalPresenceEntries * 2 {
				userID := int64(1)
				if manyUsers {
					userID = int64(index + 1)
				}
				cache.TouchUserPresence(ctx, userID, fmt.Sprintf("session-%d", index), now.Add(time.Duration(index)), 150*time.Second)
			}
			cache.mu.Lock()
			entries := 0
			for _, sessions := range cache.userPresence {
				entries += len(sessions)
			}
			cache.mu.Unlock()
			if entries != maxLocalPresenceEntries {
				t.Fatalf("user fallback session entries=%d want global cap=%d", entries, maxLocalPresenceEntries)
			}
			cache.TouchUserPresence(ctx, 100000, "latest-a", now.Add(time.Second), 150*time.Second)
			cache.TouchUserPresence(ctx, 100000, "latest-b", now.Add(time.Second), 150*time.Second)
			cache.RemoveUserPresence(ctx, 100000, "latest-a", now.Add(time.Second), 150*time.Second)
			if !cache.UsersOnline(ctx, []int64{100000}, now.Add(time.Second), 150*time.Second)[100000] {
				t.Fatal("removing one session erased the newest remaining session")
			}
			cache.RemoveUserPresence(ctx, 100000, "latest-b", now.Add(time.Second), 150*time.Second)
			if cache.UsersOnline(ctx, []int64{100000}, now.Add(time.Second), 150*time.Second)[100000] {
				t.Fatal("removing all sessions left the user online")
			}
			if cache.UsersOnline(ctx, []int64{1, 8192}, now.Add(151*time.Second), 150*time.Second)[1] {
				t.Fatal("expired user remained online")
			}
			cache.mu.Lock()
			remaining := len(cache.userPresence)
			cache.mu.Unlock()
			if remaining != 0 {
				t.Fatalf("expired fallback users=%d", remaining)
			}
		})
	}
}

// These exercise the production fallback cache, not an authenticated HTTP load
// generator. The full HTTP tests separately verify membership and the 30s TTL.
func TestTEST022ConcurrentChatPresenceFallbackHasHardCapacity(t *testing.T) {
	cache := New(config.RedisConfig{})
	ctx := context.Background()
	var group sync.WaitGroup
	for worker := range 16 {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for index := range 512 {
				userID := int64(worker*512 + index + 1)
				cache.TouchChatPresence(ctx, userID, userID+1, 30*time.Second)
			}
		}(worker)
	}
	group.Wait()
	cache.mu.Lock()
	got := len(cache.chatPresence)
	cache.mu.Unlock()
	if got != maxLocalPresenceEntries {
		t.Fatalf("chat fallback entries=%d want hard cap=%d", got, maxLocalPresenceEntries)
	}
	cache.TouchChatPresence(ctx, 100000, 200000, 30*time.Second)
	if conversationID, online := cache.ChatPresence(ctx, 100000); !online || conversationID != 200000 {
		t.Fatalf("newest admitted state missing: conversation=%d online=%v", conversationID, online)
	}
}

func TestTEST022ExpiredChatPresenceIsReclaimedWithoutReadingOldUser(t *testing.T) {
	cache := New(config.RedisConfig{})
	ctx := context.Background()
	cache.TouchChatPresence(ctx, 1, 2, 20*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	cache.TouchChatPresence(ctx, 3, 4, 30*time.Second)
	cache.mu.Lock()
	_, oldRetained := cache.chatPresence[1]
	got := len(cache.chatPresence)
	cache.mu.Unlock()
	if oldRetained || got != 1 {
		t.Fatalf("expired state retained without a reader: old=%v total=%d", oldRetained, got)
	}
	if _, online := cache.ChatPresence(ctx, 1); online {
		t.Fatal("expired old user became online")
	}
	if conversationID, online := cache.ChatPresence(ctx, 3); !online || conversationID != 4 {
		t.Fatalf("fresh state missing: conversation=%d online=%v", conversationID, online)
	}
}
