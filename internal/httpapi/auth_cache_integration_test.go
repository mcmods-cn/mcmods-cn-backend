package httpapi

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

type integrationQueryCounter struct{ queries atomic.Int64 }

func (counter *integrationQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	counter.queries.Add(1)
	return ctx
}
func (*integrationQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestSessionAndRBACCacheHitAvoidsDatabaseLoadersIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	appConfig := config.Load()
	poolConfig, err := pgxpool.ParseConfig(appConfig.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "auth-integration", PoolSize: 4, MinIdleConns: 1,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, TTL: time.Minute, AuthSessionCacheEnabled: true, RBACCacheEnabled: true, SessionTTL: time.Minute, RBACCacheTTL: time.Minute})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	unique := fmt.Sprintf("cache_%d", time.Now().UnixNano())
	var userID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status) values($1,$2,'test',true,'active') returning id,public_id`, unique, unique+"@example.test").Scan(&userID, &publicID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from users where id=$1`, userID) })
	claims, err := security.NewClaims(publicID, unique, unique+"@example.test", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at) values($1,$2,1,$3)`, security.SessionFingerprint(claims.SessionID), userID, time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	first := claims
	if err = server.resolveClaimsSubject(ctx, &first); err != nil {
		t.Fatal(err)
	}
	loadsAfterFirst := cache.Metrics().PostgresLoads
	counter.queries.Store(0)
	second := claims
	if err = server.resolveClaimsSubject(ctx, &second); err != nil {
		t.Fatal(err)
	}
	if got := counter.queries.Load(); got != 0 {
		t.Fatalf("warm session/RBAC request executed %d PostgreSQL queries", got)
	}
	if got := cache.Metrics().PostgresLoads; got != loadsAfterFirst {
		t.Fatalf("cache hit caused loader: before=%d after=%d", loadsAfterFirst, got)
	}
	if second.Subject != userID {
		t.Fatalf("subject=%d want=%d", second.Subject, userID)
	}
}
