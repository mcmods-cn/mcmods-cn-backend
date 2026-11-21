package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestLocalOutboxDeliversNotificationsPastBlockedImports(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	client := New(ctx, config.NATSConfig{Tasks: []config.NATSTaskConfig{
		{Code: "mod_export_import", Enabled: true, MaxConcurrent: 1, TimeoutSeconds: 8},
		{Code: "notifications", Enabled: true, MaxConcurrent: 1, TimeoutSeconds: 2},
		{Code: "ai", Enabled: false},
	}})
	defer client.Close()
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var imports atomic.Int32
	_ = client.SubscribeTask("mod_export_import", func(ctx context.Context, _ []byte) error {
		if imports.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	_ = client.SubscribeTask("notifications", func(context.Context, []byte) error { return nil })
	_ = client.SubscribeTask("ai", func(context.Context, []byte) error { return errors.New("disabled handler entered") })
	if _, err := pool.Exec(ctx, `insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload)
		select 'import-'||i,'test','mod_export_import','test',i::text,'{}'::jsonb from generate_series(1,70) i`); err != nil {
		t.Fatal(err)
	}
	dispatcher := NewOutboxDispatcher(pool, client)
	dispatcher.Start(ctx)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("import did not start")
	}
	if _, err := pool.Exec(ctx, `insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload) values
		('note','test','notifications','test','note','{}'),('paused','test','ai','test','paused','{}')`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status string
		if err := pool.QueryRow(ctx, `select status from nats_outbox where event_id='note'`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "published" {
			t.Logf("notification committed with import still running after %s", time.Since(start).Round(time.Millisecond))
			break
		}
		if time.Since(start) > 3500*time.Millisecond {
			t.Fatal("notification remained behind the blocked import for 3.5 seconds")
		}
		<-ticker.C
	}
	var processing, untouched int
	if err := pool.QueryRow(ctx, `select count(*) filter(where status='publishing'),count(*) filter(where attempts=0)
		from nats_outbox where subject='mod_export_import'`).Scan(&processing, &untouched); err != nil || processing != 1 || untouched != 69 {
		t.Fatalf("in-flight imports=%d untouched=%d err=%v", processing, untouched, err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `select attempts from nats_outbox where event_id='paused'`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("paused task consumed attempts=%d err=%v", attempts, err)
	}
}
