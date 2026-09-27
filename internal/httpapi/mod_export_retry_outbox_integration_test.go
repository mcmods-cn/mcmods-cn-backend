package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestModExportAttemptEntrypointsUseTransactionalOutbox(t *testing.T) {
	for _, name := range []string{
		"mod_export_handlers.go",
		"mod_embedded_icon_import.go",
		"modid_validation.go",
		"mod_export_job_lifecycle.go",
	} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		if strings.Contains(strings.ToLower(text), "insert into nats_outbox") {
			t.Fatalf("%s bypasses the shared transactional attempt helper", name)
		}
		if !strings.Contains(text, "enqueueModExportAttemptTx") {
			t.Fatalf("%s does not use the shared transactional attempt helper", name)
		}
	}
	lifecycle, err := os.ReadFile("mod_export_job_lifecycle.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(lifecycle)
	for _, required := range []string{
		"recoverStaleModExportJobsTx(ctx, tx)",
		"resetModExportJobForRetryTx(ctx, tx",
		"queue.EnqueueTx(ctx, tx",
		"returning id",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("retry/recovery transaction contract is missing %q", required)
		}
	}
}

func TestModExportWorkerHasNoCoreNATSOrGoroutineTaskPath(t *testing.T) {
	for _, name := range []string{"mod_export_handlers.go", "mod_export_job_lifecycle.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		for _, forbidden := range []string{"OutboxEnabled", "PublishTask", "dispatchModExportJob", "go func(id string)"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s retains legacy task path %q", name, forbidden)
			}
		}
	}
	runtimeSource, err := os.ReadFile("../app/runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runtimeSource), "queue.NewOutboxDispatcher(db, queueClient, true)") {
		t.Fatal("application runtime can still disable the PostgreSQL outbox dispatcher")
	}
}

func TestModExportRetryAndRecoveryCreateAtomicOutboxAttempts(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `create temp table catalog_import_jobs(
		id text primary key,mod_id bigint not null,status text not null,progress integer not null default 0,
		current_stage text not null default '',error_code text not null default '',error_detail jsonb not null default '{}'::jsonb,
		created_by bigint,started_at timestamptz,finished_at timestamptz,heartbeat_at timestamptz,
		run_token text not null default '',updated_at timestamptz not null default now());
		create temp table nats_outbox(
		id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
		subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',
		payload jsonb not null,occurred_at timestamptz not null default now(),status text not null default 'pending',
		available_at timestamptz not null default now(),created_at timestamptz not null default now());
		insert into catalog_import_jobs(id,mod_id,status,created_by) values
			('retry-job',11,'failed',1),
			('stale-job',11,'importing',1),
			('fresh-job',11,'validating',1);
		update catalog_import_jobs set heartbeat_at=now()-interval '10 minutes',updated_at=now()-interval '10 minutes'
			where id='stale-job';
		update catalog_import_jobs set heartbeat_at=now(),updated_at=now() where id='fresh-job'`); err != nil {
		t.Fatal(err)
	}

	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := resetModExportJobForRetryTx(ctx, tx, "retry-job", 11, 22)
	if err != nil || !retried {
		t.Fatalf("retry reset = %v/%v", retried, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertModExportAttemptState(t, ctx, connection, "retry-job", "queued", "mod.catalog_import.retried", 1)

	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	retried, err = resetModExportJobForRetryTx(ctx, tx, "retry-job", 11, 22)
	if err != nil || retried {
		t.Fatalf("duplicate retry reset = %v/%v", retried, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertModExportAttemptState(t, ctx, connection, "retry-job", "queued", "mod.catalog_import.retried", 1)

	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := recoverStaleModExportJobsTx(ctx, tx)
	if err != nil || recovered != 1 {
		t.Fatalf("stale recovery = %d/%v", recovered, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertModExportAttemptState(t, ctx, connection, "stale-job", "queued", "mod.catalog_import.recovered", 1)
	assertModExportAttemptState(t, ctx, connection, "fresh-job", "validating", "mod.catalog_import.recovered", 0)
	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err = recoverStaleModExportJobsTx(ctx, tx)
	if err != nil || recovered != 0 {
		t.Fatalf("duplicate stale recovery = %d/%v", recovered, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertModExportAttemptState(t, ctx, connection, "stale-job", "queued", "mod.catalog_import.recovered", 1)

	if _, err = connection.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,status,created_by)
		values('outbox-failure-job',11,'failed',1);
		alter table nats_outbox add constraint reject_outbox_failure_job check(aggregate_id<>'outbox-failure-job')`); err != nil {
		t.Fatal(err)
	}
	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	retried, err = resetModExportJobForRetryTx(ctx, tx, "outbox-failure-job", 11, 22)
	if err == nil || retried {
		t.Fatalf("outbox failure reset = %v/%v", retried, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertModExportAttemptState(t, ctx, connection, "outbox-failure-job", "failed", "mod.catalog_import.retried", 0)
}

type modExportAttemptQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func assertModExportAttemptState(t *testing.T, ctx context.Context, queryer modExportAttemptQueryer, jobID, wantStatus, eventType string, wantEvents int) {
	t.Helper()
	var status string
	var events int
	if err := queryer.QueryRow(ctx, `select status from catalog_import_jobs where id=$1`, jobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := queryer.QueryRow(ctx, `select count(*) from nats_outbox
		where aggregate_type='mod_export_job' and aggregate_id=$1 and event_type=$2
		and payload->>'jobId'=$1`, jobID, eventType).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || events != wantEvents {
		t.Fatalf("job %s state/events = %s/%d, want %s/%d", jobID, status, events, wantStatus, wantEvents)
	}
}

func TestDuplicateModExportDeliveryIsAcknowledged(t *testing.T) {
	if err := modExportDeliveryResult(errModExportLeaseLost); err != nil {
		t.Fatalf("duplicate delivery returned %v", err)
	}
	want := errors.New("worker failed")
	if err := modExportDeliveryResult(want); !errors.Is(err, want) {
		t.Fatalf("worker failure changed to %v", err)
	}
}
