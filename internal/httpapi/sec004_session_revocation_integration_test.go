package httpapi

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestSEC004CommittedStatusChangeFailsClosedWhenVersionPublicationFailsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify committed session revocation cache failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "sec004-session",
		PoolSize: 2, MinIdleConns: 0,
		DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond,
		AuthSessionCacheEnabled: true, SessionTTL: time.Minute,
	})
	t.Cleanup(func() { _ = cache.Close() })
	server := &Server{db: pool, cache: cache}

	var operatorID, targetID, authVersion int64
	var targetPublicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values('sec004_operator','sec004-operator@example.invalid','test-only',true,'active') returning id`).Scan(&operatorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values('sec004_target','sec004-target@example.invalid','test-only',true,'active')
		returning id,public_id,auth_version`).Scan(&targetID, &targetPublicID, &authVersion); err != nil {
		t.Fatal(err)
	}
	targetClaims, err := security.NewClaims(targetPublicID, "sec004_target", "sec004-target@example.invalid", authVersion, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		values($1,$2,$3,$4)`, security.SessionFingerprint(targetClaims.SessionID), targetID, authVersion, time.Unix(targetClaims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	server.cacheIssuedSession(ctx, targetClaims, targetID)
	warmed := targetClaims
	if err = server.resolveClaimsSubject(ctx, &warmed); err != nil {
		t.Fatalf("warm active session: %v", err)
	}
	if err = server.refreshAuthVersionForIdentity(ctx, targetID+999, targetPublicID); err == nil {
		t.Fatal("missing authoritative row did not fail the auth-version refresh")
	}
	if redisServer.Exists("mcmods:sec004-session:" + userAuthVersionCacheKey(targetPublicID)) {
		t.Fatal("stale auth-version pointer survived the refresh query failure")
	}
	server.cacheIssuedSession(ctx, targetClaims, targetID)
	redisServer.Close()

	request := httptest.NewRequest("PUT", "/api/v1/admin/users/"+targetPublicID+"/status", strings.NewReader(`{"status":"disabled"}`)).WithContext(ctx)
	request.SetPathValue("id", targetPublicID)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: operatorID}))
	response := httptest.NewRecorder()
	server.updateUserStatus(response, request)
	if response.Code != 503 || !strings.Contains(response.Body.String(), `"code":"SECURITY_VERSION_REFRESH_FAILED"`) ||
		!strings.Contains(response.Body.String(), `"committed":true`) {
		t.Fatalf("cache publication failure status=%d body=%s", response.Code, response.Body.String())
	}
	var status string
	var afterAuthVersion int64
	if err = pool.QueryRow(ctx, `select status,auth_version from users where id=$1`, targetID).Scan(&status, &afterAuthVersion); err != nil {
		t.Fatal(err)
	}
	if status != "disabled" || afterAuthVersion <= authVersion {
		t.Fatalf("committed security fact status=%s authVersion=%d initial=%d", status, afterAuthVersion, authVersion)
	}
	stale := targetClaims
	if err = server.resolveClaimsSubject(ctx, &stale); err == nil {
		t.Fatal("stale session remained usable after the committed disabled status")
	}
	if got := server.securityVersionRefreshFailures.Load(); got != 1 {
		t.Fatalf("security version refresh failure metric=%d, want 1", got)
	}
}
