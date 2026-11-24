package database

import (
	"context"
	"testing"
	"time"
)

func TestNotificationReadFactsSurviveBaselineRestartIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := newBaselineTestDatabase(t, ctx)
	if err := Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var user, id int64
	var initialRevision string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('reader','reader@example.invalid','fixture') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,source_event_id) values($1,'review','Initial','one-source') returning id,revision_xid::text`, user).Scan(&id, &initialRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into notification_receipts(notification_id,user_id,read_at,read_revision_xid)
		select id,$1,clock_timestamp(),revision_xid from notifications where id=$2`, user, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into notification_read_watermarks(user_id,max_notification_id,read_at,visibility_snapshot)
		values($1,$2,clock_timestamp(),pg_current_snapshot())`, user, id); err != nil {
		t.Fatal(err)
	}
	var updatedRevision string
	if err := pool.QueryRow(ctx, `update notifications set body='Changed after read-all' where id=$1 returning revision_xid::text`, id).Scan(&updatedRevision); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var readRevision, currentRevision string
	var readWasVisible, currentWasVisible bool
	if err := pool.QueryRow(ctx, `select receipt.read_revision_xid::text,n.revision_xid::text,
		pg_visible_in_snapshot(receipt.read_revision_xid,watermark.visibility_snapshot),
		pg_visible_in_snapshot(n.revision_xid,watermark.visibility_snapshot)
		from notifications n join notification_receipts receipt on receipt.notification_id=n.id
		join notification_read_watermarks watermark on watermark.user_id=receipt.user_id
		where n.id=$1 and receipt.user_id=$2 and n.source_event_id='one-source'`, id, user).
		Scan(&readRevision, &currentRevision, &readWasVisible, &currentWasVisible); err != nil {
		t.Fatal(err)
	}
	if readRevision != initialRevision || currentRevision != updatedRevision || !readWasVisible || currentWasVisible {
		t.Fatalf("notification read facts changed across restart: read=%s/%t current=%s/%t", readRevision, readWasVisible, currentRevision, currentWasVisible)
	}
}
