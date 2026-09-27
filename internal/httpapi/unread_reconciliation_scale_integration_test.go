package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestUnreadCachedSampleStaysConstantAtTenMillionBroadcastsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate unread sampling at 100k, 1m and 10m broadcasts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `
		create temporary table users(id bigint primary key,public_id text not null,status text not null);
		create temporary table notifications(id bigint primary key,recipient_id bigint,updated_at timestamptz not null);
		create index idx_notifications_recipient_id on notifications(recipient_id,id);
		create index idx_notifications_broadcast_id on notifications(id) where recipient_id is null;
		create temporary table notification_receipts(notification_id bigint not null,user_id bigint not null,read_at timestamptz,primary key(notification_id,user_id));
		create temporary table notification_read_watermarks(user_id bigint primary key,max_notification_id bigint not null,read_at timestamptz not null);
		create temporary table notification_broadcast_state(singleton boolean primary key,live_count bigint not null,max_notification_id bigint not null);
		create temporary table direct_messages(id bigint primary key,recipient_id bigint not null,read_at timestamptz);
		create index idx_direct_messages_recipient_read on direct_messages(recipient_id,read_at);
		insert into users select value,'u'||lpad(value::text,8,'0'),'active' from generate_series(1,20) value;
		insert into notification_broadcast_state values(true,0,0)
	`); err != nil {
		t.Fatal(err)
	}
	userIDs := make([]int64, 0, unreadReconcileMaxSampleSize)
	for userID := int64(1); userID <= int64(unreadReconcileMaxSampleSize); userID++ {
		userIDs = append(userIDs, userID)
	}
	previous := int64(0)
	for _, total := range []int64{100_000, 1_000_000, 10_000_000} {
		fixtureStarted := time.Now()
		if _, err = pool.Exec(ctx, `insert into notifications(id,recipient_id,updated_at)
			select value,null,timestamptz '2026-01-01 00:00:00+00'+value*interval '1 microsecond'
			from generate_series($1::bigint,$2::bigint) value`, previous+1, total); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `update notification_broadcast_state set live_count=$1,max_notification_id=$1 where singleton`, total); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `analyze notifications`); err != nil {
			t.Fatal(err)
		}
		t.Logf("broadcast fixture advanced from %d to %d in %s", previous, total, time.Since(fixtureStarted))
		counter.queries.Store(0)
		truth, loadErr := loadUnreadTruth(ctx, pool, unreadTruthInternalUsersSQL, userIDs)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if counter.queries.Load() != 1 || len(truth) != unreadReconcileMaxSampleSize {
			t.Fatalf("rows=%d queries=%d at total=%d", len(truth), counter.queries.Load(), total)
		}
		for _, item := range truth {
			if item.Summary.Notifications != total || item.Summary.Messages != 0 {
				t.Fatalf("user=%d truth=%+v at total=%d", item.UserID, item.Summary, total)
			}
		}
		plan := unreadScalePlan(t, ctx, pool, userIDs)
		if strings.Contains(plan, "Seq Scan on notifications") || !strings.Contains(plan, "idx_notifications_recipient_id") {
			t.Fatalf("unread sample did not stay index/O(1)-state bounded at %d rows:\n%s", total, plan)
		}
		t.Logf("%d broadcast unread sample plan:\n%s", total, plan)
		previous = total
	}
}

func unreadScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userIDs []int64) string {
	t.Helper()
	rows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+unreadTruthInternalUsersSQL, userIDs)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	lines := make([]string, 0)
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
