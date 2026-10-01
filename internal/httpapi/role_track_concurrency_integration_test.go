package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

// The audit history is intentionally immutable in the actual migrated schema.
// These uniquely named synthetic users/audits remain until the dedicated test
// database is disposed; no production history or trigger is altered for cleanup.
func newRoleTrackFixture(t *testing.T) (context.Context, *Server, int64, string, string, []string) {
	t.Helper()
	url := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires dedicated migrated PostgreSQL via MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	key := "apia_role_" + randomHex(6)
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = key
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	server := &Server{db: pool, cache: cache}
	var userID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'fixture') returning id,public_id`, key, key+"@example.test").Scan(&userID, &publicID); err != nil {
		t.Fatal(err)
	}
	roles := make([]string, 3)
	for i := range roles {
		roles[i] = fmt.Sprintf("%s.%d", key, i)
		if _, err = pool.Exec(ctx, `insert into roles(code,name) values($1,$1)`, roles[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `insert into permission_role_tracks(code,name) values($1,$1)`, key); err != nil {
		t.Fatal(err)
	}
	for i, role := range roles {
		if _, err = pool.Exec(ctx, `insert into permission_role_track_roles(track_code,role_id,position) select $1,id,$3 from roles where code=$2`, key, role, i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `insert into user_role_bindings(user_id,role_id) select $1,id from roles where code=$2`, userID, roles[0]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		for _, q := range []struct {
			sql string
			arg any
		}{
			{`delete from user_role_bindings where user_id=$1`, userID},
			{`delete from permission_role_tracks where code=$1`, key},
			{`delete from roles where code=any($1::text[])`, roles},
		} {
			if _, err := pool.Exec(cleanup, q.sql, q.arg); err != nil {
				t.Errorf("allocated role fixture cleanup: %v", err)
			}
		}
	})
	return ctx, server, userID, publicID, key, roles
}

func TestConcurrentRoleTrackUpgradesIntegration(t *testing.T) {
	ctx, server, userID, publicID, track, roles := newRoleTrackFixture(t)
	var initialVersion int64
	if err := server.db.QueryRow(ctx, `select permission_version from users where id=$1`, userID).Scan(&initialVersion); err != nil {
		t.Fatal(err)
	}
	blocker, err := server.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, `select id from users where id=$1 for update`, userID); err != nil {
		t.Fatal(err)
	}
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/role-upgrade", nil)
			req.SetPathValue("id", publicID)
			req.SetPathValue("code", track)
			req = req.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
			res := httptest.NewRecorder()
			server.upgradeUserRoleTrack(res, req)
			responses <- res
		}()
	}
	// Observe both real transactions blocked on the same user lock; release
	// only after both have entered the conflicting update path.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int
		if err = server.db.QueryRow(ctx, `select count(*) from pg_stat_activity where application_name=$1 and wait_event_type='Lock'`, track).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case res := <-responses:
			t.Fatalf("request completed before serialized lock: %d %s", res.Code, res.Body.String())
		case <-deadline.C:
			t.Fatal("did not observe both PostgreSQL transactions waiting on the user lock")
		case <-tick.C:
		}
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case res := <-responses:
			if res.Code != http.StatusOK {
				t.Fatalf("upgrade failed: %d %s", res.Code, res.Body.String())
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var role string
	if err = server.db.QueryRow(ctx, `select r.code from user_role_bindings b join roles r on r.id=b.role_id where b.user_id=$1`, userID).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != roles[2] {
		t.Fatalf("two upgrades lost an update: role=%q want=%q", role, roles[2])
	}
	var version, audits int64
	if err = server.db.QueryRow(ctx, `select permission_version from users where id=$1`, userID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < initialVersion+4 {
		t.Fatalf("binding changes did not increment the authoritative version: %d -> %d", initialVersion, version)
	}
	if err = server.db.QueryRow(ctx, `select count(*) from permission_audit_logs where target_user_id=$1 and action='upgrade_role_track'`, userID).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audit count=%d err=%v", audits, err)
	}
}

func TestGroupPermissionDenyAndUnsupportedScopeIntegration(t *testing.T) {
	ctx, server, userID, publicID, _, roles := newRoleTrackFixture(t)
	put := func(entry userPermissionEntry) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(updateUserPermissionsRequest{Permissions: []userPermissionEntry{entry}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPut, "/permissions", strings.NewReader(string(raw)))
		req.SetPathValue("id", publicID)
		req = req.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
		res := httptest.NewRecorder()
		server.updateUserPermissions(res, req)
		return res
	}
	for _, entry := range []userPermissionEntry{
		{Code: "group." + roles[1], Allow: true, ExpiresAt: "2030-01-01T00:00:00Z"},
		{Code: "group." + roles[1], Allow: true, Context: "project:fixture"},
	} {
		res := put(entry)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("unsupported group scope silently became a global permanent grant: %d %s", res.Code, res.Body.String())
		}
		var code string
		if err := server.db.QueryRow(ctx, `select r.code from user_role_bindings b join roles r on r.id=b.role_id where b.user_id=$1`, userID).Scan(&code); err != nil || code != roles[0] {
			t.Fatalf("rejected update changed bindings: %q %v", code, err)
		}
	}
	res := put(userPermissionEntry{Code: "group." + roles[1], Allow: false})
	if res.Code != http.StatusOK {
		t.Fatalf("deny replacement failed: %d %s", res.Code, res.Body.String())
	}
	var grants int
	if err := server.db.QueryRow(ctx, `select count(*) from user_role_bindings where user_id=$1`, userID).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("allow=false unexpectedly granted a role: %d %v", grants, err)
	}
	res = put(userPermissionEntry{Code: "group." + roles[1], Allow: true})
	if res.Code != http.StatusOK {
		t.Fatalf("positive role grant failed: %d %s", res.Code, res.Body.String())
	}
	var code string
	if err := server.db.QueryRow(ctx, `select r.code from user_role_bindings b join roles r on r.id=b.role_id where b.user_id=$1`, userID).Scan(&code); err != nil || code != roles[1] {
		t.Fatalf("positive grant did not persist: %q %v", code, err)
	}
}
