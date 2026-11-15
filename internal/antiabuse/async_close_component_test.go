package antiabuse

// Component tests exercise the actual async workers, pgxpool Acquire context,
// and normal Close. Both connection hooks reject physical connections: these
// controlled failures are not PostgreSQL persistence or HTTP/Redis proof.
import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

type asyncCloseComponentKey struct{}

type asyncCloseAcquireBarrier struct {
	entered chan context.Context
	block func(context.Context)
}

func (b *asyncCloseAcquireBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context { return ctx }
func (b *asyncCloseAcquireBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (b *asyncCloseAcquireBarrier) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	b.entered <- ctx
	b.block(ctx)
	return ctx
}
func (b *asyncCloseAcquireBarrier) TraceAcquireEnd(context.Context, *pgxpool.Pool, pgxpool.TraceAcquireEndData) {}

func newAsyncCloseComponentPool(t *testing.T, barrier *asyncCloseAcquireBarrier) (*pgxpool.Pool, *atomic.Int64) {
	t.Helper()
	// Explicit public dummy fields avoid ambient connection selectors. The
	// BeforeConnect error and guarded DialFunc make this a no-network component.
	poolConfig, err := pgxpool.ParseConfig("host=127.0.0.1 port=1 user=component password=component dbname=component sslmode=disable")
	if err != nil { t.Fatal("component-only pool configuration failed") }
	poolConfig.MinConns, poolConfig.MinIdleConns, poolConfig.MaxConns = 0, 0, successWorkerCount
	poolConfig.ConnConfig.Tracer = barrier
	poolConfig.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return errors.New("controlled component connection refusal") }
	dials := &atomic.Int64{}
	poolConfig.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("physical connection prohibited in async-close component")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil { t.Fatal("component-only pool construction failed") }
	return pool, dials
}

func waitAsyncCloseComponentContext(t *testing.T, entered <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-entered: return ctx
	case <-time.After(5*time.Second): t.Fatal("actual worker did not reach the Acquire barrier")
	}
	return nil
}

func waitAsyncCloseCancellation(t *testing.T, service *Service) time.Time {
	t.Helper()
	select {
	case <-service.workerCtx.Done():
	case <-time.After(5*time.Second): t.Fatal("normal Close did not stop worker selection")
	}
	// Done can close before cancellation has traversed every child. The same
	// existing closeOnce synchronizes with the full original Close callback;
	// this test never supplies the first callback or substitutes cancellation.
	unexpected := false
	service.closeOnce.Do(func() { unexpected = true })
	if unexpected { t.Fatal("component did not observe the original Close callback") }
	return time.Now()
}

func TestAsyncClosePreservesAcceptedWriteContextComponent(t *testing.T) {
	for _, kind := range []string{"risk", "success"} {
		t.Run(kind, func(t *testing.T) {
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			barrier := &asyncCloseAcquireBarrier{entered: make(chan context.Context, 8), block: func(context.Context) { <-release }}
			pool, dials := newAsyncCloseComponentPool(t, barrier)
			cache := querycache.New(config.RedisConfig{})
			// Only suppress unrelated retention work in this no-PG component.
			cache.ClaimThrottle(context.Background(), "anti-abuse-cleanup", 6*time.Hour)
			parent := context.WithValue(context.Background(), asyncCloseComponentKey{}, "accepted-record")
			service := New(parent, config.AntiAbuseConfig{Enabled: true}, pool, cache)
			t.Cleanup(func() {
				unblock()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := service.Close(ctx); err != nil { t.Error("component normal worker cleanup failed") }
				cache.Close()
				pool.Close()
			})
			input := Evaluation{UserID: 1, Action: "review.submit", Content: "accepted component record"}
			if kind == "risk" {
				if err := service.RecordDecision(context.Background(), input, Decision{Outcome: Deny}, HumanCrawler); err != nil { t.Fatal("component event enqueue failed") }
			} else if !service.EnqueueSuccess(input, Decision{Outcome: Allow}) { t.Fatal("component success enqueue failed") }
			writeCtx := waitAsyncCloseComponentContext(t, barrier.entered)
			deadline, bounded := writeCtx.Deadline()
			bound := asyncRecordTimeout
			if kind == "risk" { bound = time.Second }
			remaining := time.Until(deadline)
			if !bounded || remaining <= 0 || remaining > bound || writeCtx.Err() != nil || writeCtx.Value(asyncCloseComponentKey{}) != "accepted-record" { t.Fatal("actual accepted write lost its original bounded context or values") }
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			closed := make(chan error, 1)
			go func() { closed <- service.Close(closeCtx) }()
			waitAsyncCloseCancellation(t, service)
			cancelledByStop := writeCtx.Err()
			afterDeadline, stillBounded := writeCtx.Deadline()
			earlyReturn := false
			select { case <-closed: earlyReturn = true; default: }
			unblock()
			if !earlyReturn {
				select {
				case err := <-closed: if err != nil { t.Fatal("normal Close failed after controlled release") }
				case <-closeCtx.Done(): t.Fatal("normal Close did not join released real workers")
				}
			}
			if cancelledByStop != nil { t.Fatal("normal Close canceled an already accepted write before its existing bound") }
			if earlyReturn { t.Fatal("Close returned while a real accepted write was still in flight") }
			if !stillBounded || !deadline.Equal(afterDeadline) { t.Fatal("Close reset or extended the accepted write deadline") }
			if dials.Load() != 0 { t.Fatal("component attempted a physical connection") }
			metrics := service.AsyncMetrics()
			if kind == "risk" {
				if metrics.RiskEventFailed != 1 || metrics.RiskDropped != 0 || metrics.RiskQueueDepth != 0 { t.Fatal("controlled risk failure was not normally joined and accounted") }
			} else if metrics.SuccessQueued != 1 || metrics.SuccessProcessed != 1 || metrics.SuccessFailed != 1 || metrics.SuccessDropped != 0 || metrics.SuccessQueueDepth != 0 { t.Fatal("controlled success failure was not normally joined and accounted") }
		})
	}
}

func TestAsyncCloseSharesOneGraceWithInFlightAndQueuedWorkComponent(t *testing.T) {
	// Two real normal workers must reach their actual two-second write contexts
	// before Close. Queued work can use only the remainder of the same3s grace.
	barrier := &asyncCloseAcquireBarrier{entered: make(chan context.Context, 64), block: func(ctx context.Context) { <-ctx.Done() }}
	pool, dials := newAsyncCloseComponentPool(t, barrier)
	cache := querycache.New(config.RedisConfig{})
	cache.ClaimThrottle(context.Background(), "anti-abuse-cleanup", 6*time.Hour)
	service := New(context.Background(), config.AntiAbuseConfig{Enabled: true}, pool, cache)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.Close(ctx); err != nil { t.Error("bounded component worker cleanup failed") }
		cache.Close()
		pool.Close()
	})
	const count = 20
	for index := 0; index < count; index++ {
		if !service.EnqueueSuccess(Evaluation{UserID: 1, Action: "review.submit", Content: "bounded queued component"}, Decision{Outcome: Allow}) { t.Fatal("bounded component queue rejected accepted work") }
	}
	inflight := make([]context.Context, successWorkerCount)
	for index := range inflight { inflight[index] = waitAsyncCloseComponentContext(t, barrier.entered) }
	closeCtx, cancel := context.WithTimeout(context.Background(), asyncShutdownDrainTimeout+time.Second)
	defer cancel()
	closed := make(chan error, 1)
	started := time.Now()
	go func() { closed <- service.Close(closeCtx) }()
	stopped := waitAsyncCloseCancellation(t, service)
	// Stopping cannot cancel either accepted two-second write immediately.
	for _, ctx := range inflight { if ctx.Err() != nil { t.Error("stop canceled accepted work instead of sharing a bounded grace") } }
	select {
	case err := <-closed: if err != nil { t.Fatal("Close exceeded the fixed shared grace") }
	case <-closeCtx.Done(): t.Fatal("Close reset the drain budget after finishing in-flight work")
	}
	if time.Since(started) > asyncShutdownDrainTimeout+500*time.Millisecond { t.Fatal("in-flight work was added on top of a new drain grace") }
	var drainDeadline time.Time
	drained := 0
	for len(barrier.entered) > 0 {
		ctx := <-barrier.entered
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(stopped.Add(asyncShutdownDrainTimeout)) { t.Fatal("queued work reset the shared shutdown deadline") }
		if drainDeadline.IsZero() { drainDeadline = deadline } else if !deadline.Equal(drainDeadline) { t.Fatal("different workers received independent new drain deadlines") }
		drained++
	}
	if drained == 0 { t.Fatal("component did not exercise queued writes after in-flight work") }
	metrics := service.AsyncMetrics()
	if metrics.SuccessQueued != count || metrics.SuccessQueueDepth != 0 || metrics.SuccessDropped == 0 || metrics.SuccessProcessed+metrics.SuccessDropped != count || metrics.SuccessFailed != metrics.SuccessProcessed { t.Fatal("bounded queued component did not account for every failure/drop") }
	if dials.Load() != 0 { t.Fatal("slow component attempted a physical connection") }
}
