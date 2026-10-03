package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

type oct02SessionLoadPause struct {
	loaded  chan struct{}
	release chan struct{}
	once    sync.Once
}

type oct02SessionLoadTraceKey struct{}

func (pause *oct02SessionLoadPause) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, oct02SessionLoadTraceKey{}, strings.Contains(data.SQL, "from auth_sessions session"))
}

func (pause *oct02SessionLoadPause) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if sessionLoad, _ := ctx.Value(oct02SessionLoadTraceKey{}).(bool); sessionLoad && data.Err == nil {
		pause.once.Do(func() {
			close(pause.loaded)
			<-pause.release
		})
	}
}

func TestOCT02LogoutDoesNotAllowInflightSessionCacheRepopulationIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `alter table users add column status text default 'active',add column auth_version bigint default 1;
		create table auth_sessions(session_hash bytea primary key,user_id bigint,auth_version bigint,expires_at timestamptz,revoked_at timestamptz);
		create table user_presence_sessions(session_hash bytea,user_id bigint)`); err != nil {
		t.Fatal(err)
	}
	redis := miniredis.RunT(t)
	cfg := config.RedisConfig{Enabled: true, Addr: redis.Addr(), Prefix: "fixture", Namespace: "oct02-inflight", PoolSize: 2, TTL: time.Minute,
		AuthSessionCacheEnabled: true, SessionTTL: time.Minute, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, PresenceTTL: time.Minute}
	logoutCache, loadingCache := querycache.New(cfg), querycache.New(cfg)
	defer logoutCache.Close()
	defer loadingCache.Close()
	claims, err := security.NewClaims("u00000042", "fixture", "fixture@example.invalid", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims.Subject = 42
	if _, err := pool.Exec(ctx, `insert into auth_sessions values($1,42,1,$2,null)`, security.SessionFingerprint(claims.SessionID), time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	pause := &oct02SessionLoadPause{loaded: make(chan struct{}), release: make(chan struct{})}
	pc := pool.Config()
	pc.ConnConfig.Tracer = pause
	loadingPool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer loadingPool.Close()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(pause.release) })
	loading := &Server{db: loadingPool, cache: loadingCache}
	done := make(chan error, 1)
	go func() {
		_, e := loading.resolveCachedSessionSubject(ctx, claims)
		done <- e
	}()
	select {
	case <-pause.loaded:
	case <-ctx.Done():
		t.Fatal("session loader did not reach completed database read")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil).WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	(&Server{db: pool, cache: logoutCache}).logout(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("logout=%d %s", response.Code, response.Body.String())
	}
	releaseOnce.Do(func() { close(pause.release) })
	if err := <-done; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("inflight loader accepted session after logout: %v", err)
	}
	if _, err := loading.resolveCachedSessionSubject(ctx, claims); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("late cache population accepted on next request: %v", err)
	}
}

func TestOCT02LogoutDoesNotClaimSuccessWhenRevocationFails(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://fixture:fixture@127.0.0.1:1/fixture?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 42, SessionID: "fixture-session"}))
	response := httptest.NewRecorder()
	(&Server{db: pool}).logout(response, request)
	if response.Code != http.StatusServiceUnavailable || len(response.Result().Cookies()) != 0 {
		t.Fatalf("failed logout status=%d cookies=%v", response.Code, response.Result().Cookies())
	}
}

func TestOCT02LogoutRevokesAcrossWarmAPIReplicasIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `alter table users add column status text default 'active',add column auth_version bigint default 1;
	 create table auth_sessions(session_hash bytea primary key,user_id bigint,auth_version bigint,expires_at timestamptz,revoked_at timestamptz);
	 create table user_presence_sessions(session_hash bytea,user_id bigint)`); err != nil {
		t.Fatal(err)
	}
	redis := miniredis.RunT(t)
	cfg := config.RedisConfig{Enabled: true, Addr: redis.Addr(), Prefix: "fixture", Namespace: "oct02-auth", PoolSize: 2, TTL: time.Minute,
		AuthSessionCacheEnabled: true, SessionTTL: time.Minute, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, PresenceTTL: time.Minute}
	firstCache, secondCache := querycache.New(cfg), querycache.New(cfg)
	defer firstCache.Close()
	defer secondCache.Close()
	first, second := &Server{db: pool, cache: firstCache}, &Server{db: pool, cache: secondCache}
	claims, err := security.NewClaims("u00000042", "fixture", "fixture@example.invalid", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims.Subject = 42
	if _, err = pool.Exec(ctx, `insert into auth_sessions values($1,42,1,$2,null)`, security.SessionFingerprint(claims.SessionID), time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = second.resolveCachedSessionSubject(ctx, claims); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil).WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	first.logout(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("logout=%d %s", response.Code, response.Body.String())
	}
	if _, err = second.resolveCachedSessionSubject(ctx, claims); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("warm second replica accepted revoked session: %v", err)
	}
	if len(response.Result().Cookies()) != 1 || response.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("successful revocation did not clear the session cookie")
	}
}

func TestOCT02LogoutRollsBackWhenPresenceDeletionFailsIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `create table auth_sessions(session_hash bytea primary key,user_id bigint,revoked_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	claims := security.Claims{Subject: 42, SessionID: "fixture-session"}
	if _, err := pool.Exec(ctx, `insert into auth_sessions values($1,42,null)`, security.SessionFingerprint(claims.SessionID)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil).WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	(&Server{db: pool}).logout(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("failed transaction=%d %s", response.Code, response.Body.String())
	}
	var revoked *time.Time
	if err := pool.QueryRow(ctx, `select revoked_at from auth_sessions`).Scan(&revoked); err != nil || revoked != nil {
		t.Fatalf("partial logout committed: revoked=%v error=%v", revoked, err)
	}
}
