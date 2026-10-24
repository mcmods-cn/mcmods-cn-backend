package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

// Keeps the original ten-million test's five-minute context, one SQL request,
// expected counts, recipient index and no-notification-sequential-scan gate.
// The added variable is the real pg_snapshot watermark used after read-all.
func TestAuditMsgReviewSnapshotWatermarkUnreadSampleScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 for snapshot-watermark scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	cfg.ConnConfig.Tracer = counter
	cfg.MaxConns = 1
	cfg.MinConns = 1
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Keep an unrelated old top-level XID open while newer notification rows
	// commit. An xmin-only shortcut cannot prove those rows visible cheaply.
	oldConfig := cfg.Copy()
	oldPool, err := pgxpool.NewWithConfig(ctx, oldConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer oldPool.Close()
	oldTransaction, err := oldPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer oldTransaction.Rollback(context.Background())
	var oldXID string
	if err = oldTransaction.QueryRow(ctx, `select pg_current_xact_id()::text`).Scan(&oldXID); err != nil {
		t.Fatal(err)
	}
	t.Log("an owned unrelated old top-level transaction remains active during all snapshot-watermark measurements")
	if _, err = db.Exec(ctx, `create temporary table users(id bigint primary key,public_id text not null,status text not null);
 create temporary table notifications(id bigint primary key,recipient_id bigint,updated_at timestamptz not null,revision_xid xid8 not null default pg_current_xact_id());
 create index idx_notifications_recipient_id on notifications(recipient_id,id);
 create index idx_notifications_broadcast_id on notifications(id) where recipient_id is null;
 create index idx_notifications_broadcast_page on notifications(updated_at desc,id desc) where recipient_id is null;
 create index idx_notifications_broadcast_revision on notifications(revision_xid,id) include(updated_at) where recipient_id is null;
 create temporary table notification_receipts(notification_id bigint not null,user_id bigint not null,read_at timestamptz,read_revision_xid xid8,primary key(notification_id,user_id));
 create temporary table notification_read_watermarks(user_id bigint primary key,max_notification_id bigint not null,read_at timestamptz not null,updated_at timestamptz not null,visibility_snapshot pg_snapshot);
 create temporary table notification_broadcast_state(singleton boolean primary key,live_count bigint not null,max_notification_id bigint not null);
 create temporary table direct_messages(id bigint primary key,recipient_id bigint not null,read_at timestamptz);
 create index idx_direct_messages_recipient_read on direct_messages(recipient_id,read_at);
 insert into users select value,'u'||lpad(value::text,8,'0'),'active' from generate_series(1,20) value;
 insert into notification_broadcast_state values(true,0,0)`); err != nil {
		t.Fatal(err)
	}
	users := make([]int64, unreadReconcileMaxSampleSize)
	for i := range users {
		users[i] = int64(i + 1)
	}
	previous := int64(0)
	for _, total := range []int64{100_000, 1_000_000, 10_000_000} {
		started := time.Now()
		if _, err = db.Exec(ctx, `insert into notifications(id,recipient_id,updated_at) select value,null,timestamptz '2026-01-01 00:00:00+00'+value*interval '1 microsecond' from generate_series($1::bigint,$2::bigint) value`, previous+1, total); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `update notification_broadcast_state set live_count=$1,max_notification_id=$1 where singleton`, total); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `analyze notifications`); err != nil {
			t.Fatal(err)
		}
		for _, user := range users {
			var timestamp time.Time
			if err = db.QueryRow(ctx, markAllNotificationsReadSQL, user).Scan(&timestamp); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = db.Exec(ctx, `analyze notification_read_watermarks`); err != nil {
			t.Fatal(err)
		}
		t.Logf("snapshot watermark fixture from%d to%d loaded in%s, users=%d", previous, total, time.Since(started), len(users))
		counter.queries.Store(0)
		started = time.Now()
		items, err := loadUnreadTruth(ctx, db, unreadTruthInternalUsersSQL, users)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != unreadReconcileMaxSampleSize || counter.queries.Load() != 1 {
			t.Fatalf("scale%d rows%d queries%d", total, len(items), counter.queries.Load())
		}
		for _, item := range items {
			if item.Summary.Notifications != 0 || item.Summary.Messages != 0 {
				t.Fatalf("scale%d user%d count%+v", total, item.UserID, item.Summary)
			}
		}
		plan := unreadScalePlan(t, ctx, db, users)
		t.Logf("scale%d with pg_snapshot: elapsed=%s\n%s", total, elapsed, plan)
		if strings.Contains(plan, fmt.Sprintf("Rows Removed by Filter: %d", total)) {
			t.Fatalf("snapshot lookup filtered the entire historical broadcast prefix at%d rows", total)
		}
		if strings.Contains(plan, "Seq Scan on notifications") || !strings.Contains(plan, "idx_notifications_recipient_id") {
			t.Fatalf("original index/O(1)-state plan gate failed with snapshot watermark at%d rows", total)
		}
		previous = total
	}
}
