package queue

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestLateOutboxFailureCannotOverwriteNewOwnerIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires the dedicated PostgreSQL test cluster")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := EnqueueTx(ctx, tx, "audit_fence", "audit.created", "audit_test", randomEventID(), "", map[string]bool{"synthetic": true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), "delete from nats_outbox where event_id=$1", eventID) }()
	var record outboxRecord
	if err = pool.QueryRow(ctx, `update nats_outbox set status='publishing',locked_by='new-owner',attempts=2 where event_id=$1 returning id,max_attempts`, eventID).Scan(&record.ID, &record.MaxAttempts); err != nil {
		t.Fatal(err)
	}
	record.Attempts = 0
	dispatcher := NewOutboxDispatcher(pool, nil, true)
	dispatcher.workerID = "old-owner"
	if err := dispatcher.fail(ctx, record, errors.New("late synthetic failure")); err != nil {
		t.Fatal(err)
	}
	var status, owner string
	if err = pool.QueryRow(ctx, "select status,locked_by from nats_outbox where event_id=$1", eventID).Scan(&status, &owner); err != nil {
		t.Fatal(err)
	}
	if status != "publishing" || owner != "new-owner" {
		t.Fatalf("late worker overwrote current owner: status=%q owner=%q", status, owner)
	}
	if metrics := dispatcher.Metrics(); metrics.Failed != 0 || metrics.Retried != 0 || metrics.Dead != 0 {
		t.Fatalf("stale failure counted as persisted transition: %+v", metrics)
	}
}
