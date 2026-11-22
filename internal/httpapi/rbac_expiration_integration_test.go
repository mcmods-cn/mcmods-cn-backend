package httpapi

import (
	"context"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestRBACCacheHonorsNaturalGrantAndDenyExpirationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newOCT02IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.Redis = securityTestRedis(t)
	replicas := []*Server{securityTestReplica(t, ctx, cfg, db, nil), securityTestReplica(t, ctx, cfg, db, nil)}
	if _, err := db.Exec(ctx, `
		insert into permissions(code,module,name) values ('fixture.view','fixture','View fixture');
		insert into roles(code,name,parents) values ('fixture_stable','Stable','{}'),
			('fixture_parent','Parent','{}'),('fixture_child','Child','{fixture_parent}');
		insert into users(username,email,password_hash)
			select name,name||'@example.test','unused' from unnest(array['fixture_binding','fixture_rule','fixture_deny','fixture_direct']) name;
		insert into role_permissions(role_id,permission_id,allow,expires_at)
			select role.id,permission.id,true,case when role.code='fixture_parent' then now()+interval '3 seconds' end
			from roles role cross join permissions permission where role.code in ('fixture_stable','fixture_parent') and permission.code='fixture.view';
		insert into user_role_bindings(user_id,role_id,expires_at)
			select actor.id,role.id,case when actor.username='fixture_binding' then now()+interval '3 seconds' end
			from users actor join roles role on (actor.username in ('fixture_binding','fixture_deny') and role.code='fixture_stable')
				or (actor.username='fixture_rule' and role.code='fixture_child');
		insert into user_permissions(user_id,permission_id,allow,expires_at)
			select actor.id,permission.id,actor.username='fixture_direct',now()+interval '3 seconds'
			from users actor cross join permissions permission where actor.username in ('fixture_direct','fixture_deny') and permission.code='fixture.view'`); err != nil {
		t.Fatal(err)
	}
	check := func(beforeExpiry bool) {
		t.Helper()
		for _, name := range []string{"fixture_binding", "fixture_rule", "fixture_direct", "fixture_deny"} {
			var id int64
			if err := db.QueryRow(ctx, `select id from users where username=$1`, name).Scan(&id); err != nil {
				t.Fatal(err)
			}
			want := (name != "fixture_deny") == beforeExpiry
			for i, server := range replicas {
				_, rules, err := server.resolveUserRootPermissions(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if got := permissionRulesAllow(rules, "fixture.view"); got != want {
					t.Errorf("%s replica %d before expiry=%v: allow=%v, want %v", name, i, beforeExpiry, got, want)
				}
			}
		}
	}
	check(true)
	// PostgreSQL advances past the stored deadline with no authorization row
	// mutation, application invalidation or simulated Redis clock adjustment.
	if _, err := db.Exec(ctx, `select pg_sleep_until(expires_at) from role_permissions
		where role_id=(select id from roles where code='fixture_parent')`); err != nil {
		t.Fatal(err)
	}
	check(false)
}
