package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The tracer holds the actual PostgreSQL renewal at its query boundary until
// cancellation, making shutdown overlap deterministic without replacing Exec.
type oct02SeedHeartbeatTracer struct{ entered chan struct{} }

func (tracer *oct02SeedHeartbeatTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "set lease_expires_at=now()+") {
		tracer.entered <- struct{}{}
		<-ctx.Done()
	}
	return ctx
}

func (*oct02SeedHeartbeatTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestOCT02SeedCrawlerNormalHeartbeatStopDuringRenewalIntegration(t *testing.T) {
	tracer := &oct02SeedHeartbeatTracer{entered: make(chan struct{}, 1)}
	ctx, _, worker := newOCT02SeedHeartbeatFixture(t, tracer, false)
	runCtx, stop := worker.startLeaseHeartbeat(ctx, 1, "current-owner")
	select {
	case <-tracer.entered:
	case <-ctx.Done():
		t.Fatal("renewal did not reach the controlled query boundary")
	}
	if err := stop(); err != nil {
		t.Fatalf("normal heartbeat shutdown returned a renewal failure: %v", err)
	}
	if runCtx.Err() == nil {
		t.Fatal("normal heartbeat shutdown did not cancel the finished run context")
	}
}

func TestOCT02SeedCrawlerHeartbeatParentCancellationRemainsErrorIntegration(t *testing.T) {
	tracer := &oct02SeedHeartbeatTracer{entered: make(chan struct{}, 1)}
	ctx, cancel, worker := newOCT02SeedHeartbeatFixture(t, tracer, false)
	_, stop := worker.startLeaseHeartbeat(ctx, 1, "current-owner")
	select {
	case <-tracer.entered:
	case <-ctx.Done():
		t.Fatal("renewal did not reach the controlled query boundary")
	}
	cancel()
	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation was hidden by normal stop: %v", err)
	}
}

func TestOCT02SeedCrawlerHeartbeatDatabaseFailureSurvivesStopIntegration(t *testing.T) {
	ctx, _, worker := newOCT02SeedHeartbeatFixture(t, nil, true)
	runCtx, stop := worker.startLeaseHeartbeat(ctx, 1, "current-owner")
	select {
	case <-runCtx.Done():
	case <-ctx.Done():
		t.Fatal("database renewal failure did not cancel the active run")
	}
	var databaseErr *pgconn.PgError
	if err := stop(); !errors.As(err, &databaseErr) || databaseErr.Code != "23514" {
		t.Fatalf("actual database constraint failure was hidden by stop: %v", err)
	}
}

func TestOCT02SeedCrawlerHeartbeatLeaseLossSurvivesStopIntegration(t *testing.T) {
	ctx, _, worker := newOCT02SeedHeartbeatFixture(t, nil, false)
	runCtx, stop := worker.startLeaseHeartbeat(ctx, 1, "stale-owner")
	select {
	case <-runCtx.Done():
	case <-ctx.Done():
		t.Fatal("lease loss did not cancel the active run")
	}
	if err := stop(); !errors.Is(err, errSeedCrawlerLeaseOwnershipLost) {
		t.Fatalf("lease ownership loss was hidden by stop: %v", err)
	}
}

func newOCT02SeedHeartbeatFixture(t *testing.T, tracer pgx.QueryTracer, rejectRenewal bool) (context.Context, context.CancelFunc, *SeedCrawlerWorker) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" || os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set an explicit isolated MCMODS_TEST_DATABASE_URL and MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		cancel()
		t.Fatal("invalid explicit test database configuration")
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	poolConfig.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		cancel()
		t.Fatal("failed to create isolated test database pool")
	}
	t.Cleanup(pool.Close)
	t.Cleanup(cancel)
	if _, err = pool.Exec(ctx, `create temporary table seed_crawler_runs(
		id bigint primary key,status text not null,lease_owner text not null,lease_expires_at timestamptz
	); insert into seed_crawler_runs values(1,'running','current-owner','2000-01-01')`); err != nil {
		t.Fatal(err)
	}
	if rejectRenewal {
		if _, err = pool.Exec(ctx, `alter table seed_crawler_runs add constraint oct02_reject_renewal
			check(lease_expires_at<'2001-01-01'::timestamptz)`); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, cancel, &SeedCrawlerWorker{
		server: &Server{db: pool}, leaseDuration: time.Minute, heartbeatInterval: 5 * time.Millisecond,
	}
}
