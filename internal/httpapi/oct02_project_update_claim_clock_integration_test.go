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
)

type oct02ProjectUpdateClaimContextKey struct{}

// Pause a real database claim before execution or immediately after acquiring
// its row lock. No task or notification query is replaced with a fake result.
type oct02ProjectUpdateClaimPause struct {
	afterSelect bool
	arrived     chan struct{}
	resume      chan struct{}
	pauseOnce   sync.Once
	resumeOnce  sync.Once
	pid         atomic.Uint32
}

func (pause *oct02ProjectUpdateClaimPause) wait(ctx context.Context) {
	pause.pauseOnce.Do(func() {
		close(pause.arrived)
		select {
		case <-pause.resume:
		case <-ctx.Done():
		}
	})
}

func (pause *oct02ProjectUpdateClaimPause) release() {
	pause.resumeOnce.Do(func() { close(pause.resume) })
}

func (pause *oct02ProjectUpdateClaimPause) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !strings.Contains(data.SQL, "for update of task") {
		return ctx
	}
	pause.pid.Store(conn.PgConn().PID())
	if pause.afterSelect {
		return context.WithValue(ctx, oct02ProjectUpdateClaimContextKey{}, true)
	}
	pause.wait(ctx)
	return ctx
}

func (pause *oct02ProjectUpdateClaimPause) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if pause.afterSelect && ctx.Value(oct02ProjectUpdateClaimContextKey{}) == true {
		pause.wait(ctx)
	}
}

func TestOCT02ProjectUpdateClaimUsesCurrentClockIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("project update claim clocks require an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	stamp := fmt.Sprintf("oct02-claim-%d", time.Now().UnixNano())
	var actorID, projectID, routeID, firstCursor, lastCursor int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$1||'@example.invalid','synthetic',true) returning id`, stamp+"-actor").Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into users(username,email,password_hash,email_verified,preferred_ui_language)
		select $1||'-recipient-'||n,$1||'-recipient-'||n||'@example.invalid','synthetic',true,'zh-CN'
		from generate_series(1,201) n`, stamp); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from users where username like $1 order by id offset 199 limit 1`, stamp+"-recipient-%").Scan(&firstCursor); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select max(id) from users where username like $1`, stamp+"-recipient-%").Scan(&lastCursor); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('t018c0001','oct02-claim-clock','Claim clock project','approved',$1) returning id`, actorID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_follows(user_id,project_route_id)
		select id,$1 from users where username like $2`, routeID, stamp+"-recipient-%"); err != nil {
		t.Fatal(err)
	}

	for _, waitingForLock := range []bool{false, true} {
		name := "old_transaction_before_claim"
		if waitingForLock {
			name = "old_transaction_waiting_for_lock"
		}
		t.Run(name, func(t *testing.T) {
			subctx, subcancel := context.WithCancel(ctx)
			defer subcancel()
			eventID := insertTEST018ProjectUpdateTask(t, subctx, pool, routeID, actorID, name)
			oldPause := &oct02ProjectUpdateClaimPause{arrived: make(chan struct{}), resume: make(chan struct{})}
			defer oldPause.release()
			newPause := &oct02ProjectUpdateClaimPause{afterSelect: true, arrived: make(chan struct{}), resume: make(chan struct{})}
			defer newPause.release()
			newWorkerPool := func(pause *oct02ProjectUpdateClaimPause) *pgxpool.Pool {
				t.Helper()
				config := pool.Config().Copy()
				config.MaxConns, config.MinConns = 1, 0
				config.ConnConfig.Tracer = pause
				value, err := pgxpool.NewWithConfig(subctx, config)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(value.Close)
				return value
			}
			oldWorker := NewProjectUpdateNotificationWorker(newWorkerPool(oldPause), nil, nil)
			oldResult := make(chan error, 1)
			go func() { oldResult <- oldWorker.process(subctx, eventID) }()
			await := func(signal <-chan struct{}) {
				t.Helper()
				select {
				case <-signal:
				case <-subctx.Done():
					t.Fatal(subctx.Err())
				}
			}
			await(oldPause.arrived) // Its transaction starts before the next batch writer.
			if waitingForLock {
				newWorker := NewProjectUpdateNotificationWorker(newWorkerPool(newPause), nil, nil)
				newResult := make(chan error, 1)
				go func() { newResult <- newWorker.process(subctx, eventID) }()
				await(newPause.arrived) // The new writer holds the actual task row lock.
				oldPause.release()
				tick := time.NewTicker(5 * time.Millisecond)
				defer tick.Stop()
				for {
					var blocked bool
					if err := pool.QueryRow(subctx, `select coalesce(wait_event_type='Lock',false)
						from pg_stat_activity where pid=$1`, oldPause.pid.Load()).Scan(&blocked); err != nil {
						t.Fatal(err)
					}
					if blocked {
						break
					}
					select {
					case <-tick.C:
					case <-subctx.Done():
						t.Fatal(subctx.Err())
					}
				}
				newPause.release()
				if err := <-newResult; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := NewProjectUpdateNotificationWorker(pool, nil, nil).process(subctx, eventID); err != nil {
					t.Fatal(err)
				}
				assertTEST018Task(t, subctx, pool, eventID, "pending", firstCursor, 200, 0)
				oldPause.release()
			}
			if err := <-oldResult; err != nil {
				t.Fatal(err)
			}
			assertTEST018Task(t, subctx, pool, eventID, "completed", lastCursor, 201, 0)
			assertTEST018Deliveries(t, subctx, pool, eventID, 201, 201, 0)
			if err := oldWorker.process(subctx, eventID); err != nil {
				t.Fatal(err)
			}
			assertTEST018Deliveries(t, subctx, pool, eventID, 201, 201, 0)
		})
	}

	// The fresh clock must not bypass a real delivery window or retry backoff.
	eventID := insertTEST018ProjectUpdateTask(t, ctx, pool, routeID, actorID, "future")
	if _, err := pool.Exec(ctx, `update project_update_notification_tasks
		set next_attempt_at=clock_timestamp()+interval '1 hour' where event_id=$1`, eventID); err != nil {
		t.Fatal(err)
	}
	if err := NewProjectUpdateNotificationWorker(pool, nil, nil).process(ctx, eventID); err != nil {
		t.Fatal(err)
	}
	assertTEST018Task(t, ctx, pool, eventID, "pending", 0, 0, 0)
	assertTEST018Deliveries(t, ctx, pool, eventID, 0, 0, 0)
}
