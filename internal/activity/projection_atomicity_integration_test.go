package activity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestDurableProjectionFailureDoesNotLosePendingActivityIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := time.Now().UnixNano()
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'synthetic') returning id`, fmt.Sprintf("projection-%d", suffix), fmt.Sprintf("projection-%d@example.invalid", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "delete from activity_event_outbox where user_id=$1", userID)
		_, _ = pool.Exec(context.Background(), "delete from user_activity_events where user_id=$1", userID)
		_, _ = pool.Exec(context.Background(), "delete from users where id=$1", userID)
	}()
	failure := errors.New("synthetic projection failure")
	store := newPostgresStore(pool, func(context.Context, pgx.Tx, []Event) error { return failure }).(*postgresStore)
	if err = store.EnqueueDurable(ctx, Event{UserID: userID, ActionID: ActionCreate, ObjectTypeID: ObjectUser, OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err = store.DrainDurable(ctx, 256)
	if !errors.Is(err, failure) {
		t.Errorf("durable projection error was discarded: %v", err)
	}
	var queued, persisted int
	if err = pool.QueryRow(ctx, "select count(*) from activity_event_outbox where user_id=$1", userID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "select count(*) from user_activity_events where user_id=$1", userID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || persisted != 0 {
		t.Fatalf("failed projection lost retry source or committed partial facts: queued=%d persisted=%d", queued, persisted)
	}
	store.processor = nil
	if _, err = pool.Exec(ctx, "update activity_event_outbox set available_at=now() where user_id=$1", userID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DrainDurable(ctx, 256); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "select count(*) from user_activity_events where user_id=$1", userID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != 1 {
		t.Fatalf("recovery persisted %d facts, want 1", persisted)
	}
}
