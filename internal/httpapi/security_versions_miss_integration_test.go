package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

type securityReadBarrier struct {
	selectSQL        string
	reached, release chan struct{}
	once             sync.Once
}
type pausedSecurityRead struct{}

func (b *securityReadBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	// Before the combined authority read, these were separate version queries.
	match := strings.Contains(data.SQL, b.selectSQL) || (strings.Contains(data.SQL, "users.permission_version") && strings.Contains(data.SQL, "from auth_sessions session"))
	return context.WithValue(ctx, pausedSecurityRead{}, match)
}
func (b *securityReadBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if matches, _ := ctx.Value(pausedSecurityRead{}).(bool); !matches || data.Err != nil {
		return
	}
	b.once.Do(func() {
		close(b.reached)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
}

func TestSecurityVersionDelayedRequestCannotAuthorizeNextRequestIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db := newOCT02IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.Redis = securityTestRedis(t)
	operator, _, adminToken := createTEST044User(t, ctx, db, cfg, "version-manager", "UTC")
	grantTEST044Permissions(t, ctx, db, operator, "permission.write")
	for _, kind := range []string{"permission", "rbac", "project-acl", "auth"} {
		t.Run(kind, func(t *testing.T) {
			id, publicID, token := createTEST044User(t, ctx, db, cfg, "miss-"+kind, "UTC")
			cfg := cfg
			cfg.Redis.Namespace += ":" + kind
			b := securityTestReplica(t, ctx, cfg, db, nil)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := db.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			permission := "fixture.miss." + kind
			key := querycache.UserPermissionVersionKey(id)
			selectSQL := "select permission_version from users where id=$1"
			want := http.StatusForbidden
			var revoke func()
			switch kind {
			case "permission":
				grantTEST044Permissions(t, ctx, db, id, permission)
				h := b.requirePermission("permission.write", b.updateUserPermissions)
				revoke = func() {
					response := performTEST044Request(ctx, h, adminToken, http.MethodPut, "/api/v1/admin/users/"+publicID+"/permissions", `{"permissions":[]}`, map[string]string{"id": publicID})
					if response.Code != http.StatusOK {
						t.Fatalf("admin permission update status=%d", response.Code)
					}
				}
			case "rbac":
				exec(`insert into roles(code,name) values('fixture_miss_role','Fixture')`)
				exec(`insert into permissions(code,module,name) values($1,'fixture','View')`, permission)
				exec(`insert into role_permissions(role_id,permission_id,allow) select r.id,p.id,true from roles r cross join permissions p where r.code='fixture_miss_role' and p.code=$1`, permission)
				exec(`insert into user_role_bindings(user_id,role_id) select $1,id from roles where code='fixture_miss_role'`, id)
				key, selectSQL = "versions:rbac", "select version from runtime_versions where name='rbac'"
				revoke = func() {
					exec(`update role_permissions set allow=false where role_id=(select id from roles where code='fixture_miss_role')`)
					if err := b.refreshRBACVersion(ctx); err != nil {
						t.Fatal(err)
					}
				}
			case "project-acl":
				code := randomCatalogPublicID()
				exec(`insert into mods(project_code,slug,primary_name,review_status,submitted_by) values($1,'fixture-miss-project','Fixture','approved',$2)`, code, id)
				exec(`insert into creators(kind,name,normalized_name,review_status) values('author','Fixture author','fixture author','approved')`)
				exec(`insert into creator_claims(user_id,creator_id,status) select $1,id,'approved' from creators where normalized_name='fixture author'`, id)
				exec(`insert into content_creator_bindings(subject_type,subject_id,creator_id,status,permission_granting) select 'mod',m.id,c.id,'approved',true from mods m cross join creators c where m.project_code=$1 and c.normalized_name='fixture author'`, code)
				permission = "project.edit." + code
				key, selectSQL = "versions:project-acl", "select version from runtime_versions where name='project_acl'"
				revoke = func() {
					exec(`update content_creator_bindings set permission_granting=false where subject_type='mod' and subject_id=(select id from mods where project_code=$1)`, code)
					if err := b.refreshProjectACLVersion(ctx); err != nil {
						t.Fatal(err)
					}
				}
			case "auth":
				grantTEST044Permissions(t, ctx, db, id, permission)
				key, selectSQL = userAuthVersionCacheKey(publicID), "select auth_version from users where public_id="
				want = http.StatusUnauthorized
				revoke = func() {
					exec(`update users set password_hash='fixture-reset' where id=$1`, id)
					if err := b.refreshAuthVersion(ctx, id); err != nil {
						t.Fatal(err)
					}
				}
			}
			barrier := &securityReadBarrier{selectSQL: selectSQL, reached: make(chan struct{}), release: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(barrier.release) }) }
			defer release()
			a := securityTestReplica(t, ctx, cfg, db, barrier)
			// Register public guards before the schedule so no role metadata changes mid-test.
			endpoint := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
			gateA, gateB := a.requirePermission(permission, endpoint), b.requirePermission(permission, endpoint)
			get := func(gate http.HandlerFunc) int {
				return performTEST044Request(ctx, gate, token, http.MethodGet, "/fixture/view", "", nil).Code
			}
			if got := get(gateB); got != http.StatusNoContent {
				t.Fatalf("initial grant status=%d", got)
			}
			if err := b.cache.DeleteShared(ctx, key); err != nil {
				t.Fatal(err)
			}
			oldRequest := make(chan int, 1)
			go func() { oldRequest <- get(gateA) }()
			select {
			case <-barrier.reached:
			case <-ctx.Done():
				t.Fatal("old request never reached PostgreSQL version read")
			case status := <-oldRequest:
				t.Fatalf("old request completed before its barrier, HTTP %d", status)
			}
			previous, err := dbVersionForTest(ctx, db, kind, id)
			if err != nil {
				t.Fatal(err)
			}
			revoke()
			current, err := dbVersionForTest(ctx, db, kind, id)
			if err != nil || current <= previous {
				t.Fatalf("revocation did not advance its version: %d to %d, err=%v", previous, current, err)
			}
			if visible, ok := b.cache.Get(ctx, key); !ok || string(visible) != strconv.FormatInt(current, 10) {
				t.Fatalf("new version was not published before releasing the old reader")
			}

			release()
			// The original request began before the commit. Its response is not
			// evidence about a new request started strictly after revocation.
			select {
			case <-oldRequest:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if got := get(gateB); got != want {
				t.Errorf("fresh request after delayed lookup: HTTP %d, want %d", got, want)
			}
		})
	}
}

func dbVersionForTest(ctx context.Context, db *pgxpool.Pool, kind string, id int64) (int64, error) {
	var version int64
	var err error
	switch kind {
	case "permission":
		err = db.QueryRow(ctx, `select permission_version from users where id=$1`, id).Scan(&version)
	case "auth":
		err = db.QueryRow(ctx, `select auth_version from users where id=$1`, id).Scan(&version)
	default:
		err = db.QueryRow(ctx, `select version from runtime_versions where name=$1`, strings.ReplaceAll(kind, "-", "_")).Scan(&version)
	}
	return version, err
}
