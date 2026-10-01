package httpapi

import (
	"context"
	"errors"
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
	// A warm request still checks the session's authoritative revocation and
	// user status/version. Permission payloads retain their cached loaders.
	if got := counter.queries.Load(); got != 1 {
		t.Fatalf("warm session/RBAC request executed %d PostgreSQL queries, want one authoritative session check", got)
	}
	if got := cache.Metrics().PostgresLoads; got != loadsAfterFirst {
		t.Fatalf("cache hit caused loader: before=%d after=%d", loadsAfterFirst, got)
	}
	if second.Subject != userID {
		t.Fatalf("subject=%d want=%d", second.Subject, userID)
	}
}

func TestRoleBindingChangesPermissionVersionWithoutInvalidatingSessionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	appConfig := config.Load()
	pool, err := pgxpool.New(ctx, appConfig.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "version-integration", PoolSize: 4, MinIdleConns: 1,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, TTL: time.Minute, AuthSessionCacheEnabled: true, RBACCacheEnabled: true, SessionTTL: time.Minute, RBACCacheTTL: time.Minute})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	unique := fmt.Sprintf("version_%d", time.Now().UnixNano())
	roleCode := "project_editor." + unique
	var userID int64
	var publicID string
	var initialAuth, initialPermission int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values($1,$2,'test',true,'active') returning id,public_id,auth_version,permission_version`, unique, unique+"@example.test").
		Scan(&userID, &publicID, &initialAuth, &initialPermission); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from users where id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `delete from roles where code=$1`, roleCode)
	})
	var roleID int64
	if err = pool.QueryRow(ctx, `insert into roles(code,name,description,weight) values($1,$1,'integration test',1) returning id`, roleCode).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	permissionCode := "project.edit." + unique
	var permissionID int64
	if err = pool.QueryRow(ctx, `insert into permissions(code,module,name,description) values($1,'project',$1,'integration test')
		on conflict(code) do update set code=excluded.code returning id`, permissionCode).Scan(&permissionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from permissions where code=$1`, permissionCode)
	})
	if _, err = pool.Exec(ctx, `insert into role_permissions(role_id,permission_id,allow) values($1,$2,true)`, roleID, permissionID); err != nil {
		t.Fatal(err)
	}
	claims, err := security.NewClaims(publicID, unique, unique+"@example.test", initialAuth, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at) values($1,$2,$3,$4)`,
		security.SessionFingerprint(claims.SessionID), userID, initialAuth, time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_role_bindings(user_id,role_id) values($1,$2)`, userID, roleID); err != nil {
		t.Fatal(err)
	}
	var afterGrantAuth, afterGrantPermission int64
	if err = pool.QueryRow(ctx, `select auth_version,permission_version from users where id=$1`, userID).Scan(&afterGrantAuth, &afterGrantPermission); err != nil {
		t.Fatal(err)
	}
	if afterGrantAuth != initialAuth || afterGrantPermission != initialPermission+1 {
		t.Fatalf("after grant auth=%d permission=%d, want auth=%d permission=%d", afterGrantAuth, afterGrantPermission, initialAuth, initialPermission+1)
	}
	if err = server.refreshPermissionVersion(ctx, userID); err != nil {
		t.Fatal(err)
	}
	resolved := claims
	if err = server.resolveClaimsSubject(ctx, &resolved); err != nil {
		t.Fatalf("role grant invalidated current session: %v", err)
	}
	if resolved.PermissionVersion != afterGrantPermission || !claimsAllow(resolved, "project.edit."+unique) {
		t.Fatalf("new permission was not visible on current session: version=%d rules=%#v", resolved.PermissionVersion, resolved.PermissionRules)
	}
	grantedRBACVersion := resolved.RBACVersion
	if _, err = pool.Exec(ctx, `update role_permissions set allow=false where role_id=$1 and permission_id=$2`, roleID, permissionID); err != nil {
		t.Fatal(err)
	}
	if err = server.refreshRBACVersion(ctx); err != nil {
		t.Fatal(err)
	}
	resolved = claims
	if err = server.resolveClaimsSubject(ctx, &resolved); err != nil {
		t.Fatalf("global role definition change invalidated current session: %v", err)
	}
	if resolved.PermissionVersion != afterGrantPermission || resolved.RBACVersion <= grantedRBACVersion || claimsAllow(resolved, permissionCode) {
		t.Fatalf("global RBAC change was not applied: permissionVersion=%d rbacVersion=%d rules=%#v", resolved.PermissionVersion, resolved.RBACVersion, resolved.PermissionRules)
	}
	if _, err = pool.Exec(ctx, `update role_permissions set allow=true where role_id=$1 and permission_id=$2`, roleID, permissionID); err != nil {
		t.Fatal(err)
	}
	if err = server.refreshRBACVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from user_role_bindings where user_id=$1 and role_id=$2`, userID, roleID); err != nil {
		t.Fatal(err)
	}
	if err = server.refreshPermissionVersion(ctx, userID); err != nil {
		t.Fatal(err)
	}
	resolved = claims
	if err = server.resolveClaimsSubject(ctx, &resolved); err != nil {
		t.Fatalf("role revoke invalidated current session: %v", err)
	}
	if claimsAllow(resolved, "project.edit."+unique) {
		t.Fatal("revoked permission remained active")
	}
	var afterRevokeAuth, afterRevokePermission int64
	if err = pool.QueryRow(ctx, `select auth_version,permission_version from users where id=$1`, userID).Scan(&afterRevokeAuth, &afterRevokePermission); err != nil {
		t.Fatal(err)
	}
	if afterRevokeAuth != initialAuth || afterRevokePermission != initialPermission+2 {
		t.Fatalf("after role revoke auth=%d permission=%d", afterRevokeAuth, afterRevokePermission)
	}
	if _, err = pool.Exec(ctx, `insert into user_permissions(user_id,permission_id,allow) values($1,$2,true)`, userID, permissionID); err != nil {
		t.Fatal(err)
	}
	if err = server.refreshPermissionVersion(ctx, userID); err != nil {
		t.Fatal(err)
	}
	resolved = claims
	if err = server.resolveClaimsSubject(ctx, &resolved); err != nil || !claimsAllow(resolved, permissionCode) {
		t.Fatalf("direct permission did not become active on the current session: err=%v rules=%#v", err, resolved.PermissionRules)
	}
	if _, err = pool.Exec(ctx, `delete from user_permissions where user_id=$1 and permission_id=$2`, userID, permissionID); err != nil {
		t.Fatal(err)
	}
	if err = server.refreshPermissionVersion(ctx, userID); err != nil {
		t.Fatal(err)
	}
	resolved = claims
	if err = server.resolveClaimsSubject(ctx, &resolved); err != nil {
		t.Fatalf("direct permission revoke invalidated current session: %v", err)
	}
	if claimsAllow(resolved, permissionCode) {
		t.Fatal("revoked direct permission remained active")
	}
}

func TestPasswordChangeInvalidatesCachedSessionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "auth-revocation-integration", PoolSize: 2, MinIdleConns: 1,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, TTL: time.Minute, AuthSessionCacheEnabled: true, RBACCacheEnabled: true, SessionTTL: time.Minute, RBACCacheTTL: time.Minute})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	unique := fmt.Sprintf("auth_revoke_%d", time.Now().UnixNano())
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
	warm := claims
	if err = server.resolveClaimsSubject(ctx, &warm); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update users set password_hash='new-hash' where id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if err = server.refreshAuthVersion(ctx, userID); err != nil {
		t.Fatal(err)
	}
	stale := claims
	if err = server.resolveClaimsSubject(ctx, &stale); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("password change did not revoke cached session: %v", err)
	}
}
