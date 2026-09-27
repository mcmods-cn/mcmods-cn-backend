package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestFavoriteExportReportAndWorkerFailuresAreObservableIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify export failure observability")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err = pool.Exec(ctx, `
		create temp table favorite_collections(id bigint primary key,public_id text not null);
		create temp table public_routes(id bigint primary key,public_id text not null);
		create temp table oss_files(id bigint primary key);
		create temp table favorite_modpack_export_tasks(
			id bigserial primary key,public_id text not null unique,owner_user_id bigint not null,collection_id bigint,
			collection_public_id_snapshot text not null,
			pack_name text not null,pack_version_id text not null,minecraft_version text not null,loader_type text not null,
			loader_version text not null,allow_compatible_only boolean not null default false,report_version integer not null default 1,
			status text not null,stage text not null,attempt_count integer not null default 0,
			lease_token text not null default '',lease_expires_at timestamptz,started_at timestamptz,finished_at timestamptz,
			expires_at timestamptz,updated_at timestamptz not null default now(),created_at timestamptz not null default now(),
			result_file_id bigint,collection_item_count integer not null default 0,exported_mod_count integer not null default 0,
			auto_dependency_count integer not null default 0,skipped_item_count integer not null default 0,
			failed_item_count integer not null default 0,final_file_count integer not null default 0,
			result_file_size bigint not null default 0,result_sha256 text not null default '',error_code text not null default '',
			error_detail text not null default ''
		);
		create temp table favorite_modpack_export_items(
			id bigserial primary key,task_id bigint not null,source_project_route_id bigint,source_project_type text not null,
			source_project_name_snapshot text not null,result_type text not null,reason_code text not null default '',
			reason_detail text not null default '',modrinth_project_id text not null default '',modrinth_version_id text not null default '',
			selected_version_name text not null default '',selected_file_name text not null default '',minecraft_version text not null default '',
			loader text not null default '',release_type text not null default '',env_client text not null default '',env_server text not null default '',
			file_size text not null,sha1 text not null default '',sha512 text not null default '',download_url text not null default '',
			dependency_of jsonb not null
		);
		insert into favorite_collections values(1,'arch018c1');
		insert into favorite_modpack_export_tasks(public_id,owner_user_id,collection_id,collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,
			loader_type,loader_version,status,stage,collection_item_count,exported_mod_count,final_file_count)
		values('arch01801',7,1,'arch018c1','ARCH-018 pack','1.0.0','1.20.1','forge','47.4.10','pending','queued',1,1,1);
		insert into favorite_modpack_export_items(task_id,source_project_type,source_project_name_snapshot,result_type,
			selected_file_name,file_size,dependency_of)
		values(1,'mod','Corrupt report item','exported','example.jar','1','"corrupt"'::jsonb)
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/favorites/modpack-exports/arch01801", nil)
	request.SetPathValue("taskId", "arch01801")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 7}))
	response := httptest.NewRecorder()
	server.favoriteModpackExportDetail(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt dependency report status = %d, body=%s", response.Code, response.Body.String())
	}

	if _, err = pool.Exec(ctx, `update favorite_modpack_export_items set file_size='broken',dependency_of='[]'::jsonb`); err != nil {
		t.Fatal(err)
	}
	worker := NewFavoriteModpackExportWorker(config.Config{}, pool, nil)
	if err = worker.process(ctx, "arch01801"); err == nil {
		t.Fatal("worker silently skipped an item Scan failure")
	}
	var status, stage, code, leaseToken string
	if err = pool.QueryRow(ctx, `select status,stage,error_code,lease_token from favorite_modpack_export_tasks where public_id='arch01801'`).Scan(&status, &stage, &code, &leaseToken); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || stage != "retry_wait" || code != "REPORT_LOAD_FAILED" || leaseToken != "" {
		t.Fatalf("Scan failure state = %s/%s/%s lease=%q", status, stage, code, leaseToken)
	}

	if _, err = pool.Exec(ctx, `
		update favorite_modpack_export_tasks set status='processing',stage='building',attempt_count=1,
			lease_token='failure-lease',lease_expires_at=now()+interval '1 minute',error_code='',error_detail=''
		where public_id='arch01801';
		create function pg_temp.reject_arch018_failure_state() returns trigger language plpgsql as $$
		begin
			if new.stage='retry_wait' then raise exception 'reject ARCH-018 failure state'; end if;
			return new;
		end $$;
		create trigger reject_arch018_failure_state before update on favorite_modpack_export_tasks
		for each row execute function pg_temp.reject_arch018_failure_state()
	`); err != nil {
		t.Fatal(err)
	}
	failureCause := errors.New("simulated export processing failure")
	failureErr := worker.fail(ctx, "arch01801", "failure-lease", "SIMULATED_FAILURE", failureCause)
	if failureErr == nil || !errors.Is(failureErr, failureCause) || !strings.Contains(failureErr.Error(), "persist favorite export retry state") {
		t.Fatalf("failure persistence error = %v", failureErr)
	}
	if err = pool.QueryRow(ctx, `select status,stage,error_code,lease_token from favorite_modpack_export_tasks where public_id='arch01801'`).Scan(&status, &stage, &code, &leaseToken); err != nil {
		t.Fatal(err)
	}
	if status != "processing" || stage != "building" || code != "" || leaseToken != "failure-lease" {
		t.Fatalf("rejected failure state changed task = %s/%s/%s lease=%q", status, stage, code, leaseToken)
	}
	if _, err = pool.Exec(ctx, `drop trigger reject_arch018_failure_state on favorite_modpack_export_tasks`); err != nil {
		t.Fatal(err)
	}
	if failureErr = worker.fail(ctx, "arch01801", "wrong-lease", "SIMULATED_FAILURE", failureCause); failureErr == nil || !strings.Contains(failureErr.Error(), "load favorite export failure attempt for lease") {
		t.Fatalf("lost-lease failure result = %v", failureErr)
	}

	if _, err = pool.Exec(ctx, `alter table favorite_modpack_export_tasks rename column created_at to arch018_broken_created_at`); err != nil {
		t.Fatal(err)
	}
	if err = worker.processPending(ctx); err == nil {
		t.Fatal("worker pending scan hid a database query failure")
	}
}
