package queue

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestOutboxLongUnicodeErrorCanPersistRetryAndDeadLetterIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires owned PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dispatcher := NewOutboxDispatcher(pool, nil, true)
	for _, maximum := range []int{1, 8} {
		t.Run(strings.Repeat("attempt", maximum), func(t *testing.T) {
			event := randomEventID()
			var record outboxRecord
			if err := pool.QueryRow(ctx, `insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload,status,locked_by,attempts,max_attempts)
values($1,'audit.error','audit_error','audit',$1,'{}','publishing',$2,1,$3) returning id,max_attempts`, event, dispatcher.workerID, maximum).Scan(&record.ID, &record.MaxAttempts); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
				defer done()
				for _, table := range []string{"dead_letter_events", "nats_outbox"} {
					if _, err := pool.Exec(cleanup, "delete from "+table+" where event_id=$1", event); err != nil {
						t.Error(err)
					}
				}
			})
			if err := dispatcher.fail(ctx, record, errors.New(strings.Repeat("界", 400))); err != nil {
				t.Fatalf("Unicode provider error prevented retry state: %v", err)
			}
			var message, status string
			if err := pool.QueryRow(ctx, `select last_error,status from nats_outbox where event_id=$1`, event).Scan(&message, &status); err != nil {
				t.Fatal(err)
			}
			if len(message) > 1000 || !utf8.ValidString(message) || maximum == 1 && status != "dead" || maximum == 8 && status != "failed" {
				t.Fatalf("invalid persisted retry state: bytes=%d status=%s", len(message), status)
			}
		})
	}
}
