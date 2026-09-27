package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestProjectAutomationSchedulerAndListsExposeDatabaseFailuresIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project automation failure observability")
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
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var routeID int64
	if err = pool.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		values('arch01301','mod',130013,'/mods/arch01301') returning id`).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled,next_run_at)
		values($1,'changelog','modrinth','week',true,now()-interval '1 minute')`, routeID); err != nil {
		t.Fatal(err)
	}
	worker := NewProjectAutomationWorker(config.Load(), pool)
	if _, err = pool.Exec(ctx, `create function pg_temp.arch013_reject_schedule_commit() returns trigger as $$
		begin raise exception 'injected schedule commit failure'; end $$ language plpgsql;
		create constraint trigger arch013_reject_schedule_commit after insert on project_auto_update_runs
		deferrable initially deferred for each row execute function pg_temp.arch013_reject_schedule_commit()`); err != nil {
		t.Fatal(err)
	}
	if err = worker.scheduleDue(ctx); err == nil {
		t.Fatal("deferred schedule commit failure was hidden")
	}
	var runCount int
	if err = pool.QueryRow(ctx, `select count(*)::int from project_auto_update_runs`).Scan(&runCount); err != nil || runCount != 0 {
		t.Fatalf("failed schedule run count=%d err=%v; want 0/nil", runCount, err)
	}
	if _, err = pool.Exec(ctx, `drop trigger arch013_reject_schedule_commit on project_auto_update_runs;
		drop function pg_temp.arch013_reject_schedule_commit()`); err != nil {
		t.Fatal(err)
	}
	if err = worker.scheduleDue(ctx); err != nil {
		t.Fatalf("healthy schedule: %v", err)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from project_auto_update_runs`).Scan(&runCount); err != nil || runCount != 1 {
		t.Fatalf("healthy schedule run count=%d err=%v; want 1/nil", runCount, err)
	}

	server := &Server{db: pool}
	if status, body := invokeProjectAutomationOverview(t, server); status != http.StatusOK {
		t.Fatalf("healthy automation overview status=%d body=%s", status, body)
	}
	if _, err = pool.Exec(ctx, `alter table project_auto_update_runs rename to arch013_broken_runs`); err != nil {
		t.Fatal(err)
	}
	status, body := invokeProjectAutomationOverview(t, server)
	if status != http.StatusInternalServerError {
		t.Fatalf("automation overview database failure status=%d body=%s; want 500", status, body)
	}
	if _, err = pool.Exec(ctx, `alter table arch013_broken_runs rename to project_auto_update_runs`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `alter table project_auto_update_settings rename to arch013_broken_settings`); err != nil {
		t.Fatal(err)
	}
	if err = worker.tick(ctx); err == nil {
		t.Fatal("scheduler query failure was hidden by tick")
	}
	if _, err = pool.Exec(ctx, `alter table arch013_broken_settings rename to project_auto_update_settings`); err != nil {
		t.Fatal(err)
	}
	if items, queryErr := server.querySimpleRowsWithContext(ctx, `select * from arch013_missing_table`); queryErr == nil || items != nil {
		t.Fatalf("simple row query failure=(%v,%v), want nil/error", items, queryErr)
	}

	seedWorker := NewSeedCrawlerWorker(config.Load(), pool)
	if _, err = pool.Exec(ctx, `insert into seed_crawler_configs(id,enabled,next_run_at) values(true,true,now()-interval '1 minute')
		on conflict(id) do update set enabled=excluded.enabled,next_run_at=excluded.next_run_at`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create function pg_temp.arch013_reject_seed_commit() returns trigger as $$
		begin raise exception 'injected seed schedule commit failure'; end $$ language plpgsql;
		create constraint trigger arch013_reject_seed_commit after insert on seed_crawler_runs
		deferrable initially deferred for each row execute function pg_temp.arch013_reject_seed_commit()`); err != nil {
		t.Fatal(err)
	}
	if err = seedWorker.scheduleDueRun(ctx); err == nil {
		t.Fatal("deferred seed schedule commit failure was hidden")
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from seed_crawler_runs`).Scan(&runCount); err != nil || runCount != 0 {
		t.Fatalf("failed seed schedule run count=%d err=%v; want 0/nil", runCount, err)
	}
	if _, err = pool.Exec(ctx, `drop trigger arch013_reject_seed_commit on seed_crawler_runs;
		drop function pg_temp.arch013_reject_seed_commit()`); err != nil {
		t.Fatal(err)
	}
	if err = seedWorker.scheduleDueRun(ctx); err != nil {
		t.Fatalf("healthy seed schedule: %v", err)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from seed_crawler_runs`).Scan(&runCount); err != nil || runCount != 1 {
		t.Fatalf("healthy seed schedule run count=%d err=%v; want 1/nil", runCount, err)
	}

	if _, err = pool.Exec(ctx, `alter table seed_crawler_configs rename to arch013_broken_seed_configs`); err != nil {
		t.Fatal(err)
	}
	if err = seedWorker.schedule(ctx); err == nil {
		t.Fatal("seed scheduler query failure was hidden")
	}
	if _, err = pool.Exec(ctx, `alter table arch013_broken_seed_configs rename to seed_crawler_configs`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `alter table seed_crawler_candidates rename to arch013_broken_seed_candidates`); err != nil {
		t.Fatal(err)
	}
	if _, err = seedWorker.executeRun(ctx, 1, security.Claims{}, true); err == nil {
		t.Fatal("seed crawler daily import count failure was hidden")
	}
	if _, err = pool.Exec(ctx, `alter table arch013_broken_seed_candidates rename to seed_crawler_candidates`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `alter table mods rename to arch013_broken_mods`); err != nil {
		t.Fatal(err)
	}
	if _, err = seedWorker.createSeedDraft(ctx, 1, security.Claims{}, "mod", seedModrinthHit{ProjectID: "arch013"}, seedCrawlerRuntimeConfig{}); err == nil {
		t.Fatal("seed crawler project-existence failure was hidden")
	}
	if _, err = pool.Exec(ctx, `alter table arch013_broken_mods rename to mods`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `alter table seed_crawler_translation_tasks rename to arch013_broken_translation_tasks`); err != nil {
		t.Fatal(err)
	}
	if _, err = seedWorker.translateSeedDraft(ctx, 1, []byte(`{}`), 1); err == nil {
		t.Fatal("seed crawler translation-budget failure was hidden")
	}
	if _, err = pool.Exec(ctx, `alter table arch013_broken_translation_tasks rename to seed_crawler_translation_tasks`); err != nil {
		t.Fatal(err)
	}
}

func invokeProjectAutomationOverview(t *testing.T, server *Server) (int, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/project-auto-updates", nil)
	response := httptest.NewRecorder()
	server.adminProjectAutomationOverview(response, request)
	return response.Code, response.Body.String()
}
