package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func TestOCT03PresenceMountedHTTPRejectsClientIdentityAndSourceBudgetBypass(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared-%v", shared), func(t *testing.T) {
			if shared && os.Getenv("MCMODS_RUN_REDIS_INTEGRATION") != "1" {
				t.Skip("requires explicitly owned Redis and MCMODS_RUN_REDIS_INTEGRATION=1")
			}
			f := newTEST013Fixture(t)
			cfg := f.origin.Config.Handler.(*Server).cfg
			cfg.AntiAbuse.HMACSecret = "oct03-owned-presence-test-signing-secret-at-least-32-bytes"
			cfg.Redis = config.Load().Redis
			cfg.Redis.Enabled = shared
			if shared {
				host, _, err := net.SplitHostPort(cfg.Redis.Addr)
				if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
					t.Fatal("requires an explicitly configured loopback Redis server")
				}
			}
			var nonce [16]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				t.Fatal(err)
			}
			cfg.Redis.Namespace = "oct03-presence-http-" + hex.EncodeToString(nonce[:])
			cfg.Redis.AuthSessionCacheEnabled, cfg.Redis.RBACCacheEnabled = false, false
			cfg.Redis.RateLimitFailClosed = false
			var servers []*Server
			var origins []*httptest.Server
			replicas := 1
			limit := 12
			if shared {
				replicas, limit = 2, 60
			}
			for range replicas {
				q := queue.New(f.ctx, config.NATSConfig{})
				t.Cleanup(q.Close)
				s := NewServer(f.ctx, cfg, f.db, q, nil, nil, nil)
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := s.Shutdown(ctx); err != nil {
						t.Error(err)
					}
					_ = s.cache.Close()
				})
				if shared {
					if err := s.cache.Ping(f.ctx); err != nil {
						t.Fatal("owned Redis health check:", err)
					}
				}
				origin := httptest.NewServer(s)
				t.Cleanup(origin.Close)
				servers = append(servers, s)
				origins = append(origins, origin)
			}
			request := func(index int, body, ua string, want int) []byte {
				t.Helper()
				origin := origins[index%replicas]
				r, err := http.NewRequestWithContext(f.ctx, http.MethodPost, origin.URL+"/api/v1/site/presence", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("User-Agent", ua)
				// Untrusted client headers cannot turn one network source into many.
				r.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", index+1))
				response, err := origin.Client().Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				raw, err := io.ReadAll(io.LimitReader(response.Body, 4096))
				if err != nil || response.StatusCode != want {
					t.Fatalf("heartbeat %d status=%d want=%d err=%v", index, response.StatusCode, want, err)
				}
				if want == http.StatusTooManyRequests && (response.Header.Get("Retry-After") == "" || !bytes.Contains(raw, []byte(`"code":"PRESENCE_RATE_LIMIT"`))) {
					t.Fatal("presence denial lacks stable code or retry guidance")
				}
				return raw
			}
			var identity string
			for index := range limit {
				ua := "  OCT03   Browser  "
				if index%2 == 1 {
					ua = "oct03 browser"
				}
				raw := request(index, fmt.Sprintf(`{"visitorId":"attacker-%d"}`, index), ua, http.StatusOK)
				var response struct {
					Data struct {
						Online    bool   `json:"online"`
						VisitorID string `json:"visitorId"`
					} `json:"data"`
				}
				if err := json.Unmarshal(raw, &response); err != nil || !response.Data.Online || !strings.HasPrefix(response.Data.VisitorID, "p1.") {
					t.Fatal("heartbeat did not return a server-derived opaque identity")
				}
				if index == 0 {
					identity = response.Data.VisitorID
				} else if response.Data.VisitorID != identity {
					t.Fatal("arbitrary client IDs, forwarded headers or equivalent user agents multiplied one visitor")
				}
			}
			// Same IP, new UA and client ID still share the source budget. Admission
			// runs before parsing, so a malformed extra body must also be 429.
			request(limit, `{"visitorId":"another"}`, "different browser", http.StatusTooManyRequests)
			request(limit+1, `{`, "another browser", http.StatusTooManyRequests)
			for _, s := range servers {
				if count := s.cache.OnlinePresenceCount(f.ctx, time.Now().UTC()); count != 1 {
					t.Fatalf("client-controlled identity inflated mounted HTTP count=%d", count)
				}
			}
		})
	}
}
