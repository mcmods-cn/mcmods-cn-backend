package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestSEC032ConcurrentCommentWatchesCannotExceedTheUserQuotaIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the comment watch quota")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = config.Load().DB.ConnString()
	}
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	randomBytes := make([]byte, 8)
	if _, err = rand.Read(randomBytes); err != nil {
		t.Fatal(err)
	}
	schema := "sec032_" + hex.EncodeToString(randomBytes)
	schemaSQL := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "create schema "+schemaSQL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "drop schema "+schemaSQL+" cascade"); err != nil {
			t.Errorf("drop SEC032 schema: %v", err)
		}
	})

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 4
	poolConfig.MinConns = 2
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `create table comment_watches(
		id bigserial primary key,
		public_id text not null unique default substring(md5(random()::text||clock_timestamp()::text),1,9),
		user_id bigint not null,comment_id bigint not null,status text not null default 'active',
		muted_until timestamptz,muted_forever boolean not null default false,
		unread_count integer not null default 0,watched_reply_count integer not null default 0,
		last_activity_at timestamptz not null default now(),last_read_comment_id bigint,
		created_at timestamptz not null default now(),updated_at timestamptz not null default now(),cancelled_at timestamptz,
		unique(user_id,comment_id));
		create index sec032_user_active on comment_watches(user_id) where status='active';
		create function delay_sec032_watch_insert() returns trigger language plpgsql as $$
		begin
			if new.comment_id>=2000 then perform pg_sleep(0.35); end if;
			return new;
		end $$;
		create trigger delay_sec032_watch_insert before insert on comment_watches
			for each row execute function delay_sec032_watch_insert();
		insert into comment_watches(public_id,user_id,comment_id)
		select 'w'||lpad(value::text,8,'0'),7,value from generate_series(1,1999) value`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	type result struct {
		state commentWatchState
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for _, commentID := range []int64{2000, 2001} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			state, watchErr := server.ensureCommentWatch(ctx, 7, commentID)
			results <- result{state: state, err: watchErr}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes, limited := 0, 0
	for value := range results {
		switch {
		case value.err == nil && value.state.Active:
			successes++
		case value.err != nil && strings.Contains(value.err.Error(), "watch limit exceeded"):
			limited++
		default:
			t.Fatalf("unexpected watch result: state=%+v err=%v", value.state, value.err)
		}
	}
	var active int
	if err = pool.QueryRow(ctx, `select count(*) from comment_watches where user_id=7 and status='active'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if successes != 1 || limited != 1 || active != 2000 {
		t.Fatalf("quota race successes=%d limited=%d active=%d, want 1/1/2000", successes, limited, active)
	}
	if state, err := server.ensureCommentWatch(ctx, 7, 1); err != nil || !state.Active {
		t.Fatalf("idempotent active watch at the quota failed: state=%+v err=%v", state, err)
	}
	var activatedCommentID int64
	if err = pool.QueryRow(ctx, `select comment_id from comment_watches
		where user_id=7 and status='active' and comment_id>=2000`).Scan(&activatedCommentID); err != nil {
		t.Fatal(err)
	}
	loserCommentID := int64(4_001) - activatedCommentID
	if _, err = server.ensureCommentWatch(ctx, 7, loserCommentID); err == nil || !strings.Contains(err.Error(), "watch limit exceeded") {
		t.Fatalf("new watch at the quota was not rejected: %v", err)
	}
}
