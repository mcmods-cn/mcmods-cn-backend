package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestAdminAuthorizationWritesAreTransactionalAndImmediatelyEffectiveIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify admin authorization writes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
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
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "admin-write-integration",
		PoolSize: 2, MinIdleConns: 1, DialTimeout: 100 * time.Millisecond,
		ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond,
		TTL: time.Minute, AuthSessionCacheEnabled: true, RBACCacheEnabled: true,
		SessionTTL: time.Minute, RBACCacheTTL: time.Minute,
	})
	defer cache.Close()
	server := &Server{db: pool, cache: cache, mux: http.NewServeMux()}
	server.mux.HandleFunc("PUT /api/v1/admin/users/{id}/permissions", server.updateUserPermissions)
	server.mux.HandleFunc("POST /api/v1/admin/roles", server.createRole)
	server.mux.HandleFunc("PUT /api/v1/admin/roles/{code}", server.updateRole)
	server.mux.HandleFunc("DELETE /api/v1/admin/roles/{code}", server.deleteRole)
	server.handler = server.mux

	var operatorID, targetID int64
	var targetPublicID string
	var authVersion, permissionVersion int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values('authorization_operator','authorization-operator@example.invalid','test-only',true,'active') returning id`).Scan(&operatorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values('authorization_target','authorization-target@example.invalid','test-only',true,'active')
		returning id,public_id,auth_version,permission_version`).Scan(&targetID, &targetPublicID, &authVersion, &permissionVersion); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`insert into roles(code,name,weight) values
			('authorization_manual','Manual',10),('authorization_level','Level',20),('authorization_banned','Banned',30)`,
		`insert into permissions(code,module,name) values
			('test.role_grant','test','Role grant'),('test.manual_allow','test','Manual allow'),
			('test.preference','test','Preference'),('test.rollback','test','Rollback'),
			('test.concurrent_a','test','Concurrent A'),('test.concurrent_b','test','Concurrent B'),
			('test.cache_failure','test','Cache failure')`,
		`insert into role_permissions(role_id,permission_id,allow)
			select role.id,permission.id,true from roles role,permissions permission
			where role.code='authorization_manual' and permission.code='test.role_grant'`,
		`insert into user_role_bindings(user_id,role_id,source,source_key,expires_at)
			select $1,id,'level_track','staff',now()+interval '2 hours' from roles where code='authorization_level'`,
		`insert into user_role_bindings(user_id,role_id,source,source_key,expires_at)
			select $1,id,'account_status','banned',now()+interval '1 hour' from roles where code='authorization_banned'`,
		`insert into user_permissions(user_id,permission_id,allow,source,source_key)
			select $1,id,false,'user_preference','profile_settings' from permissions where code='test.preference'`,
	} {
		if strings.Contains(statement, "$1") {
			_, err = pool.Exec(ctx, statement, targetID)
		} else {
			_, err = pool.Exec(ctx, statement)
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	targetClaims, err := security.NewClaims(targetPublicID, "authorization_target", "authorization-target@example.invalid", authVersion, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		values($1,$2,$3,$4)`, security.SessionFingerprint(targetClaims.SessionID), targetID, authVersion, time.Unix(targetClaims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	warmClaims := targetClaims
	if err = server.resolveClaimsSubject(ctx, &warmClaims); err != nil {
		t.Fatal(err)
	}
	if claimsAllow(warmClaims, "test.manual_allow") {
		t.Fatal("permission was present before the admin write")
	}

	operatorClaims := security.Claims{Subject: operatorID}
	expiresAt := time.Now().UTC().Add(90 * time.Minute).Truncate(time.Second)
	body := fmt.Sprintf(`{"permissions":[{"code":"group.authorization_manual","allow":true,"expiresAt":%q,"source":"manual"},{"code":"test.manual_allow","allow":true,"expiresAt":%q,"source":"manual"}]}`,
		expiresAt.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
	counter.queries.Store(0)
	response := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPut,
		"/api/v1/admin/users/"+targetPublicID+"/permissions", body)
	if response.Code != http.StatusOK {
		t.Fatalf("user permission write status=%d body=%s", response.Code, response.Body.String())
	}
	if got := counter.queries.Load(); got > 18 {
		t.Fatalf("one role plus one direct permission used %d SQL statements; want at most 18", got)
	}

	var afterAuthVersion, afterPermissionVersion int64
	if err = pool.QueryRow(ctx, `select auth_version,permission_version from users where id=$1`, targetID).
		Scan(&afterAuthVersion, &afterPermissionVersion); err != nil {
		t.Fatal(err)
	}
	if afterAuthVersion != authVersion || afterPermissionVersion <= permissionVersion {
		t.Fatalf("versions after write auth=%d permission=%d, initial auth=%d permission=%d",
			afterAuthVersion, afterPermissionVersion, authVersion, permissionVersion)
	}
	var levelBindings, bannedBindings, manualBindings, preferenceBindings, auditRows int
	var roleExpiry, directExpiry time.Time
	if err = pool.QueryRow(ctx, `select
		(select count(*) from user_role_bindings where user_id=$1 and source='level_track' and source_key='staff'),
		(select count(*) from user_role_bindings where user_id=$1 and source='account_status' and source_key='banned'),
		(select count(*) from user_role_bindings where user_id=$1 and source='manual'),
		(select count(*) from user_permissions where user_id=$1 and source='user_preference' and source_key='profile_settings'),
		(select count(*) from permission_audit_logs where operator_id=$2 and target_user_id=$1 and action='update_user_permissions'),
		(select expires_at from user_role_bindings binding join roles role on role.id=binding.role_id
		 where binding.user_id=$1 and binding.source='manual' and role.code='authorization_manual'),
		(select expires_at from user_permissions binding join permissions permission on permission.id=binding.permission_id
		 where binding.user_id=$1 and binding.source='manual' and permission.code='test.manual_allow')`, targetID, operatorID).
		Scan(&levelBindings, &bannedBindings, &manualBindings, &preferenceBindings, &auditRows, &roleExpiry, &directExpiry); err != nil {
		t.Fatal(err)
	}
	if levelBindings != 1 || bannedBindings != 1 || manualBindings != 1 || preferenceBindings != 1 || auditRows != 1 {
		t.Fatalf("source/audit facts level=%d banned=%d manual=%d preference=%d audit=%d",
			levelBindings, bannedBindings, manualBindings, preferenceBindings, auditRows)
	}
	if !roleExpiry.Equal(expiresAt) || !directExpiry.Equal(expiresAt) {
		t.Fatalf("manual expiries role=%s direct=%s want=%s", roleExpiry, directExpiry, expiresAt)
	}
	resolvedClaims := targetClaims
	if err = server.resolveClaimsSubject(ctx, &resolvedClaims); err != nil {
		t.Fatalf("admin permission write invalidated the target session: %v", err)
	}
	if resolvedClaims.PermissionVersion != afterPermissionVersion || !claimsAllow(resolvedClaims, "test.manual_allow") || !claimsAllow(resolvedClaims, "test.role_grant") {
		t.Fatalf("new authorization was not immediate: permissionVersion=%d rules=%#v", resolvedClaims.PermissionVersion, resolvedClaims.PermissionRules)
	}

	rejected := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPut,
		"/api/v1/admin/users/"+targetPublicID+"/permissions",
		`{"permissions":[{"code":"group.authorization_manual","allow":false}]}`)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("group allow=false status=%d body=%s", rejected.Code, rejected.Body.String())
	}

	if _, err = pool.Exec(ctx, `alter table permission_audit_logs add constraint reject_user_permission_audit
		check(action<>'update_user_permissions') not valid`); err != nil {
		t.Fatal(err)
	}
	failedAudit := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPut,
		"/api/v1/admin/users/"+targetPublicID+"/permissions",
		`{"permissions":[{"code":"test.rollback","allow":true,"source":"manual"}]}`)
	if failedAudit.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure status=%d body=%s", failedAudit.Code, failedAudit.Body.String())
	}
	var rollbackRows, retainedRows int
	if err = pool.QueryRow(ctx, `select
		(select count(*) from user_permissions binding join permissions permission on permission.id=binding.permission_id
		 where binding.user_id=$1 and binding.source='manual' and permission.code='test.rollback'),
		(select count(*) from user_permissions binding join permissions permission on permission.id=binding.permission_id
		 where binding.user_id=$1 and binding.source='manual' and permission.code='test.manual_allow')`, targetID).
		Scan(&rollbackRows, &retainedRows); err != nil {
		t.Fatal(err)
	}
	if rollbackRows != 0 || retainedRows != 1 {
		t.Fatalf("audit failure did not roll back authorization: rollback=%d retained=%d", rollbackRows, retainedRows)
	}
	if _, err = pool.Exec(ctx, `alter table permission_audit_logs drop constraint reject_user_permission_audit`); err != nil {
		t.Fatal(err)
	}

	missingParent := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPost, "/api/v1/admin/roles",
		`{"code":"authorization_orphan","name":"Orphan","parents":["authorization_missing"],"permissionEntries":[]}`)
	if missingParent.Code != http.StatusBadRequest {
		t.Fatalf("missing-parent role status=%d body=%s", missingParent.Code, missingParent.Body.String())
	}
	for _, roleBody := range []string{
		`{"code":"authorization_parent","name":"Parent","parents":[],"permissionEntries":[]}`,
		`{"code":"authorization_child","name":"Child","parents":["authorization_parent"],"permissionEntries":[]}`,
	} {
		created := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPost, "/api/v1/admin/roles", roleBody)
		if created.Code != http.StatusCreated {
			t.Fatalf("role create status=%d body=%s", created.Code, created.Body.String())
		}
	}
	cycle := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPut, "/api/v1/admin/roles/authorization_parent",
		`{"name":"Parent","parents":["authorization_child"],"permissionEntries":[]}`)
	if cycle.Code != http.StatusBadRequest {
		t.Fatalf("role cycle status=%d body=%s", cycle.Code, cycle.Body.String())
	}
	if _, err = pool.Exec(ctx, `insert into permission_role_tracks(code,name) values('authorization_track','Authorization track')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into permission_role_track_roles(track_code,role_id,position)
		select 'authorization_track',id,0 from roles where code='authorization_child'`); err != nil {
		t.Fatal(err)
	}
	blockedDelete := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodDelete,
		"/api/v1/admin/roles/authorization_child", "")
	if blockedDelete.Code != http.StatusConflict {
		t.Fatalf("track-dependent role delete status=%d body=%s", blockedDelete.Code, blockedDelete.Body.String())
	}

	if _, err = pool.Exec(ctx, `alter table permission_audit_logs add constraint reject_role_audit check(action<>'create_role') not valid`); err != nil {
		t.Fatal(err)
	}
	roleAuditFailure := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPost, "/api/v1/admin/roles",
		`{"code":"authorization_unaudited","name":"Unaudited","parents":[],"permissionEntries":[]}`)
	if roleAuditFailure.Code != http.StatusInternalServerError {
		t.Fatalf("role audit failure status=%d body=%s", roleAuditFailure.Code, roleAuditFailure.Body.String())
	}
	var unauditedRole int
	if err = pool.QueryRow(ctx, `select count(*) from roles where code='authorization_unaudited'`).Scan(&unauditedRole); err != nil {
		t.Fatal(err)
	}
	if unauditedRole != 0 {
		t.Fatal("role committed without its permission audit record")
	}
	if _, err = pool.Exec(ctx, `alter table permission_audit_logs drop constraint reject_role_audit`); err != nil {
		t.Fatal(err)
	}

	concurrentBodies := []string{
		`{"permissions":[{"code":"test.concurrent_a","allow":true,"source":"manual"}]}`,
		`{"permissions":[{"code":"test.concurrent_b","allow":true,"source":"manual"}]}`,
	}
	responses := make([]*httptest.ResponseRecorder, len(concurrentBodies))
	var wait sync.WaitGroup
	for index := range concurrentBodies {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			responses[index] = invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPut,
				"/api/v1/admin/users/"+targetPublicID+"/permissions", concurrentBodies[index])
		}(index)
	}
	wait.Wait()
	for index, concurrentResponse := range responses {
		if concurrentResponse.Code != http.StatusOK {
			t.Fatalf("concurrent write %d status=%d body=%s", index, concurrentResponse.Code, concurrentResponse.Body.String())
		}
	}
	var concurrentA, concurrentB int
	if err = pool.QueryRow(ctx, `select
		count(*) filter(where permission.code='test.concurrent_a'),
		count(*) filter(where permission.code='test.concurrent_b')
		from user_permissions binding join permissions permission on permission.id=binding.permission_id
		where binding.user_id=$1 and binding.source='manual'`, targetID).Scan(&concurrentA, &concurrentB); err != nil {
		t.Fatal(err)
	}
	if concurrentA+concurrentB != 1 {
		t.Fatalf("concurrent replacement merged payloads: A=%d B=%d", concurrentA, concurrentB)
	}

	redisServer.Close()
	cacheFailure := invokeAdminAuthorizationRoute(ctx, server, operatorClaims, http.MethodPut,
		"/api/v1/admin/users/"+targetPublicID+"/permissions",
		`{"permissions":[{"code":"test.cache_failure","allow":true,"source":"manual"}]}`)
	if cacheFailure.Code != http.StatusServiceUnavailable ||
		!strings.Contains(cacheFailure.Body.String(), `"code":"SECURITY_VERSION_REFRESH_FAILED"`) ||
		!strings.Contains(cacheFailure.Body.String(), `"committed":true`) {
		t.Fatalf("cache publication outage was not exposed after the committed write: status=%d body=%s", cacheFailure.Code, cacheFailure.Body.String())
	}
	resolvedDuringCacheFailure := targetClaims
	if err = server.resolveClaimsSubject(ctx, &resolvedDuringCacheFailure); err != nil {
		t.Fatalf("session failed closed instead of loading PostgreSQL during cache outage: %v", err)
	}
	if !claimsAllow(resolvedDuringCacheFailure, "test.cache_failure") {
		t.Fatalf("new permission was stale during cache outage: %#v", resolvedDuringCacheFailure.PermissionRules)
	}
}

func invokeAdminAuthorizationRoute(
	ctx context.Context,
	server *Server,
	claims security.Claims,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:43210"
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}
