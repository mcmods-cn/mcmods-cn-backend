package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func securityTestRedis(t *testing.T) config.RedisConfig {
	t.Helper()
	cfg := config.Load().Redis
	if os.Getenv("MCMODS_RUN_REDIS_INTEGRATION") == "1" {
		host, _, err := net.SplitHostPort(cfg.Addr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			t.Fatal("this test requires its owned loopback Redis")
		}
	} else {
		cfg.Addr = miniredis.RunT(t).Addr()
		cfg.Username, cfg.Password, cfg.DB = "", "", 0
	}
	cfg.Enabled, cfg.AuthSessionCacheEnabled, cfg.RBACCacheEnabled = true, true, true
	cfg.Namespace = "authority-" + randomHex(10)
	cfg.SessionTTL, cfg.RBACCacheTTL = time.Minute, time.Minute
	return cfg
}

func securityTestReplica(t *testing.T, ctx context.Context, cfg config.Config, db *pgxpool.Pool, tracer pgx.QueryTracer) *Server {
	t.Helper()
	pc := db.Config()
	pc.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cache := querycache.New(cfg.Redis)
	t.Cleanup(func() { _ = cache.Close() })
	if err := cache.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return &Server{db: pool, cache: cache, cfg: cfg}
}

// An earlier refresh reads an old permission version and returns after another
// API publishes a revocation. All three HTTP checks below are separate calls.
func TestSecurityVersionDelayedWriterAndColdRedisIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db := newOCT02IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.Redis = securityTestRedis(t)
	a, b := securityTestReplica(t, ctx, cfg, db, nil), securityTestReplica(t, ctx, cfg, db, nil)
	id, _, token := createTEST044User(t, ctx, db, cfg, "delayed-version", "UTC")
	const permission = "fixture.security.read"
	grantTEST044Permissions(t, ctx, db, id, permission)
	next := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	gate := b.requirePermission(permission, next)
	check := func(want int, label string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/fixture", nil).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		gate(w, r)
		if w.Code != want {
			t.Errorf("%s: got HTTP %d, want %d", label, w.Code, want)
		}
	}
	check(http.StatusNoContent, "warm grant")
	key := querycache.UserPermissionVersionKey(id)
	loaded, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- a.refreshSharedVersionPointer(ctx, key, func() (int64, error) {
			var value int64
			err := db.QueryRow(ctx, `select permission_version from users where id=$1`, id).Scan(&value)
			close(loaded)
			select {
			case <-release:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			return value, err
		})
	}()
	select {
	case <-loaded:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := db.Exec(ctx, `delete from user_permissions where user_id=$1 and permission_id=(select id from permissions where code=$2)`, id, permission); err != nil {
		t.Fatal(err)
	}
	if err := b.refreshPermissionVersion(ctx, id); err != nil {
		t.Fatal(err)
	}
	check(http.StatusForbidden, "committed revoke")
	// Evict precisely this run's hint, just as on expiration or a cold Redis.
	if err := b.cache.DeleteShared(ctx, key); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	check(http.StatusForbidden, "late old result on a cold version key")
}

func TestSecurityVersionWarmRequestCostIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db := newOCT02IsolatedDatabase(t, ctx)
	counter := &integrationQueryCounter{}
	cfg := config.Load()
	cfg.Redis = securityTestRedis(t)
	s := securityTestReplica(t, ctx, cfg, db, counter)
	_, _, token := createTEST044User(t, ctx, db, cfg, "warm-request", "UTC")
	claims, err := security.ParseToken(cfg.JWTSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	for range 12 {
		copy := claims
		if err = s.resolveClaimsSubject(ctx, &copy); err != nil {
			t.Fatal(err)
		}
	}
	counter.queries.Store(0)
	const iterations = 300
	latencies := make([]time.Duration, 0, iterations)
	for range iterations {
		copy := claims
		start := time.Now()
		if err = s.resolveClaimsSubject(ctx, &copy); err != nil {
			t.Fatal(err)
		}
		latencies = append(latencies, time.Since(start))
	}
	slices.Sort(latencies)
	t.Logf("warm calls=%d SQL=%d median_us=%d p95_us=%d", iterations, counter.queries.Load(), latencies[iterations/2].Microseconds(), latencies[iterations*95/100].Microseconds())
}

// The writer loses its Redis connection, but another API keeps its warm, fully
// reachable Redis connection. The committed PostgreSQL state must decide.
func TestSecurityVersionCommittedPropagationFailureAcrossReplicasIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db := newOCT02IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.Redis = securityTestRedis(t)
	a, b := securityTestReplica(t, ctx, cfg, db, nil), securityTestReplica(t, ctx, cfg, db, nil)
	manager, _, admin := createTEST044User(t, ctx, db, cfg, "asymmetric-manager", "UTC")
	target, publicID, token := createTEST044User(t, ctx, db, cfg, "asymmetric-member", "UTC")
	grantTEST044Permissions(t, ctx, db, manager, "permission.write")
	const permission = "fixture.private.read"
	grantTEST044Permissions(t, ctx, db, target, permission)
	identity, err := security.ParseToken(cfg.JWTSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	other, err := security.NewClaims(publicID, identity.Username, identity.Email, identity.AuthVersion, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at) values($1,$2,$3,$4)`, security.SessionFingerprint(other.SessionID), target, other.AuthVersion, time.Unix(other.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	sibling, err := security.SignToken(cfg.JWTSecret, other)
	if err != nil {
		t.Fatal(err)
	}
	next := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	view, auth := b.requirePermission(permission, next), b.requireAuth(next)
	check := func(label string, gate http.HandlerFunc, token string, want int) {
		t.Helper()
		if got := performTEST044Request(ctx, gate, token, http.MethodGet, "/fixture", "", nil).Code; got != want {
			t.Errorf("%s: HTTP %d, want %d", label, got, want)
		}
	}
	check("warm grant", view, token, http.StatusNoContent)
	check("warm second session", auth, sibling, http.StatusNoContent)
	put := a.requirePermission("permission.write", a.updateUserPermissions)
	logout := a.requireAuth(a.logout)
	if err := a.cache.Close(); err != nil {
		t.Fatal(err)
	}
	committed := func(w *httptest.ResponseRecorder) {
		t.Helper()
		var response struct {
			Code    string
			Details struct{ Committed bool }
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusServiceUnavailable || response.Code != "SECURITY_VERSION_REFRESH_FAILED" || !response.Details.Committed {
			t.Errorf("publication response HTTP %d, code=%s committed=%v", w.Code, response.Code, response.Details.Committed)
		}
	}
	committed(performTEST044Request(ctx, put, admin, http.MethodPut, "/api/v1/admin/users/"+publicID+"/permissions", `{"permissions":[]}`, map[string]string{"id": publicID}))
	check("other replica after committed permission revoke", view, token, http.StatusForbidden)
	check("permission revoke preserves session", auth, token, http.StatusNoContent)
	committed(performTEST044Request(ctx, logout, token, http.MethodPost, "/api/v1/auth/logout", "", nil))
	check("other replica after committed logout", auth, token, http.StatusUnauthorized)
	check("unrelated login on same account", auth, sibling, http.StatusNoContent)
	if err := b.cache.Ping(ctx); err != nil {
		t.Fatalf("reader's independently healthy Redis connection: %v", err)
	}
	if _, err = db.Exec(ctx, `update auth_sessions set expires_at=statement_timestamp()-interval '1 second' where session_hash=$1`, security.SessionFingerprint(other.SessionID)); err != nil {
		t.Fatal(err)
	}
	check("individually expired database session", auth, sibling, http.StatusUnauthorized)
}
