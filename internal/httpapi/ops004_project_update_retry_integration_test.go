package httpapi

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestOPS004ProjectUpdateRetryPersistsTerminalAttemptsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project-update retry persistence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table project_update_notification_tasks(
		event_id bigint primary key,status text not null,attempt_count integer not null default 0,
		next_attempt_at timestamptz not null default now(),last_error text not null default '',updated_at timestamptz not null default now());
		insert into project_update_notification_tasks(event_id,status) values(42,'pending')`); err != nil {
		t.Fatal(err)
	}
	worker := &ProjectUpdateNotificationWorker{db: pool}
	for attempt := 1; attempt <= projectUpdateNotificationMaxAttempts; attempt++ {
		if err = worker.retry(ctx, 42, errors.New("permanent template error")); err != nil {
			t.Fatal(err)
		}
		var gotAttempt int
		var status string
		if err = pool.QueryRow(ctx, `select attempt_count,status from project_update_notification_tasks where event_id=42`).Scan(&gotAttempt, &status); err != nil {
			t.Fatal(err)
		}
		wantStatus := "pending"
		if attempt == projectUpdateNotificationMaxAttempts {
			wantStatus = "failed"
		}
		if gotAttempt != attempt || status != wantStatus {
			t.Fatalf("after retry %d task=(attempt=%d,status=%s), want (%d,%s)", attempt, gotAttempt, status, attempt, wantStatus)
		}
	}
	if err = worker.retry(ctx, 42, errors.New("duplicate terminal delivery")); err != nil {
		t.Fatal(err)
	}
	var finalAttempt int
	if err = pool.QueryRow(ctx, `select attempt_count from project_update_notification_tasks where event_id=42 and status='failed'`).Scan(&finalAttempt); err != nil {
		t.Fatal(err)
	}
	if finalAttempt != projectUpdateNotificationMaxAttempts {
		t.Fatalf("terminal retry changed attempt_count to %d", finalAttempt)
	}

	if _, err = pool.Exec(ctx, `insert into project_update_notification_tasks(event_id,status) values(43,'pending');
		alter table project_update_notification_tasks add constraint reject_retry check(event_id<>43 or attempt_count=0)`); err != nil {
		t.Fatal(err)
	}
	if err = worker.retry(ctx, 43, errors.New("retry persistence failure")); err == nil {
		t.Fatal("retry persistence failure was swallowed")
	}
	var failedAttempt int
	var failedStatus string
	if err = pool.QueryRow(ctx, `select attempt_count,status from project_update_notification_tasks where event_id=43`).Scan(&failedAttempt, &failedStatus); err != nil {
		t.Fatal(err)
	}
	if failedAttempt != 0 || failedStatus != "pending" {
		t.Fatalf("failed retry partially changed task=(%d,%s)", failedAttempt, failedStatus)
	}
}
