package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestLogoutRejectsOtherInstancesWarmSessionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	redisServer := miniredis.RunT(t)
	cacheConfig := config.RedisConfig{Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "logout-integration",
		PoolSize: 2, MinIdleConns: 1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		TTL: time.Minute, AuthSessionCacheEnabled: true, SessionTTL: time.Minute}
	firstCache := querycache.New(cacheConfig)
	defer firstCache.Close()
	secondCache := querycache.New(cacheConfig)
	defer secondCache.Close()
	first := &Server{db: pool, cache: firstCache}
	second := &Server{db: pool, cache: secondCache}
	unique := fmt.Sprintf("logout_%d", time.Now().UnixNano())
	var userID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values($1,$2,'test',true,'active') returning id,public_id`, unique, unique+"@example.test").Scan(&userID, &publicID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from users where id=$1`, userID) })
	claims, err := security.NewClaims(publicID, unique, unique+"@example.test", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims.Subject = userID
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at) values($1,$2,1,$3)`,
		security.SessionFingerprint(claims.SessionID), userID, time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = first.resolveCachedSessionSubject(ctx, claims); err != nil {
		t.Fatal(err)
	}
	if _, err = second.resolveCachedSessionSubject(ctx, claims); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	first.logout(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = second.resolveCachedSessionSubject(ctx, claims); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another instance accepted logged-out warm session: %v", err)
	}
	// Recreate the cache entry left by a loader that completed after logout.
	// Cache payloads must never override the durable revocation record.
	stale, err := json.Marshal(cachedSessionSubject{SchemaVersion: authCacheSchemaVersion, UserID: userID,
		PublicID: publicID, AuthVersion: 1, ExpiresAt: claims.ExpiresAt})
	if err != nil {
		t.Fatal(err)
	}
	secondCache.Set(ctx, sessionCacheKey(claims.SessionID), stale, time.Minute)
	if _, err = second.resolveCachedSessionSubject(ctx, claims); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a late cache fill resurrected logged-out session: %v", err)
	}
}

func TestLogoutDatabaseFailureDoesNotReportSuccessIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	pool, err := pgxpool.New(context.Background(), config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	response := httptest.NewRecorder()
	server.logout(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("logout with unavailable database status=%d body=%s", response.Code, response.Body.String())
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("failed revocation discarded the client's retryable session cookie")
	}
}

func TestPasswordChangeRejectsWarmSessionWithoutCacheInvalidationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "password-revocation-no-invalidation",
		PoolSize: 2, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		AuthSessionCacheEnabled: true, SessionTTL: time.Minute, TTL: time.Minute})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	unique := fmt.Sprintf("revoke_no_cache_%d", time.Now().UnixNano())
	var userID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values($1,$2,'old-hash',true,'active') returning id,public_id`, unique, unique+"@example.test").Scan(&userID, &publicID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from users where id=$1`, userID) })
	claims, err := security.NewClaims(publicID, unique, unique+"@example.test", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at) values($1,$2,1,$3)`,
		security.SessionFingerprint(claims.SessionID), userID, time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.resolveCachedSessionSubject(ctx, claims); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update users set password_hash='new-hash' where id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.resolveCachedSessionSubject(ctx, claims); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("password change without cache invalidation kept session alive: %v", err)
	}
}
