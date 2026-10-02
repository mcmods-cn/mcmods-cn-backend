package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02CRealtimeSessionRevocationStopsEstablishedStreamIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.JWTSecret = "oct02-c-realtime-test-only-signing-secret"
	cfg.SettingsEncryptionKey = "oct02-c-test-only-setting-encryption"
	cfg.AntiAbuse.Enabled = false
	cfg.NATS.Enabled = false
	redis := miniredis.RunT(t)
	cfg.Redis.Enabled = true
	cfg.Redis.Addr, cfg.Redis.Password = redis.Addr(), ""
	cfg.Redis.Prefix, cfg.Redis.Namespace = "oct02-c", "realtime-revocation"
	cfg.Redis.AuthSessionCacheEnabled = true
	server := NewServer(ctx, cfg, pool, nil, nil, nil, nil)
	t.Cleanup(func() {
		shutdownCtx, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		_ = server.Shutdown(shutdownCtx)
		_ = server.cache.Close()
	})
	origin := httptest.NewServer(server)
	t.Cleanup(origin.Close)
	userID, _, token := createTEST044User(t, ctx, pool, cfg, "oct02-c-realtime", "UTC")
	stream := openTEST023Stream(t, ctx, origin, token)
	server.publishRealtimeUser(userID, "message.created", map[string]string{"text": "active session"})
	if frame := stream.receive(t); frame.kind != "message.created" {
		t.Fatalf("active session frame=%+v", frame)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.URL+"/api/v1/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: token})
	request.Header.Set("Origin", cfg.FrontendOrigin)
	response, err := origin.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("actual logout status=%d", response.StatusCode)
	}
	server.publishRealtimeUser(userID, "message.created", map[string]string{"text": "must stay private after logout"})
	select {
	case frame := <-stream.frames:
		t.Fatalf("revoked session received private frame=%+v", frame)
	case <-stream.done:
	case <-time.After(2 * time.Second):
		t.Fatal("revoked session stream did not close")
	}
	if count, _ := server.realtime.connectionCount(); count != 0 {
		t.Fatalf("revoked session retained subscriptions=%d", count)
	}
	// A direct heartbeat check must also reject committed security changes when
	// their cache publication is lost. Keep a real, warmed session cache here.
	for name, query := range map[string]string{
		"revoked":            `update auth_sessions set revoked_at=now() where user_id=$1`,
		"expired":            `update auth_sessions set expires_at=now()-interval '1 second' where user_id=$1`,
		"disabled":           `update users set status='disabled' where id=$1`,
		"credential-version": `update users set auth_version=auth_version+1 where id=$1`,
	} {
		t.Run(name, func(t *testing.T) {
			id, _, signed := createTEST044User(t, ctx, pool, cfg, "oct02-c-"+name, "UTC")
			claims, parseErr := security.ParseToken(cfg.JWTSecret, signed)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			claims.Subject = id
			if !server.realtimeSessionValid(ctx, claims, false) || !server.realtimeSessionValid(ctx, claims, true) {
				t.Fatal("active session failed event or heartbeat authorization")
			}
			if _, changeErr := pool.Exec(ctx, query, id); changeErr != nil {
				t.Fatal(changeErr)
			}
			if server.realtimeSessionValid(ctx, claims, true) {
				t.Fatal("heartbeat trusted stale cached session")
			}
		})
	}
}
