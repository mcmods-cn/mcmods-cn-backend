package antiabuse

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestSuccessQueueIsBoundedAndReportsOverflow(t *testing.T) {
	service := &Service{
		cfg:       config.AntiAbuseConfig{Enabled: true},
		successes: make(chan queuedSuccessRecord, 2),
	}
	input := Evaluation{Action: "comment.create", Content: "body"}
	for index := 0; index < 2; index++ {
		if !service.EnqueueSuccess(input, Decision{Outcome: Allow}) {
			t.Fatalf("queue rejected item %d before capacity", index)
		}
	}
	if service.EnqueueSuccess(input, Decision{Outcome: Allow}) {
		t.Fatal("queue accepted work beyond capacity")
	}
	metrics := service.AsyncMetrics()
	if metrics.SuccessQueued != 2 || metrics.SuccessDropped != 1 || metrics.SuccessQueueDepth != 2 || metrics.SuccessQueueCapacity != 2 {
		t.Fatalf("unexpected async metrics: %#v", metrics)
	}
}

func TestSuccessWorkersFailFastAndDrainOnShutdown(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://invalid:invalid@127.0.0.1:1/invalid")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	service := New(context.Background(), config.AntiAbuseConfig{Enabled: true}, pool, cache)
	for index := 0; index < 100; index++ {
		if !service.EnqueueSuccess(Evaluation{Action: "comment.create", Content: "bounded"}, Decision{Outcome: Allow}) {
			t.Fatalf("enqueue %d failed before queue capacity", index)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = service.Close(shutdownCtx); err != nil {
		t.Fatalf("close async recorders: %v", err)
	}
	if err = service.Close(shutdownCtx); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	metrics := service.AsyncMetrics()
	if metrics.SuccessProcessed != 100 || metrics.SuccessFailed == 0 || metrics.SuccessQueueDepth != 0 {
		t.Fatalf("unexpected drained failure metrics: %#v", metrics)
	}
}

func TestSuccessWorkersBoundSlowDatabaseAndDiscardUndrainedWork(t *testing.T) {
	poolConfig, err := pgxpool.ParseConfig("postgres://invalid:invalid@127.0.0.1:1/invalid")
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.BeforeConnect = func(ctx context.Context, _ *pgx.ConnConfig) error {
		<-ctx.Done()
		return ctx.Err()
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	service := New(context.Background(), config.AntiAbuseConfig{Enabled: true}, pool, cache)
	for index := 0; index < 20; index++ {
		if !service.EnqueueSuccess(Evaluation{Action: "comment.create", Content: "bounded"}, Decision{Outcome: Allow}) {
			t.Fatalf("enqueue %d failed", index)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = service.Close(shutdownCtx); err != nil {
		t.Fatalf("slow database shutdown exceeded its bound: %v", err)
	}
	metrics := service.AsyncMetrics()
	if metrics.SuccessQueueDepth != 0 || metrics.SuccessDropped == 0 ||
		metrics.SuccessProcessed+metrics.SuccessDropped != metrics.SuccessQueued {
		t.Fatalf("slow database work was not accounted for: %#v", metrics)
	}
}

func TestSuccessQueueTruncatesContentBeforeRetention(t *testing.T) {
	service := &Service{
		cfg:       config.AntiAbuseConfig{Enabled: true},
		successes: make(chan queuedSuccessRecord, 1),
	}
	if !service.EnqueueSuccess(Evaluation{Action: "comment.create", Content: string(make([]byte, maximumQueuedSuccessContentBytes+100))}, Decision{Outcome: Allow}) {
		t.Fatal("enqueue failed")
	}
	queued := <-service.successes
	if len(queued.input.Content) != maximumQueuedSuccessContentBytes {
		t.Fatalf("queued content length = %d", len(queued.input.Content))
	}
}
