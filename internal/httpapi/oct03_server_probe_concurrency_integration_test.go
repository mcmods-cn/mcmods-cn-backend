package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/serverprobe"
)

// Lock half of the actual due rows so the first caller cannot claim the whole
// fixture. Releasing the locks after its probes start forces two independent
// committed claim batches, rather than relying on PostgreSQL scheduling luck.
func TestOCT03ServerSchedulerSplitClaimsShareProcessProbeBudgetIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	ids := oct03SeedSchedulerTargets(t, f, 48)
	locked, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(f.ctx)
	if _, err = locked.Exec(f.ctx, `select id from minecraft_servers where id=any($1::bigint[]) for update`, ids[24:]); err != nil {
		t.Fatal(err)
	}

	started := make(chan string, len(ids))
	release := make(chan struct{})
	var releaseOnce sync.Once
	var workers sync.WaitGroup
	var active, peak atomic.Int64
	seen := make(map[string]int)
	var seenLock sync.Mutex
	probe := func(ctx context.Context, address string) (serverprobe.Result, error) {
		seenLock.Lock()
		seen[address]++
		seenLock.Unlock()
		current := active.Add(1)
		defer active.Add(-1)
		for prior := peak.Load(); current > prior; prior = peak.Load() {
			if peak.CompareAndSwap(prior, current) {
				break
			}
		}
		started <- address
		select {
		case <-release:
			return test015ProbeResult(address), nil
		case <-ctx.Done():
			return serverprobe.Result{}, ctx.Err()
		}
	}
	finish := func() { releaseOnce.Do(func() { close(release) }); workers.Wait() }
	t.Cleanup(finish)
	start := func() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			probeDueMinecraftServersWithProbe(f.ctx, f.db, probe)
		}()
	}
	start()
	oct03WaitSchedulerStarts(t, started, 24)
	if err = locked.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	start()
	oct03WaitSchedulerStarts(t, started, 8)
	var claimed int
	if err = f.db.QueryRow(f.ctx, `select count(*) from minecraft_servers where id=any($1::bigint[]) and next_probe_at>now()`, ids).Scan(&claimed); err != nil {
		t.Fatal(err)
	}
	if claimed != 48 || active.Load() != 32 {
		t.Fatalf("split claims were not exercised: claimed=%d active=%d", claimed, active.Load())
	}
	select {
	case address := <-started:
		t.Fatalf("second committed claim batch exceeded the process budget: address=%s active=%d peak=%d", address, active.Load(), peak.Load())
	case <-time.After(3 * time.Second):
		// Keep the original TEST015 observation budget. A positive extra entry
		// is the old-code failure; no sleeps or larger timeout hide the race.
	}
	finish()
	seenLock.Lock()
	defer seenLock.Unlock()
	if len(seen) != 48 || peak.Load() != 32 || active.Load() != 0 {
		t.Fatalf("completed split cycle facts: seen=%d peak=%d active=%d", len(seen), peak.Load(), active.Load())
	}
	for address, count := range seen {
		if count != 1 {
			t.Errorf("split claim repeated %s %d times", address, count)
		}
	}
	var samples int
	if err = f.db.QueryRow(f.ctx, `select count(*) from minecraft_server_status_samples where server_id=any($1::bigint[])`, ids).Scan(&samples); err != nil {
		t.Fatal(err)
	}
	if samples != 48 {
		t.Fatalf("split cycle persisted %d samples, want 48", samples)
	}
}

// Wait for the second transaction's real COMMIT, then cancel its callers while
// the first cycle still owns all 32 permits. A fresh full cycle must afterwards
// reach 32 again, proving cancellation and completion do not leak permits.
func TestOCT03ServerSchedulerCancellationReleasesProcessProbeBudgetIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	ids := oct03SeedSchedulerTargets(t, f, 48)
	locked, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(f.ctx)
	if _, err = locked.Exec(f.ctx, `select id from minecraft_servers where id=any($1::bigint[]) for update`, ids[32:]); err != nil {
		t.Fatal(err)
	}
	firstStarted := make(chan string, 32)
	firstRelease := make(chan struct{})
	var releaseOnce sync.Once
	var workers sync.WaitGroup
	firstProbe := func(ctx context.Context, address string) (serverprobe.Result, error) {
		firstStarted <- address
		select {
		case <-firstRelease:
			return test015ProbeResult(address), nil
		case <-ctx.Done():
			return serverprobe.Result{}, ctx.Err()
		}
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		probeDueMinecraftServersWithProbe(f.ctx, f.db, firstProbe)
	}()
	secondCtx, cancelSecond := context.WithCancel(f.ctx)
	finish := func() {
		cancelSecond()
		releaseOnce.Do(func() { close(firstRelease) })
		workers.Wait()
	}
	t.Cleanup(finish)
	oct03WaitSchedulerStarts(t, firstStarted, 32)
	if err = locked.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	committed := make(chan struct{})
	poolConfig := f.db.Config().Copy()
	poolConfig.MaxConns, poolConfig.MinConns = 1, 0
	poolConfig.ConnConfig.Tracer = &oct03SchedulerCommitTrace{committed: committed}
	secondPool, err := pgxpool.NewWithConfig(f.ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { finish(); secondPool.Close() })
	var cancelledProbes atomic.Int64
	secondDone := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer close(secondDone)
		probeDueMinecraftServersWithProbe(secondCtx, secondPool, func(context.Context, string) (serverprobe.Result, error) {
			cancelledProbes.Add(1)
			return serverprobe.Result{}, fmt.Errorf("cancelled queued probe must not execute")
		})
	}()
	select {
	case <-committed:
	case <-time.After(3 * time.Second):
		t.Fatal("second scheduler did not commit its distinct claim batch")
	}
	cancelSecond()
	select {
	case <-secondDone:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled scheduler did not release its queued work")
	}
	if cancelledProbes.Load() != 0 {
		t.Fatalf("cancelled claim started %d probes while 32 permits were occupied", cancelledProbes.Load())
	}
	finish()
	var before int
	if err = f.db.QueryRow(f.ctx, `select count(*) from minecraft_server_status_samples where server_id=any($1::bigint[])`, ids).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 32 {
		t.Fatalf("cancelled batch persisted samples: total=%d want=32", before)
	}
	if _, err = f.db.Exec(f.ctx, `update minecraft_servers set next_probe_at=now()-interval '1 second' where id=any($1::bigint[])`, ids); err != nil {
		t.Fatal(err)
	}
	recoveredStarted := make(chan string, len(ids))
	recoveredRelease := make(chan struct{})
	var recoveredOnce sync.Once
	var recoveredCalls atomic.Int64
	workers.Add(1)
	go func() {
		defer workers.Done()
		probeDueMinecraftServersWithProbe(f.ctx, f.db, func(ctx context.Context, address string) (serverprobe.Result, error) {
			recoveredCalls.Add(1)
			recoveredStarted <- address
			select {
			case <-recoveredRelease:
				return test015ProbeResult(address), nil
			case <-ctx.Done():
				return serverprobe.Result{}, ctx.Err()
			}
		})
	}()
	t.Cleanup(func() { recoveredOnce.Do(func() { close(recoveredRelease) }); workers.Wait() })
	oct03WaitSchedulerStarts(t, recoveredStarted, 32)
	recoveredOnce.Do(func() { close(recoveredRelease) })
	workers.Wait()
	var after int
	if err = f.db.QueryRow(f.ctx, `select count(*) from minecraft_server_status_samples where server_id=any($1::bigint[])`, ids).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if recoveredCalls.Load() != 48 || after != 80 {
		t.Fatalf("fresh cycle could not reuse all permits: calls=%d samples=%d", recoveredCalls.Load(), after)
	}
}

func oct03SeedSchedulerTargets(t *testing.T, f test015ServerFixture, count int) []int64 {
	t.Helper()
	ids := make([]int64, count)
	for index := range ids {
		ids[index] = f.seedServer(t, fmt.Sprintf("oct03-owned-scheduled-%02d.invalid:25565", index), "approved")
	}
	return ids
}

func oct03WaitSchedulerStarts(t *testing.T, started <-chan string, count int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for range count {
		select {
		case <-started:
		case <-deadline.C:
			t.Fatalf("scheduler did not start its expected %d probes", count)
		}
	}
}

type oct03SchedulerCommitKey struct{}

type oct03SchedulerCommitTrace struct {
	committed chan struct{}
	once      sync.Once
}

func (trace *oct03SchedulerCommitTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, oct03SchedulerCommitKey{}, strings.ToLower(strings.TrimSpace(data.SQL)))
}

func (trace *oct03SchedulerCommitTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if query, _ := ctx.Value(oct03SchedulerCommitKey{}).(string); query == "commit" && data.Err == nil {
		trace.once.Do(func() { close(trace.committed) })
	}
}
