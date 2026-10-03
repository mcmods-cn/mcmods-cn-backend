package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestOCT02ProjectAutomationLateAttemptCannotMutateCurrentRunIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	var mod, route, setting, run int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values('autolease','auto-lease','Auto lease','approved') returning id`).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, mod).Scan(&route); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at) values($1,'modrinth','synthetic','https://modrinth.com/mod/synthetic',now())`, route); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code) values($1,'minecraft_versions','modrinth','week') returning id`, route).Scan(&setting); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into project_auto_update_runs(setting_id,status,lease_owner,lease_expires_at,attempts,result) values($1,'running','new-attempt',now()+interval '10 minutes',2,'{"current":true}') returning id`, setting).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err := saveMinecraftVersionConfigAndInvalidateStaleArtifacts(ctx, pool, minecraftVersionConfig{Versions: []minecraftVersionOption{{Code: "1.20.1", Type: "release"}}, Loaders: []minecraftLoaderOption{{Code: "forge", Name: "Forge", Versions: []string{"1.20.1"}}}}); err != nil {
		t.Fatal(err)
	}
	job := projectAutomationJob{RunID: run, SettingID: setting, RouteID: route, InternalID: mod, ProjectType: "mod", ProjectPublicID: "autolease", Kind: "minecraft_versions", SourceType: "modrinth", ExternalID: "synthetic", Interval: "week"}
	// JSON accommodates the old job shape when this regression runs on the
	// fixed baseline; the new claim token is intentionally stale.
	if err := json.Unmarshal([]byte(`{"LeaseOwner":"old-attempt"}`), &job); err != nil {
		t.Fatal(err)
	}
	worker := &ProjectAutomationWorker{server: &Server{db: pool}}
	t.Run("late_failure", func(t *testing.T) {
		if err := worker.fail(ctx, job, "old_failure", errors.New("synthetic late failure"), []byte(`{"late":true}`)); err == nil {
			t.Error("late attempt changed the newer run's retry state")
		}
		var status, owner string
		var current bool
		if err := pool.QueryRow(ctx, `select status,lease_owner,coalesce((result->>'current')::boolean,false) from project_auto_update_runs where id=$1`, run).Scan(&status, &owner, &current); err != nil || status != "running" || owner != "new-attempt" || !current {
			t.Errorf("newer attempt overwritten: %s/%s/current=%t error=%v", status, owner, current, err)
		}
	})
	t.Run("late_business_write", func(t *testing.T) {
		if _, err := worker.mergeCompatibility(ctx, job, []providerProjectFile{{GameVersions: []string{"1.20.1"}, Loaders: []string{"forge"}}}); err == nil {
			t.Error("late attempt was allowed to publish compatibility facts")
		}
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from mod_loader_compatibilities where mod_id=$1`, mod).Scan(&count); err != nil || count != 0 {
			t.Errorf("late compatibility facts=%d error=%v", count, err)
		}
	})
	if err := json.Unmarshal([]byte(`{"LeaseOwner":"new-attempt"}`), &job); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []struct {
		name, mutate, restore string
	}{
		{"expired_claim", `update project_auto_update_runs set lease_expires_at=now()-interval '1 second' where id=$1`, `update project_auto_update_runs set lease_expires_at=now()+interval '10 minutes' where id=$1`},
		{"rebound_source", `update project_external_sources set external_project_id='replacement' where project_route_id=$1`, `update project_external_sources set external_project_id='synthetic' where project_route_id=$1`},
	} {
		t.Run(changed.name, func(t *testing.T) {
			id := run
			if changed.name == "rebound_source" {
				id = route
			}
			if _, err := pool.Exec(ctx, changed.mutate, id); err != nil {
				t.Fatal(err)
			}
			if _, err := worker.mergeCompatibility(ctx, job, []providerProjectFile{{GameVersions: []string{"1.20.1"}, Loaders: []string{"forge"}}}); err == nil {
				t.Fatal("expired or rebound claim published compatibility facts")
			}
			if _, err := pool.Exec(ctx, changed.restore, id); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("current_claim", func(t *testing.T) {
		if _, err := worker.mergeCompatibility(ctx, job, []providerProjectFile{{GameVersions: []string{"1.20.1"}, Loaders: []string{"forge"}}}); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from mod_loader_compatibilities where mod_id=$1`, mod).Scan(&count); err != nil || count != 1 {
			t.Fatalf("current claim compatibility facts=%d error=%v", count, err)
		}
		if err := worker.fail(ctx, job, "provider_timeout", errors.New("synthetic retryable timeout"), []byte(`{"retry":true}`)); err != nil {
			t.Fatal(err)
		}
		var status, owner string
		var retry bool
		if err := pool.QueryRow(ctx, `select status,lease_owner,next_attempt_at>now() from project_auto_update_runs where id=$1`, run).Scan(&status, &owner, &retry); err != nil || status != "pending" || owner != "" || !retry {
			t.Fatalf("current retry state=%s/%s/retry=%t error=%v", status, owner, retry, err)
		}
	})
}

func TestOCT02ProjectAutomationCrashRecoveryHasAttemptLimitIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	var route, setting, run int64
	if err := pool.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id) values('octcrash1','mod',900001) returning id`).Scan(&route); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled,next_run_at) values($1,'minecraft_versions','modrinth','week',false,now()-interval '1 day') returning id`, route).Scan(&setting); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into project_auto_update_runs(setting_id,status,lease_owner,lease_expires_at,attempts) values($1,'running','crashed-attempt',now()-interval '1 minute',5) returning id`, setting).Scan(&run); err != nil {
		t.Fatal(err)
	}
	worker := &ProjectAutomationWorker{server: &Server{db: pool}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := worker.tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var status, owner, lastStatus string
	var finished, nextCycle bool
	var attempts int
	if err := pool.QueryRow(ctx, `select run.status,run.lease_owner,run.attempts,run.finished_at is not null,setting.last_status,setting.next_run_at>now() from project_auto_update_runs run join project_auto_update_settings setting on setting.id=run.setting_id where run.id=$1`, run).Scan(&status, &owner, &attempts, &finished, &lastStatus, &nextCycle); err != nil || status != "dead_letter" || owner != "" || attempts != 5 || !finished || lastStatus != "dead_letter" || !nextCycle {
		t.Fatalf("crash recovery state=%s owner=%s attempts=%d finished=%t setting=%s nextcycle=%t error=%v", status, owner, attempts, finished, lastStatus, nextCycle, err)
	}
}
