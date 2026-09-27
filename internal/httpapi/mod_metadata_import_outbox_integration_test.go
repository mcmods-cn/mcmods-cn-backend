package httpapi

import (
	"context"
	"testing"
	"time"
)

func TestModMetadataImportRecoveryUsesOnlyTransactionalEnvelopes(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `create temp table mod_metadata_import_jobs(
		public_id text primary key,status text not null,progress integer not null default 0,error text not null default '',
		started_at timestamptz,updated_at timestamptz not null,created_at timestamptz not null default now());
		create temp table nats_outbox(
		id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
		subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',
		payload jsonb not null,occurred_at timestamptz not null default now(),status text not null default 'pending',
		available_at timestamptz not null default now(),created_at timestamptz not null default now());
		insert into mod_metadata_import_jobs(public_id,status,updated_at) values
			('metadata-orphan','queued',now()),
			('metadata-covered','queued',now()),
			('metadata-stale','running',now()-interval '10 minutes'),
			('metadata-fresh','running',now());
		insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload,status,occurred_at)
		values('covered-event','mod.metadata.import.requested','mod_metadata_import','mod_metadata_import_job','metadata-covered','{}','published',now())`); err != nil {
		t.Fatal(err)
	}
	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := recoverModMetadataImportOutboxTx(ctx, tx, 5*time.Minute, 200)
	if err != nil || recovered != 2 {
		t.Fatalf("metadata recovery = %d/%v, want 2", recovered, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertModMetadataRecoveryState(t, ctx, connection, "metadata-orphan", "queued", 1)
	assertModMetadataRecoveryState(t, ctx, connection, "metadata-stale", "queued", 1)
	assertModMetadataRecoveryState(t, ctx, connection, "metadata-covered", "queued", 1)
	assertModMetadataRecoveryState(t, ctx, connection, "metadata-fresh", "running", 0)

	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered, err = recoverModMetadataImportOutboxTx(ctx, tx, 5*time.Minute, 200); err != nil || recovered != 0 {
		t.Fatalf("duplicate metadata recovery = %d/%v", recovered, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = connection.Exec(ctx, `insert into mod_metadata_import_jobs(public_id,status,updated_at) values('metadata-broken','running',now()-interval '10 minutes');
		alter table nats_outbox add constraint reject_metadata_recovery check(aggregate_id<>'metadata-broken')`); err != nil {
		t.Fatal(err)
	}
	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recoverModMetadataImportOutboxTx(ctx, tx, 5*time.Minute, 200); err == nil {
		t.Fatal("metadata recovery event failure was not returned")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertModMetadataRecoveryState(t, ctx, connection, "metadata-broken", "running", 0)
}

func assertModMetadataRecoveryState(t *testing.T, ctx context.Context, queryer modExportAttemptQueryer, jobID, wantStatus string, wantEvents int) {
	t.Helper()
	var status string
	var events int
	if err := queryer.QueryRow(ctx, `select status from mod_metadata_import_jobs where public_id=$1`, jobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := queryer.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_type='mod_metadata_import_job' and aggregate_id=$1`, jobID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || events != wantEvents {
		t.Fatalf("metadata job %s = %s/%d, want %s/%d", jobID, status, events, wantStatus, wantEvents)
	}
}
