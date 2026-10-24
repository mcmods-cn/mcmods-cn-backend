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

// The first 10M RED fixture inherited an old no-watermark fixture's missing
// time index. This controlled same-schema comparison includes every relevant
// production index (and even the new XID index for both queries), so a fixture
// omission cannot be mistaken for the old SQL's actual full-prefix scans.
func TestAuditMsgReviewBeforeAndAfterSnapshotPlanWithCompleteIndexesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
 insert into users select value,'u'||value,'active' from generate_series(1,16) value;
 insert into notifications(id,recipient_id,updated_at) select value,null,timestamptz '2026-01-01'+value*interval '1 microsecond' from generate_series(1,100000) value;
 insert into notification_broadcast_state values(true,100000,100000);analyze notifications`); err != nil {
		t.Fatal(err)
	}
	users := make([]int64, 16)
	for i := range users {
		users[i] = int64(i + 1)
		var timestamp time.Time
		if err = db.QueryRow(ctx, markAllNotificationsReadSQL, users[i]).Scan(&timestamp); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(ctx, `analyze notification_read_watermarks`); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile("testdata/audit_msg_review_unread_before.sql")
	if err != nil {
		t.Fatal(err)
	}
	plans := map[string]string{}
	for _, version := range []struct{ name, sql string }{{"before_480cf76", string(before)}, {"after", unreadTruthInternalUsersSQL}} {
		items, err := loadUnreadTruth(ctx, db, version.sql, users)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 16 {
			t.Fatalf("%s rows=%d", version.name, len(items))
		}
		for _, item := range items {
			if item.Summary.Notifications != 0 {
				t.Fatalf("%s changed truth=%+v", version.name, item)
			}
		}
		rows, err := db.Query(ctx, "explain(analyze,buffers,format text) "+version.sql, users)
		if err != nil {
			t.Fatal(err)
		}
		lines := []string{}
		for rows.Next() {
			var line string
			if err = rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, line)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		plans[version.name] = strings.Join(lines, "\n")
		t.Logf("%s complete-index plan:\n%s", version.name, plans[version.name])
	}
	if !strings.Contains(plans["before_480cf76"], "Rows Removed by Filter: 100000") {
		t.Fatal("frozen-query counterexample did not reproduce full historical filtering; inspect plan instead of claiming RED")
	}
	if strings.Contains(plans["after"], "Rows Removed by Filter: 100000") {
		t.Fatal("optimized query still filters the entire old broadcast prefix")
	}
}
