package httpapi

import (
	"mcmods-cn-backend/internal/database"
	"testing"
)

func TestAuditNEXT001AdditiveUpgradePreservesReadFacts(t *testing.T) {
	f := newTEST013Fixture(t)
	user := f.userIDs[f.editor]
	if _, err := f.db.Exec(f.ctx, `drop trigger trg_notifications_revision on notifications;
		drop function stamp_notification_revision();
		alter table notifications drop column revision_xid;
		alter table notification_receipts drop column read_revision_xid;
		alter table notification_read_watermarks drop column visibility_snapshot;`); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.db.QueryRow(f.ctx, `insert into notifications(recipient_id,kind,title,body) values($1,'review','Synthetic migration','Retained') returning id`, user).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into notification_receipts(notification_id,user_id,read_at) values($1,$2,clock_timestamp())`, id, user); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into notification_read_watermarks(user_id,max_notification_id,read_at) values($2,$1,clock_timestamp())`, id, user); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range database.NotificationVisibilitySchemaStatements() {
			if _, err = tx.Exec(f.ctx, statement); err != nil {
				tx.Rollback(f.ctx)
				t.Fatal(err)
			}
		}
		if err = tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		var notifications, receipts, watermarks int
		var versionMatches bool
		if err = f.db.QueryRow(f.ctx, `select (select count(*) from notifications),(select count(*) from notification_receipts),(select count(*) from notification_read_watermarks),
			(select r.read_revision_xid=n.revision_xid from notification_receipts r join notifications n on n.id=r.notification_id where n.id=$1 and r.user_id=$2)`, id, user).Scan(&notifications, &receipts, &watermarks, &versionMatches); err != nil {
			t.Fatal(err)
		}
		if notifications != 1 || receipts != 1 || watermarks != 1 || !versionMatches {
			t.Fatalf("round %d changed retained facts: %d/%d/%d matched=%t", round, notifications, receipts, watermarks, versionMatches)
		}
	}
}
