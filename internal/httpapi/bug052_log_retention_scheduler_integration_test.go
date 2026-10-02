package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBUG052LogRetentionWorkerRunsThePersistedPolicyIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify automatic log retention")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	db, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	ctx := context.Background()
	if _, err = db.Exec(ctx, `
		create temp table system_settings(key text primary key,value jsonb not null);
		create temp table app_logs(id bigserial primary key,category text not null,created_at timestamptz not null default now());
		create temp table permission_audit_logs(id bigserial primary key,created_at timestamptz not null default now());
		create temp table user_login_logs(id bigserial primary key,created_at timestamptz not null default now());
		create temp table oss_upload_logs(id bigserial primary key,created_at timestamptz not null default now());
		create temp table oss_scan_logs(id bigserial primary key,created_at timestamptz not null default now())`); err != nil {
		t.Fatal(err)
	}
	storeBUG052LogPolicy(t, ctx, db, true)
	if _, err = db.Exec(ctx, `
		insert into app_logs(category,created_at)
		select 'api_access',now()-interval '10 days' from generate_series(1,1005);
		insert into app_logs(category,created_at) values('api_access',now());
		insert into permission_audit_logs(created_at) values(now()-interval '10 days'),(now());
		insert into user_login_logs(created_at) values(now()-interval '10 days'),(now());
		insert into oss_upload_logs(created_at) values(now()-interval '10 days'),(now());
		insert into oss_scan_logs(created_at) values(now()-interval '10 days'),(now())`); err != nil {
		t.Fatal(err)
	}

	worker := NewLogRetentionWorker(db)
	deleted, err := worker.pruneWithError(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for label, expected := range map[string]int64{
		"api_access": 1005, "permission_change": 1, "login_security": 1, "file_upload": 1, "file_scan": 1,
	} {
		if deleted[label] != expected {
			t.Errorf("deleted[%s] = %d, want %d (all=%v)", label, deleted[label], expected, deleted)
		}
	}
	for _, table := range []string{"app_logs", "permission_audit_logs", "user_login_logs", "oss_upload_logs", "oss_scan_logs"} {
		var remaining int
		if err = db.QueryRow(ctx, `select count(*) from `+table).Scan(&remaining); err != nil || remaining != 1 {
			t.Errorf("fresh rows in %s = %d/%v, want 1", table, remaining, err)
		}
	}

	storeBUG052LogPolicy(t, ctx, db, false)
	if _, err = db.Exec(ctx, `insert into app_logs(category,created_at) values('api_access',now()-interval '10 days')`); err != nil {
		t.Fatal(err)
	}
	if deleted, err = worker.pruneWithError(ctx); err != nil || deleted["api_access"] != 0 {
		t.Fatalf("disabled retention deleted rows: %v/%v", deleted, err)
	}
	assertBUG052OldAPILogCount(t, ctx, db, 1)

	storeBUG052LogPolicy(t, ctx, db, true)
	lockPool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lockPool.Close)
	lockConnection, err := lockPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lockConnection.Release()
	if _, err = lockConnection.Exec(ctx, `select pg_advisory_lock(hashtext('mcmods-log-retention'))`); err != nil {
		t.Fatal(err)
	}
	if deleted, err = worker.pruneWithError(ctx); err != nil || deleted["api_access"] != 0 {
		t.Fatalf("worker bypassed another instance's lease: %v/%v", deleted, err)
	}
	assertBUG052OldAPILogCount(t, ctx, db, 1)
	if _, err = lockConnection.Exec(ctx, `select pg_advisory_unlock(hashtext('mcmods-log-retention'))`); err != nil {
		t.Fatal(err)
	}
	if deleted, err = worker.pruneWithError(ctx); err != nil || deleted["api_access"] != 1 {
		t.Fatalf("released lease did not restore cleanup: %v/%v", deleted, err)
	}
	assertBUG052OldAPILogCount(t, ctx, db, 0)
}

func storeBUG052LogPolicy(t *testing.T, ctx context.Context, db *pgxpool.Pool, enabled bool) {
	t.Helper()
	raw, err := json.Marshal(logRetentionConfig{
		Enabled: enabled, DefaultDays: 1,
		CategoryDays: map[string]int{
			"api_access": 1, "permission_change": 1, "login_security": 1, "file_upload": 1, "file_scan": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into system_settings(key,value) values('logs.retention',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, raw); err != nil {
		t.Fatal(err)
	}
}

func assertBUG052OldAPILogCount(t *testing.T, ctx context.Context, db *pgxpool.Pool, expected int) {
	t.Helper()
	var count int
	if err := db.QueryRow(ctx, `select count(*) from app_logs where created_at<now()-interval '1 day'`).Scan(&count); err != nil || count != expected {
		t.Fatalf("old API log count = %d/%v, want %d", count, err, expected)
	}
}
