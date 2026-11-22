package queue

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
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

func TestExhaustedLocalHandlerPreservesConsumerReplayEnvelope(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	client := New(ctx, config.NATSConfig{})
	defer client.Close()
	_ = client.SubscribeTask("notifications", func(context.Context, []byte) error { return errors.New("synthetic failure") })
	if _, err := pool.Exec(ctx, `insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload,max_attempts)
		values('local-exhausted','test.local','notifications','fixture','one','{"ok":true}',1)`); err != nil {
		t.Fatal(err)
	}
	if n, err := NewOutboxDispatcher(pool, client).DispatchBatch(ctx, 1); n != 1 || err != nil {
		t.Fatalf("local delivery: n=%d err=%v", n, err)
	}
	var stage string
	var raw []byte
	if err := pool.QueryRow(ctx, `select failure_stage,payload from dead_letter_events where event_id='local-exhausted'`).Scan(&stage, &raw); err != nil {
		t.Fatal(err)
	}
	payload, envelope, err := UnwrapEvent(raw)
	if err != nil || stage != "consumer:notifications" || envelope.EventID != "local-exhausted" || !strings.Contains(string(payload), "true") {
		t.Fatalf("local dead letter cannot use consumer replay: stage=%s event=%+v err=%v", stage, envelope, err)
	}
}

func TestFullStreamPreservesOutboxUntilCapacityReturns(t *testing.T) {
	pool := openOutboxStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg := config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "capacity-" + randomEventID(),
		JetStream: config.JetStreamConfig{Enabled: true, Stream: uniqueJetStreamName("CAPACITY_"), NewStreamMaxBytes: 1600}}
	client := New(ctx, cfg)
	defer client.Close()
	js := client.jetStream
	if js == nil {
		t.Fatal("could not create bounded stream")
	}
	defer js.DeleteStream(cfg.JetStream.Stream)
	sample := client.StreamUsage(ctx)
	if sample == nil || sample.MaxBytes != 1600 || sample.MaxAgeSeconds != 0 || sample.Discard != nats.DiscardNew.String() {
		t.Fatalf("bootstrap policy: %+v", sample)
	}
	original := strings.Repeat("b", 1240)
	first, err := js.Publish(cfg.SubjectPrefix+".owned-backlog", []byte(original))
	if err != nil {
		t.Fatal(err)
	}
	sample = client.StreamUsage(ctx)
	if sample == nil || (sample.CapacityState != "warning" && sample.CapacityState != "critical") {
		t.Fatalf("missing capacity alert: %+v", sample)
	}
	if _, err := pool.Exec(ctx, `insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload,max_attempts)
		values('capacity-test','test','notifications','test','one',jsonb_build_object('text',repeat('w',1000)),1)`); err != nil {
		t.Fatal(err)
	}
	d := NewOutboxDispatcher(pool, client)
	if n, err := d.DispatchBatch(ctx, 64); err != nil || n != 1 {
		t.Fatalf("full stream dispatch count=%d err=%v", n, err)
	}
	var status string
	var attempts, dead int
	var delayed bool
	if err = pool.QueryRow(ctx, `select status,attempts,available_at>now(),(select count(*) from dead_letter_events) from nats_outbox where event_id='capacity-test'`).Scan(&status, &attempts, &delayed, &dead); err != nil || status != "failed" || attempts != 0 || !delayed || dead != 0 {
		t.Fatalf("capacity rejection exhausted intent: status=%s attempts=%d delayed=%v dead=%d err=%v", status, attempts, delayed, dead, err)
	}
	old, err := js.GetMsg(cfg.JetStream.Stream, first.Sequence)
	if err != nil || string(old.Data) != original {
		t.Fatalf("existing backlog lost on refusal: %v", err)
	}
	info, err := js.StreamInfo(cfg.JetStream.Stream)
	if err != nil {
		t.Fatal(err)
	}
	info.Config.MaxBytes = 8192
	if _, err = js.UpdateStream(&info.Config); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update nats_outbox set available_at=now() where event_id='capacity-test'`); err != nil {
		t.Fatal(err)
	}
	if _, err = d.DispatchBatch(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var published bool
	if err = pool.QueryRow(ctx, `select status='published' and attempts=1 from nats_outbox where event_id='capacity-test'`).Scan(&published); err != nil || !published {
		t.Fatalf("outbox did not recover: published=%v err=%v", published, err)
	}
	retried, err := js.GetMsg(cfg.JetStream.Stream, first.Sequence+1)
	if err != nil || retried.Header.Get(nats.MsgIdHdr) != "capacity-test" {
		t.Fatalf("original event identity lost after capacity recovery: %v", err)
	}
}

// Publication and the PostgreSQL outbox receipt cannot commit atomically.
// Exercise both sides of that boundary, including a retry after the broker's
// duplicate window has elapsed. The handler models a transactional business
// effect with a unique event ID, independently of transport deduplication.
func TestOutboxReplaysSameEventAfterDeliveryReceiptWriteFailsIntegration(t *testing.T) {
	for _, mode := range []string{"local", "jetstream"} {
		t.Run(mode, func(t *testing.T) {
			pool := openOutboxStateTestDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cfg := config.NATSConfig{}
			if mode == "jetstream" {
				cfg = config.NATSConfig{Enabled: true, URL: jetStreamIntegrationURL(t), SubjectPrefix: "receipt-" + randomEventID(),
					JetStream: config.JetStreamConfig{Enabled: true, Stream: uniqueJetStreamName("RECEIPT_")}}
			}
			client := New(ctx, cfg)
			defer client.Close()
			const duplicateWindow = 100 * time.Millisecond
			if mode == "jetstream" {
				js := client.jetStream
				if js == nil {
					t.Fatal("JetStream unavailable")
				}
				defer js.DeleteStream(cfg.JetStream.Stream)
				info, err := js.StreamInfo(cfg.JetStream.Stream)
				if err != nil {
					t.Fatal(err)
				}
				info.Config.Duplicates = duplicateWindow
				if _, err = js.UpdateStream(&info.Config); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `create temp table handled_business_events(event_id text primary key);
				alter table nats_outbox add constraint temporarily_reject_outbox_receipt check(status <> 'published');
				insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload)
				values('receipt-retry','test.deliver','notifications','test','one','{}')`); err != nil {
				t.Fatal(err)
			}
			deliveries := make(chan string, 4)
			if err := client.SubscribeTask("notifications", func(work context.Context, _ []byte) error {
				id := EventIDFromContext(work)
				if _, err := pool.Exec(work, `insert into handled_business_events(event_id) values($1) on conflict do nothing`, id); err != nil {
					return err
				}
				select {
				case deliveries <- id:
					return nil
				case <-work.Done():
					return work.Err()
				}
			}); err != nil && !(mode == "local" && errors.Is(err, ErrUnavailable)) {
				t.Fatal(err)
			}
			waitHandled := func() {
				t.Helper()
				select {
				case id := <-deliveries:
					if id != "receipt-retry" {
						t.Fatalf("delivery changed the persisted event identity: %q", id)
					}
				case <-ctx.Done():
					t.Fatal("missing consumer delivery", ctx.Err())
				}
			}
			before := NewOutboxDispatcher(pool, client)
			if n, err := before.DispatchBatch(ctx, 1); err == nil || n != 1 || before.Metrics().Published != 0 {
				t.Fatalf("receipt failure acknowledged by dispatcher: rows=%d err=%v metrics=%+v", n, err, before.Metrics())
			}
			waitHandled()
			assertOutboxStatus(t, ctx, pool, 1, "publishing")
			if _, err := pool.Exec(ctx, `alter table nats_outbox drop constraint temporarily_reject_outbox_receipt;
				update nats_outbox set locked_at=now()-interval '1 hour' where event_id='receipt-retry'`); err != nil {
				t.Fatal(err)
			}
			if mode == "jetstream" {
				// Redelivery must reach the consumer even when the transport can
				// no longer recognize the first publication as a duplicate.
				time.Sleep(3 * duplicateWindow)
			}
			after := NewOutboxDispatcher(pool, client)
			if n, err := after.DispatchBatch(ctx, 1); err != nil || n != 1 || after.Metrics().Published != 1 {
				t.Fatalf("stale lease recovery: rows=%d err=%v metrics=%+v", n, err, after.Metrics())
			}
			waitHandled()
			var attempts, effects int
			if err := pool.QueryRow(ctx, `select attempts,(select count(*) from handled_business_events) from nats_outbox
				where event_id='receipt-retry' and status='published'`).Scan(&attempts, &effects); err != nil || attempts != 2 || effects != 1 {
				t.Fatalf("recovery left attempts=%d effects=%d err=%v", attempts, effects, err)
			}
		})
	}
}
