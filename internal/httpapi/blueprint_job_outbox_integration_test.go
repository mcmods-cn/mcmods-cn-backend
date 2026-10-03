package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBlueprintJobsUseOneTransactionalOutboxPath(t *testing.T) {
	source, err := os.ReadFile("blueprint_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, forbidden := range []string{"OutboxEnabled", "PublishTask"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("blueprint producer retains alternate queue path %q", forbidden)
		}
	}
	if count := strings.Count(text, "insert into blueprint_jobs("); count != 1 {
		t.Fatalf("blueprint job insert has %d implementations, want one", count)
	}

	start := strings.Index(text, "func (s *Server) completeBlueprintUpload(")
	end := strings.Index(text, "func int64Value(")
	if start < 0 || end <= start {
		t.Fatal("blueprint upload completion boundary was not found")
	}
	upload := text[start:end]
	queued := strings.Index(upload, "enqueueBlueprintJobTx(ctx, tx")
	committed := strings.LastIndex(upload, "tx.Commit(ctx)")
	if queued < 0 || committed < 0 || queued > committed {
		t.Fatal("upload completion does not atomically enqueue the normalization job and outbox event")
	}

	start = strings.Index(text, "func (s *Server) enqueueBlueprintJob(")
	end = strings.Index(text, "func (s *Server) userOwnsPendingBlueprintObject(")
	if start < 0 || end <= start {
		t.Fatal("blueprint enqueue wrapper boundary was not found")
	}
	wrapper := text[start:end]
	queued = strings.Index(wrapper, "enqueueBlueprintJobTx(ctx, tx")
	committed = strings.LastIndex(wrapper, "tx.Commit(ctx)")
	if queued < 0 || committed < 0 || queued > committed {
		t.Fatal("conversion/retry does not use the shared transactional enqueue helper")
	}
}

func TestBlueprintJobAndOutboxCommitOrRollbackTogether(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `create temp table blueprint_jobs(
		id bigserial primary key,public_id text not null unique default md5(random()::text),blueprint_id bigint not null,
		operation text not null,target_format text not null default '',created_by bigint not null,status text not null default 'queued');
		create temp table nats_outbox(
		id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
		subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',
		payload jsonb not null,occurred_at timestamptz not null default now(),status text not null default 'pending',
		available_at timestamptz not null default now(),created_at timestamptz not null default now())`); err != nil {
		t.Fatal(err)
	}

	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	jobPublicID, err := enqueueBlueprintJobTx(ctx, tx, 41, 7, "normalize", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var jobs, events int
	var payloadJobID int64
	if err = connection.QueryRow(ctx, `select count(*) from blueprint_jobs where public_id=$1`, jobPublicID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err = connection.QueryRow(ctx, `select count(*),coalesce(max((payload->>'jobId')::bigint),0)
		from nats_outbox where subject='blueprint_convert' and event_type='blueprint.conversion.requested'
		and aggregate_type='blueprint_job' and aggregate_id=$1`, jobPublicID).Scan(&events, &payloadJobID); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || events != 1 || payloadJobID <= 0 {
		t.Fatalf("committed blueprint job/outbox = %d/%d (payload job %d), want 1/1 with an id", jobs, events, payloadJobID)
	}

	if _, err = connection.Exec(ctx, `alter table nats_outbox add constraint reject_second_blueprint_event check(payload->>'jobId'<>'2')`); err != nil {
		t.Fatal(err)
	}
	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	if _, err = enqueueBlueprintJobTx(ctx, tx, 42, 7, "convert", "schem"); err == nil {
		t.Fatal("outbox constraint failure was not returned")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = connection.QueryRow(ctx, `select count(*) from blueprint_jobs where blueprint_id=42`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("failed outbox left %d blueprint jobs, want zero", jobs)
	}
}
