package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/systemactor"
)

func TestTEST019ProjectAutomationWorkerRetriesCompletesIdempotentlyAndRecoversLeasesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the project automation Worker lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if dropErr := database.DropEphemeralSchema(cleanupCtx, pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var providerRequests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/project/test019-project/version" {
			http.NotFound(w, r)
			return
		}
		requestNumber := providerRequests.Add(1)
		if requestNumber == 1 {
			http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{
			"ID":"test019-release","Name":"TEST019 release","VersionNumber":"1.0.0",
			"Changelog":"Reliable automated changelog","DatePublished":%q,"VersionType":"release",
			"game_versions":["1.21.1"],"loaders":["fabric"],"files":[]
		}]`, time.Now().UTC().Add(-30*24*time.Hour).Format(time.RFC3339))
	}))
	defer provider.Close()

	serverConfig := config.Load()
	server := &Server{cfg: serverConfig, db: pool}
	importConfig := defaultModImportConfig()
	importConfig.RequestTimeoutSeconds = 5
	importConfig.Modrinth.BaseURL = provider.URL
	importConfig.Modrinth.Token = ""
	sealedImportConfig, err := server.sealSystemSetting(importConfig)
	if err != nil {
		t.Fatal(err)
	}
	minecraftCatalog := `{"versions":[{"code":"1.21.1","type":"release"}],"commonVersions":["1.21.1"],"loaders":[{"code":"Fabric","name":"Fabric","versions":["1.21.1"]}]}`
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values
		($1,$2::jsonb),($3,$4::jsonb) on conflict(key) do update set value=excluded.value`,
		minecraftVersionsSettingKey, minecraftCatalog, modImportConfigSettingKey, sealedImportConfig); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `with account as (
		insert into users(username,email,password_hash,email_verified,status)
		values($1,$2,'password-login-disabled',true,$3) returning id
	), permission as (
		insert into permissions(code,module,name,description,access_type)
		select code,'test019',code,'TEST019 automation fixture','write'
		from unnest(array['project.create','project.edit','project.no-review','content.no-review']) code
		returning id
	)
	insert into user_permissions(user_id,permission_id,allow,source,source_key)
	select account.id,permission.id,true,'system_seed','test019' from account cross join permission`,
		systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus); err != nil {
		t.Fatal(err)
	}

	stamp := time.Now().UnixNano()
	var ownerID, projectID, routeID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test019',true) returning id`, fmt.Sprintf("test019-%d", stamp),
		fmt.Sprintf("test019-%d@example.invalid", stamp)).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('t019m0001','test019-project','TEST019 project','approved',$1) returning id`, ownerID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
		values($1,'modrinth','test019-project',$2,now(),$3)`, routeID, provider.URL+"/project/test019-project", ownerID); err != nil {
		t.Fatal(err)
	}
	var changelogSettingID int64
	if err = pool.QueryRow(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled,next_run_at,configured_by)
		values($1,'changelog','modrinth','week',true,now()-interval '1 minute',$2) returning id`, routeID, ownerID).Scan(&changelogSettingID); err != nil {
		t.Fatal(err)
	}

	worker := &ProjectAutomationWorker{server: server}
	if err = worker.scheduleDue(ctx); err != nil {
		t.Fatal(err)
	}
	if err = worker.scheduleDue(ctx); err != nil {
		t.Fatal(err)
	}
	var runID int64
	var scheduledRuns int
	if err = pool.QueryRow(ctx, `select count(*)::int,min(id) from project_auto_update_runs where setting_id=$1`, changelogSettingID).Scan(&scheduledRuns, &runID); err != nil {
		t.Fatal(err)
	}
	if scheduledRuns != 1 {
		t.Fatalf("duplicate scheduling created %d active runs", scheduledRuns)
	}

	processed, err := worker.processOne(ctx)
	if err != nil || !processed {
		t.Fatalf("temporary provider attempt processed=%t err=%v", processed, err)
	}
	var status, errorCode, leaseOwner string
	var attempts int
	var retryScheduled, actorRecorded bool
	if err = pool.QueryRow(ctx, `select status,attempts,last_error_code,lease_owner,next_attempt_at>now(),actor_id is not null
		from project_auto_update_runs where id=$1`, runID).Scan(&status, &attempts, &errorCode, &leaseOwner, &retryScheduled, &actorRecorded); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 || errorCode != "external_service_unavailable" || leaseOwner != "" || !retryScheduled || !actorRecorded {
		t.Fatalf("temporary state=%q attempts=%d code=%q lease=%q retry=%t actor=%t", status, attempts, errorCode, leaseOwner, retryScheduled, actorRecorded)
	}
	if _, err = pool.Exec(ctx, `update project_auto_update_runs set next_attempt_at=now()-interval '1 second' where id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	processed, err = worker.processOne(ctx)
	if err != nil || !processed {
		t.Fatalf("successful provider retry processed=%t err=%v", processed, err)
	}
	var created int
	if err = pool.QueryRow(ctx, `select status,attempts,coalesce((result->>'created')::int,0),last_error_code
		from project_auto_update_runs where id=$1`, runID).Scan(&status, &attempts, &created, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || attempts != 2 || created != 1 || errorCode != "" {
		t.Fatalf("completed state=%q attempts=%d created=%d code=%q", status, attempts, created, errorCode)
	}
	assertTEST019ChangelogArtifacts(t, ctx, pool, routeID, 1, 1)

	if _, err = pool.Exec(ctx, `update project_auto_update_settings set next_run_at=now()-interval '1 second' where id=$1`, changelogSettingID); err != nil {
		t.Fatal(err)
	}
	if err = worker.scheduleDue(ctx); err != nil {
		t.Fatal(err)
	}
	processed, err = worker.processOne(ctx)
	if err != nil || !processed {
		t.Fatalf("idempotent provider rerun processed=%t err=%v", processed, err)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from project_auto_update_runs where setting_id=$1 and status='completed'`, changelogSettingID).Scan(&scheduledRuns); err != nil {
		t.Fatal(err)
	}
	if scheduledRuns != 2 || providerRequests.Load() != 3 {
		t.Fatalf("completed runs=%d provider requests=%d", scheduledRuns, providerRequests.Load())
	}
	assertTEST019ChangelogArtifacts(t, ctx, pool, routeID, 1, 1)

	var downloadSettingID, downloadRunID int64
	if err = pool.QueryRow(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled,configured_by)
		values($1,'site_downloads','modrinth','week',false,$2) returning id`, routeID, ownerID).Scan(&downloadSettingID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into project_auto_update_runs(setting_id) values($1) returning id`, downloadSettingID).Scan(&downloadRunID); err != nil {
		t.Fatal(err)
	}
	processed, err = worker.processOne(ctx)
	if err != nil || !processed {
		t.Fatalf("terminal license attempt processed=%t err=%v", processed, err)
	}
	var terminal bool
	if err = pool.QueryRow(ctx, `select status,attempts,last_error_code,finished_at is not null
		from project_auto_update_runs where id=$1`, downloadRunID).Scan(&status, &attempts, &errorCode, &terminal); err != nil {
		t.Fatal(err)
	}
	if status != "dead_letter" || attempts != 1 || errorCode != "license_denied" || !terminal || providerRequests.Load() != 3 {
		t.Fatalf("terminal state=%q attempts=%d code=%q finished=%t providerRequests=%d", status, attempts, errorCode, terminal, providerRequests.Load())
	}

	var orphanRouteID, leaseSettingID, leaseRunID int64
	if err = pool.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		values('t019or001','mod',190019,'/mods/t019or001') returning id`).Scan(&orphanRouteID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled)
		values($1,'minecraft_versions','modrinth','week',false) returning id`, orphanRouteID).Scan(&leaseSettingID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into project_auto_update_runs(setting_id,status,lease_owner,lease_expires_at,attempts)
		values($1,'running','dead-worker',now()-interval '1 minute',2) returning id`, leaseSettingID).Scan(&leaseRunID); err != nil {
		t.Fatal(err)
	}
	if err = worker.tick(ctx); err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err = pool.QueryRow(ctx, `select status,lease_owner,last_error_code,next_attempt_at<=now(),attempts
		from project_auto_update_runs where id=$1`, leaseRunID).Scan(&status, &leaseOwner, &errorCode, &ready, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || leaseOwner != "" || errorCode != "lease_expired" || !ready || attempts != 2 {
		t.Fatalf("recovered lease status=%q lease=%q code=%q ready=%t attempts=%d", status, leaseOwner, errorCode, ready, attempts)
	}
}

func assertTEST019ChangelogArtifacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID int64, wantBindings, wantRevisions int) {
	t.Helper()
	var bindings, changelogs, revisions, events, tasks, outbox int
	if err := pool.QueryRow(ctx, `select
		(select count(*)::int from external_release_bindings where project_route_id=$1),
		(select count(*)::int from project_changelogs where object_route_id=$1),
		(select count(*)::int from content_revisions where aggregate_type='project_changelog' and source='auto_update'
			and aggregate_key in(select public_id from project_changelogs where object_route_id=$1)),
		(select count(*)::int from project_update_events where project_route_id=$1),
		(select count(*)::int from project_update_notification_tasks where event_id in(select id from project_update_events where project_route_id=$1)),
		(select count(*)::int from nats_outbox where aggregate_type='project_update_event'
			and aggregate_id in(select id::text from project_update_events where project_route_id=$1))`, routeID).
		Scan(&bindings, &changelogs, &revisions, &events, &tasks, &outbox); err != nil {
		t.Fatal(err)
	}
	if bindings != wantBindings || changelogs != wantBindings || revisions != wantRevisions || events != 1 || tasks != 1 || outbox != 1 {
		t.Fatalf("artifacts bindings=%d changelogs=%d revisions=%d events=%d tasks=%d outbox=%d",
			bindings, changelogs, revisions, events, tasks, outbox)
	}
	var sections []string
	if err := pool.QueryRow(ctx, `select changed_sections from project_update_events where project_route_id=$1`, routeID).Scan(&sections); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sections, []string{"changelog", "minecraft_versions", "project_version"}) {
		t.Fatalf("project update sections=%v", sections)
	}
}
