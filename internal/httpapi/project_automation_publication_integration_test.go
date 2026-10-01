package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProjectAutomationWritesAndTerminalStatusRespectCurrentClaimIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var actorID, modID, routeID, settingID, runID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('synthetic-automation','synthetic-automation@example.test','fixture') returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'synthetic-automation','Synthetic automation','approved') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at) values($1,'modrinth','fixture','https://example.invalid/fixture',now())`, routeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled) values($1,'changelog','modrinth','week',true) returning id`, routeID).Scan(&settingID); err != nil {
		t.Fatal(err)
	}
	oldToken, newToken := randomHex(16), randomHex(16)
	if err := pool.QueryRow(ctx, `insert into project_auto_update_runs(setting_id,status,attempts,lease_owner,lease_expires_at,result) values($1,'running',1,$2,clock_timestamp()+interval '10 minutes','{"newer":true}') returning id`, settingID, newToken).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	worker := &ProjectAutomationWorker{server: &Server{db: pool, cfg: cfg}}
	job := projectAutomationJob{RunID: runID, SettingID: settingID, RouteID: routeID, InternalID: modID, ProjectType: "mod", Kind: "changelog", SourceType: "modrinth", ExternalID: "fixture", Interval: "week", ActorID: actorID, LeaseToken: oldToken}
	oldCtx := withProjectAutomationLease(ctx, runID, oldToken)
	for _, entry := range []struct {
		name string
		run  func() error
	}{
		{"late_failure", func() error {
			return worker.fail(oldCtx, job, "external_service_unavailable", errors.New("synthetic old failure"), []byte(`{}`))
		}},
		{"late_completion", func() error { return worker.complete(oldCtx, job, []byte(`{"old":true}`)) }},
		{"late_compatibility", func() error { _, err := worker.mergeCompatibility(oldCtx, job, nil); return err }},
		{"late_changelog", func() error {
			_, err := worker.syncChangelogs(oldCtx, job, []projectAutomationRelease{{ID: "fixture-release", Version: "1.0", Body: "Synthetic old content", GameVersions: []string{"1.21.1"}, PublishedAt: time.Now().UTC()}})
			return err
		}},
		{"late_maintenance", func() error {
			_, err := worker.applyProjectMaintenancePolicy(oldCtx, job, time.Now().UTC().AddDate(-2, 0, 0), time.Now().UTC())
			return err
		}},
		{"late_actor_write", func() error {
			return worker.execAutomationWrite(oldCtx, job, `update project_auto_update_runs set actor_id=$2 where id=$1`, runID, actorID)
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			if err := entry.run(); !errors.Is(err, errProjectAutomationLeaseLost) {
				t.Fatalf("old claimant was not rejected: %v", err)
			}
		})
	}
	var status, token, lastStatus string
	var newer bool
	var changelogs int
	if err := pool.QueryRow(ctx, `select run.status,run.lease_owner,run.result->>'newer'='true',setting.last_status from project_auto_update_runs run join project_auto_update_settings setting on setting.id=run.setting_id where run.id=$1`, runID).Scan(&status, &token, &newer, &lastStatus); err != nil {
		t.Fatal(err)
	}
	if status != "running" || token != newToken || !newer || lastStatus != "never" {
		t.Fatalf("old worker altered replacement: status=%s tokenMatches=%v newer=%v setting=%s", status, token == newToken, newer, lastStatus)
	}
	if err := pool.QueryRow(ctx, `select count(*) from project_changelogs where object_route_id=$1`, routeID).Scan(&changelogs); err != nil || changelogs != 0 {
		t.Fatalf("late changelog persisted: %d %v", changelogs, err)
	}
	job.LeaseToken = newToken
	liveCtx := withProjectAutomationLease(ctx, runID, newToken)
	t.Run("current_claim_write", func(t *testing.T) {
		if err := worker.execAutomationWrite(liveCtx, job, `update project_auto_update_runs set actor_id=$2 where id=$1`, runID, actorID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("paused_setting", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `update project_auto_update_settings set enabled=false where id=$1`, settingID); err != nil {
			t.Fatal(err)
		}
		if err := worker.complete(liveCtx, job, []byte(`{}`)); !errors.Is(err, errProjectAutomationLeaseLost) {
			t.Fatalf("paused run published: %v", err)
		}
		if _, err := pool.Exec(ctx, `update project_auto_update_runs set status='pending',lease_owner='',lease_expires_at=null where id=$1`, runID); err != nil {
			t.Fatal(err)
		}
		if processed, err := worker.processOne(ctx); err != nil || processed {
			t.Fatalf("disabled setting acquired pending run: %v %v", processed, err)
		}
		if _, err := pool.Exec(ctx, `update project_auto_update_settings set enabled=true where id=$1`, settingID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `update project_auto_update_runs set status='running',lease_owner=$2,lease_expires_at=clock_timestamp()+interval '10 minutes' where id=$1`, runID, newToken); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("rebound_source", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `update project_external_sources set external_project_id='new-fixture' where project_route_id=$1`, routeID); err != nil {
			t.Fatal(err)
		}
		if err := worker.execAutomationWrite(liveCtx, job, `update project_auto_update_runs set actor_id=null where id=$1`, runID); !errors.Is(err, errProjectAutomationLeaseLost) {
			t.Fatalf("rebound source accepted old snapshot: %v", err)
		}
		if _, err := pool.Exec(ctx, `update project_external_sources set external_project_id='fixture' where project_route_id=$1`, routeID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cancelled_context_failure_write", func(t *testing.T) {
		cancelled, stop := context.WithCancel(liveCtx)
		stop()
		if err := worker.fail(cancelled, job, "external_service_unavailable", errors.New("synthetic interruption"), []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		var bounded bool
		if err := pool.QueryRow(ctx, `select status='pending' and lease_owner='' and next_attempt_at>now() from project_auto_update_runs where id=$1`, runID).Scan(&bounded); err != nil || !bounded {
			t.Fatalf("cancelled failure did not persist bounded retry: %v %v", bounded, err)
		}
		if _, err := pool.Exec(ctx, `update project_auto_update_runs set status='running',lease_owner=$2,lease_expires_at=clock_timestamp()+interval '10 minutes' where id=$1`, runID, newToken); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("current_completion", func(t *testing.T) {
		if err := worker.complete(liveCtx, job, []byte(`{"success":true}`)); err != nil {
			t.Fatal(err)
		}
		var completed bool
		if err := pool.QueryRow(ctx, `select run.status='completed' and run.lease_owner='' and run.result->>'success'='true' and setting.last_status='completed' from project_auto_update_runs run join project_auto_update_settings setting on setting.id=run.setting_id where run.id=$1`, runID).Scan(&completed); err != nil || !completed {
			t.Fatalf("completion and setting not atomic: %v %v", completed, err)
		}
	})
}
