package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestFavoriteExportWorkerRejectsReportCountDriftBeforeUploadIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify export result integrity")
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
		create temp table favorite_modpack_export_tasks(
			id bigserial primary key,public_id text not null unique,owner_user_id bigint not null,
			pack_name text not null,pack_version_id text not null,minecraft_version text not null,loader_type text not null,
			loader_version text not null,status text not null,stage text not null,attempt_count integer not null default 0,
			lease_token text not null default '',lease_expires_at timestamptz,started_at timestamptz,finished_at timestamptz,
			expires_at timestamptz,updated_at timestamptz not null default now(),created_at timestamptz not null default now(),
			result_file_id bigint,collection_item_count integer not null default 0,exported_mod_count integer not null default 0,
			auto_dependency_count integer not null default 0,skipped_item_count integer not null default 0,
			failed_item_count integer not null default 0,final_file_count integer not null default 0,
			result_file_size bigint not null default 0,result_sha256 text not null default '',error_code text not null default '',
			error_detail text not null default ''
		);
		create temp table favorite_modpack_export_items(
			id bigserial primary key,task_id bigint not null,result_type text not null,selected_file_name text not null default '',
			sha1 text not null default '',sha512 text not null default '',env_client text not null default '',
			env_server text not null default '',download_url text not null default '',file_size bigint not null default 0
		);
		insert into favorite_modpack_export_tasks(public_id,owner_user_id,pack_name,pack_version_id,minecraft_version,
			loader_type,loader_version,status,stage,collection_item_count,exported_mod_count,final_file_count)
		values('bug069001',7,'BUG-069 pack','1.0.0','1.21.1','fabric','0.16.14','pending','queued',2,2,2);
		insert into favorite_modpack_export_items(task_id,result_type,selected_file_name,sha1,sha512,env_client,env_server,download_url,file_size)
		values(1,'exported','only-one.jar',repeat('1',40),repeat('2',128),'required','required',
			'https://cdn.modrinth.com/data/project/versions/version/only-one.jar',123)
	`); err != nil {
		t.Fatal(err)
	}

	worker := NewFavoriteModpackExportWorker(config.Config{}, pool, nil)
	err = worker.process(ctx, "bug069001")
	if err == nil || !strings.Contains(err.Error(), "favorite export result counts") {
		t.Fatalf("partial report result = %v", err)
	}
	var status, stage, code, leaseToken string
	if err = pool.QueryRow(ctx, `select status,stage,error_code,lease_token from favorite_modpack_export_tasks where public_id='bug069001'`).
		Scan(&status, &stage, &code, &leaseToken); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || stage != "retry_wait" || code != "REPORT_INCONSISTENT" || leaseToken != "" {
		t.Fatalf("partial report state = %s/%s/%s lease=%q", status, stage, code, leaseToken)
	}
}
