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

func newPopularityConcurrencyPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires PostgreSQL for two transactions and a lock observer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	fixture, err := NewIsolatedTestSchema(ctx, config.Load().DB.ConnString(), 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fixture.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return ctx, fixture.Pool
}

func TestRatingEligibilityChangesDuringVoteIntegration(t *testing.T) {
	ctx, pool := newPopularityConcurrencyPool(t)
	if _, err := pool.Exec(ctx, `insert into users(id,username,email,password_hash,security_score)
		values (71,'rating_concurrency','rating-concurrency@example.invalid','fixture',80);
		insert into mods(id,project_code,slug,primary_name,review_status) values(72,'ratingcon','concurrent-rating','Concurrent rating','approved');
		insert into content_ratings(id,object_route_id,author_id,overall_score)
		select 73,id,71,4 from public_routes where entity_type='mod' and internal_id=72`); err != nil {
		t.Fatal(err)
	}
	assertRating := func(t *testing.T, wantCounted bool, wantCount, wantSum int) {
		t.Helper()
		var counted bool
		var count, score int
		err := pool.QueryRow(ctx, `select rating.counted,facts.rating_count,facts.rating_sum
			from content_ratings rating join content_popularity_lifetime_facts facts using(object_route_id)
			where rating.id=73`).Scan(&counted, &count, &score)
		if err != nil || counted != wantCounted || count != wantCount || score != wantSum {
			t.Fatalf("final eligibility=%t count=%d score=%d, want %t/%d/%d: %v", counted, count, score, wantCounted, wantCount, wantSum, err)
		}
	}
	t.Run("eligibility update waits for an uncommitted edit", func(t *testing.T) {
		runPopularityLockOrder(t, ctx, pool,
			`select 1 from content_ratings where id=73 for update`,
			`update users set security_score=30 where id=71`,
			`update content_ratings set overall_score=5 where id=73`)
		assertRating(t, false, 0, 0)
	})
	t.Run("new vote waits for the current account", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `delete from content_ratings where id=73; update users set security_score=30 where id=71`); err != nil {
			t.Fatal(err)
		}
		runPopularityLockOrder(t, ctx, pool,
			`update users set security_score=80 where id=71`,
			`insert into content_ratings(id,object_route_id,author_id,overall_score)
			 select 73,id,71,3 from public_routes where entity_type='mod' and internal_id=72`,
			`select 1`)
		assertRating(t, true, 1, 3)
	})
	t.Run("remove a rated project", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `update users set security_score=80 where id=71;
			insert into content_rating_scores(rating_id,dimension_code,score) values(73,'quality',4);
			delete from mods where id=72`); err != nil {
			t.Fatal(err)
		}
		var votes, dimensions, count, sum int
		err := pool.QueryRow(ctx, `select (select count(*) from content_ratings),(select count(*) from content_rating_scores),
			rating_count,rating_sum from content_rating_global_stats where entity_type='mod'`).Scan(&votes, &dimensions, &count, &sum)
		if err != nil || votes != 0 || dimensions != 0 || count != 0 || sum != 0 {
			t.Fatalf("after removing project votes=%d dimensions=%d global=%d/%d: %v", votes, dimensions, count, sum, err)
		}
	})

}

func runPopularityLockOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, hold, wait, finish string) {
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
