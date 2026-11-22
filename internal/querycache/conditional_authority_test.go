package querycache

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestLookupOnlyResultDoesNotAuthorizeWaitingCaller(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := New(config.RedisConfig{TTL: time.Minute})
		defer cache.Close()
		started, release := make(chan struct{}), make(chan struct{})
		first, second := make(chan string, 1), make(chan string, 1)
		go func() {
			value, err := cache.GetOrLoadCacheableTTL(t.Context(), "expiring-rule", time.Minute, func(context.Context) ([]byte, bool, error) {
				close(started)
				<-release
				return []byte("allowed-before-deadline"), false, nil
			})
			if err != nil {
				t.Error(err)
			}
			first <- string(value)
		}()
		<-started
		// The second call starts after the source deadline while the old
		// read is still in progress. Wait until both calls are blocked.
		go func() {
			value, err := cache.GetOrLoadCacheableTTL(t.Context(), "expiring-rule", time.Minute, func(context.Context) ([]byte, bool, error) {
				return []byte("denied-after-deadline"), true, nil
			})
			if err != nil {
				t.Error(err)
			}
			second <- string(value)
		}()
		synctest.Wait()
		close(release)
		if a, b := <-first, <-second; a != "allowed-before-deadline" || b != "denied-after-deadline" {
			t.Fatalf("old lookup=%s, waiting caller=%s", a, b)
		}
	})
}
