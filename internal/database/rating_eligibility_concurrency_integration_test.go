package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestRatingEligibilityChangesDuringVoteIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires PostgreSQL for two transactions and a lock observer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture, err := NewIsolatedTestSchema(ctx, config.Load().DB.ConnString(), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fixture.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	pool := fixture.Pool
	if _, err := pool.Exec(ctx, `insert into users(id,username,email,password_hash,security_score)
		values (71,'rating_concurrency','rating-concurrency@example.invalid','fixture',80);
		insert into mods(id,project_code,slug,primary_name,review_status) values(72,'ratingcon','concurrent-rating','Concurrent rating','approved');
		insert into content_ratings(id,object_route_id,author_id,overall_score)
		select 73,id,71,4 from public_routes where entity_type='mod' and internal_id=72`); err != nil {
		t.Fatal(err)
	}
	t.Run("eligibility update waits for an uncommitted edit", func(t *testing.T) {
		runRatingLockOrder(t, ctx, pool,
			`select 1 from content_ratings where id=73 for update`,
			`update users set security_score=30 where id=71`,
			`update content_ratings set overall_score=5 where id=73`)
	})
	t.Run("new vote waits for the current account", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `delete from content_ratings where id=73`); err != nil {
			t.Fatal(err)
		}
		runRatingLockOrder(t, ctx, pool,
			`update users set security_score=80 where id=71`,
			`insert into content_ratings(id,object_route_id,author_id,overall_score)
			 select 73,id,71,3 from public_routes where entity_type='mod' and internal_id=72`,
			`select 1`)
	})
	var counted bool
	var count, score int
	if err := pool.QueryRow(ctx, `select rating.counted,facts.rating_count,facts.rating_sum
		from content_ratings rating join content_popularity_lifetime_facts facts using(object_route_id)
		where rating.id=73`).Scan(&counted, &count, &score); err != nil || !counted || count != 1 || score != 3 {
		t.Fatalf("final eligibility=%t count=%d score=%d: %v", counted, count, score, err)
	}
}

func runRatingLockOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, hold, wait, finish string) {
	t.Helper()
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, hold); err != nil {
		t.Fatal(err)
	}
	waiter, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Release()
	holderPID, waiterPID := holder.Conn().PgConn().PID(), waiter.Conn().PgConn().PID()
	done := make(chan struct{})
	var waitErr error
	go func() {
		defer close(done)
		waitErr = pgx.BeginFunc(ctx, waiter, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, wait)
			return err
		})
	}()
	defer func() {
		// Release the holder even when an assertion fails, then drain the child.
		_ = holder.Rollback(ctx)
		<-done
		if waitErr != nil {
			t.Errorf("waiting write: %v", waitErr)
		}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `select $1::int=any(pg_blocking_pids($2))`,
			holderPID, waiterPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ticker.C:
		case <-done:
			t.Fatal("the second transaction finished before acquiring the expected lock")
		case <-ctx.Done():
			t.Fatal("PostgreSQL did not observe the expected wait", ctx.Err())
		}
	}
	if _, err := holder.Exec(ctx, finish); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
