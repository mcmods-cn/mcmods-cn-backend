package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestGeneratedOSSObjectRegistrationFailureQueuesDurableCompensationIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify generated OSS compensation")
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

	var requestMu sync.Mutex
	putRequests, deleteRequests := 0, 0
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestMu.Lock()
		switch request.Method {
		case http.MethodPut:
			putRequests++
		case http.MethodDelete:
			deleteRequests++
		}
		requestMu.Unlock()
		if request.Method == http.MethodDelete {
			response.Header().Set("Content-Type", "application/xml")
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(response, `<Error><Code>ServiceUnavailable</Code><Message>retry</Message></Error>`)
			return
		}
		response.Header().Set("ETag", `"arch021"`)
		response.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()

	if _, err = pool.Exec(ctx, `
		create temp table system_settings(key text primary key,value jsonb not null);
		create temp table oss_files(
			id bigserial primary key,bucket text not null,endpoint text not null,region text not null,object_key text not null unique,
			category text not null,source text not null,original_name text not null,source_original_name text not null,
			content_type text not null,size_bytes bigint not null,source_size_bytes bigint not null,sha256 text not null,
			uploader_id bigint,status text not null,scan_status text not null,updated_at timestamptz not null default now()
		);
		create temp table oss_object_deletion_outbox(
			id bigserial primary key,oss_file_id bigint,bucket text not null,endpoint text not null,region text not null,
			use_cname boolean not null default false,object_key text not null,reason text not null default '',
			status text not null default 'pending',attempts integer not null default 0,max_attempts integer not null default 12,
			next_attempt_at timestamptz not null default now(),locked_at timestamptz,locked_by text not null default '',
			last_error text not null default '',failure_class text not null default '',dead_at timestamptz,deleted_at timestamptz,
			created_at timestamptz not null default now(),updated_at timestamptz not null default now(),
			unique(bucket,endpoint,object_key)
		);
		create function pg_temp.reject_arch021_generated_registration() returns trigger language plpgsql as $$
		begin
			if new.object_key like 'generated/fails-%' then
				raise exception 'ARCH-021 registration failure';
			end if;
			return new;
		end $$;
		create trigger reject_arch021_generated_registration before insert or update on oss_files
			for each row execute function pg_temp.reject_arch021_generated_registration();
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Load()}
	ossConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: provider.URL, PublicEndpoint: "https://public.example.test",
		Bucket: "generated-bucket", AccessKeyID: "arch021-key", AccessKeySecret: "arch021-secret", UseCName: true, Prefix: "mcmods",
	}
	sealed, err := server.sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}

	healthyID, err := server.writeGeneratedOSSObject(ctx, "generated/healthy.bin", "healthy.bin", "application/octet-stream", []byte("healthy"), 7, "arch021_test")
	if err != nil || healthyID <= 0 {
		t.Fatalf("healthy generated object = %d/%v", healthyID, err)
	}
	_, err = server.writeGeneratedOSSObject(ctx, "generated/fails-registration.bin", "failed.bin", "application/octet-stream", []byte("failed"), 7, "arch021_test")
	if err == nil || !strings.Contains(err.Error(), "ARCH-021 registration failure") {
		t.Fatalf("registration failure = %v", err)
	}
	var queued, withFile int
	var reason string
	if err = pool.QueryRow(ctx, `select count(*),count(oss_file_id),coalesce(max(reason),'')
		from oss_object_deletion_outbox where object_key='generated/fails-registration.bin'`).Scan(&queued, &withFile, &reason); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || withFile != 0 || reason != "generated-object-registration-failed" {
		t.Fatalf("durable compensation = %d rows/%d file refs/reason %q", queued, withFile, reason)
	}

	if _, err = pool.Exec(ctx, `alter table oss_object_deletion_outbox rename column reason to arch021_broken_reason`); err != nil {
		t.Fatal(err)
	}
	_, err = server.writeGeneratedOSSObject(ctx, "generated/fails-compensation.bin", "failed-again.bin", "application/octet-stream", []byte("failed again"), 7, "arch021_test")
	if err == nil || !strings.Contains(err.Error(), "ARCH-021 registration failure") || !strings.Contains(err.Error(), "queue generated OSS cleanup") {
		t.Fatalf("registration plus compensation failure = %v", err)
	}
	metrics := server.ossWrites.snapshot()
	if metrics.DeletionEnqueueFailures != 1 {
		t.Fatalf("compensation enqueue metric = %+v", metrics)
	}
	requestMu.Lock()
	puts, deletes := putRequests, deleteRequests
	requestMu.Unlock()
	if puts != 3 || deletes != 0 {
		t.Fatalf("provider requests = %d PUT/%d DELETE; cleanup must be persisted before execution", puts, deletes)
	}
}
