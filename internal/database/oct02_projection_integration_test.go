package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func newOCT02ProjectionPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify database projections")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal("parse test database configuration")
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("create test database pool")
	}
	t.Cleanup(pool.Close)
	if err := InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop owned ephemeral schema: %v", err)
		}
	})
	return ctx, pool
}

func TestOCT02ServerSearchProjectionCoalescesAliases(t *testing.T) {
	ctx, pool := newOCT02ProjectionPool(t)
	if _, err := pool.Exec(ctx, `
		insert into users(id,username,email,password_hash) values(91002001,'projection','projection@example.invalid','fixture');
		insert into mods(id,project_code,slug,primary_name,review_status) values
			(91002002,'p91002002','projection-a','Projection A','approved'),(91002003,'p91002003','projection-b','Projection B','approved');
		insert into minecraft_servers(id,slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,submitted_by)
		values(91002004,'projection-server','example.invalid','example.invalid','example.invalid','example.invalid',25565,'Projection server','survival',91002001);
		insert into minecraft_server_mods(server_id,mod_id,raw_mod_id) values
			(91002004,91002002,'alias-a'),(91002004,91002002,'alias-b'),(91002004,91002003,'other-mod');
		delete from search_index_queue;
	`); err != nil {
		t.Fatal(err)
	}
	t.Run("rename with multiple aliases", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `update mods set primary_name='Renamed project' where id=91002002`); err != nil {
			t.Fatalf("rename must enqueue each affected server once: %v", err)
		}
	})
	t.Run("identifier moves between mods on the same server", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `insert into mod_identifiers(mod_id,identifier) values(91002002,'projection_alias')`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `update mod_identifiers set mod_id=91002003 where identifier='projection_alias'`); err != nil {
			t.Fatalf("identifier change must coalesce old and new server membership: %v", err)
		}
	})
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from search_index_queue where document_type='server' and document_id=91002004`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("server queue rows=%d, want one", count)
	}
}

func TestOCT02ChangelogPopularityUsesVisibleStateChanges(t *testing.T) {
	ctx, pool := newOCT02ProjectionPool(t)
	if _, err := pool.Exec(ctx, `
		insert into users(id,username,email,password_hash) values(91002011,'changelog','changelog@example.invalid','fixture');
		insert into mods(id,project_code,slug,primary_name,review_status) values(91002012,'p91002012','changelog-projection','Changelog projection','approved');
		insert into project_changelogs(id,object_route_id,event_at,project_version,default_locale,created_by)
		select 91002013,id,now(),'1.0','en-US',91002011 from public_routes where entity_type='mod' and internal_id=91002012;
		update project_changelogs set review_status='approved' where id=91002013;
		update project_changelogs set status='deleted' where id=91002013;
	`); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		name, status string
		want         int
	}{
		{"repeat deletion", "deleted", 0},
		{"restore approved release", "active", 5},
		{"repeat active state", "active", 5},
	} {
		t.Run(step.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `update project_changelogs set status=$1 where id=91002013`, step.status); err != nil {
				t.Fatal(err)
			}
			var value int
			if err := pool.QueryRow(ctx, `select coalesce(sum(event.release_value),0)::int from content_popularity_events_daily event
				join public_routes route on route.id=event.object_route_id where route.entity_type='mod' and route.internal_id=91002012`).Scan(&value); err != nil {
				t.Fatal(err)
			}
			if value != step.want {
				t.Fatalf("release contribution=%d, want %d", value, step.want)
			}
		})
	}
}
