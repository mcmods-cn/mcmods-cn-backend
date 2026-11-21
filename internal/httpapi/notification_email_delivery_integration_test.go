package httpapi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/mailer"
	"mcmods-cn-backend/internal/queue"
)

func TestEmailReservationSuppressesDuplicateAndUncertainSendsIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `create table processed_events(consumer text,event_id text,event_type text,processed_at timestamptz default now(),primary key(consumer,event_id))`); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	active := queue.WithEventID(ctx, "owned-active")
	start, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- deliverNotificationEmail(active, pool, func() error {
			calls.Add(1)
			close(start)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		})
	}()
	select {
	case <-start:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	send := func() error { calls.Add(1); return nil }
	if err := deliverNotificationEmail(active, pool, send); !errors.Is(err, mailer.ErrAcceptanceUnknown) {
		t.Fatalf("concurrent send was not held: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := deliverNotificationEmail(active, pool, send); err != nil || calls.Load() != 1 {
		t.Fatalf("ack replay result=%v SMTP calls=%d", err, calls.Load())
	}
	ambiguous := queue.WithEventID(ctx, "owned-ambiguous")
	if err := deliverNotificationEmail(ambiguous, pool, func() error { calls.Add(1); return mailer.ErrAcceptanceUnknown }); !errors.Is(err, mailer.ErrAcceptanceUnknown) {
		t.Fatal(err)
	}
	if err := deliverNotificationEmail(ambiguous, pool, send); !errors.Is(err, mailer.ErrAcceptanceUnknown) || calls.Load() != 2 {
		t.Fatalf("unknown outcome sent again: calls=%d err=%v", calls.Load(), err)
	}
	retry := queue.WithEventID(ctx, "owned-rejected")
	denied := errors.New("synthetic explicit SMTP refusal")
	if err := deliverNotificationEmail(retry, pool, func() error { calls.Add(1); return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if err := deliverNotificationEmail(retry, pool, send); err != nil || calls.Load() != 4 {
		t.Fatalf("known refusal could not retry: calls=%d err=%v", calls.Load(), err)
	}
}
