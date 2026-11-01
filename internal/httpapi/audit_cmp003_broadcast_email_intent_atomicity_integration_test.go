package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"mcmods-cn-backend/internal/queue"
)

// This verifies durable requested email intent, not SMTP receipt or exactly-once
// delivery by an external mail server. Only the owned synthetic schema is used.
func TestAuditCMP003RequestedBroadcastEmailIntentCannotBeLostAfterNotificationCommitIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "notification.system.publish")
	if _, err := f.db.Exec(f.ctx, `update user_notification_settings set email_enabled=false`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_notification_settings(user_id,email_enabled) values($1,true)
 on conflict(user_id) do update set email_enabled=true`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	title := "Owned requested broadcast email atomicity"
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/notifications", map[string]any{"title": title, "body": "Synthetic result intent, no SMTP execution", "sourceLocale": "en-US", "sendEmail": true}, 202)
	var eventID string
	var raw []byte
	if err := f.db.QueryRow(f.ctx, `select event_id,payload from nats_outbox where event_type='notification.system' and payload->>'title'=$1`, title).Scan(&eventID, &raw); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `create function audit_cmp003_broadcast_email_fault() returns trigger language plpgsql as $$begin
 if new.event_type='notification.email.requested' then
 raise exception 'AUDIT_REQUESTED_EMAIL_INTENT_UNAVAILABLE' using errcode='XX001',constraint='audit_cmp003_broadcast_email_intent';
 end if;return new;end $$;
 create trigger audit_cmp003_broadcast_email_fault before insert on nats_outbox for each row execute function audit_cmp003_broadcast_email_fault()`); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	worker := &NotificationWorker{db: f.db, cache: f.server.cache}
	ctx := queue.WithEventID(f.ctx, eventID)
	err := worker.handleEvent(ctx, raw)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "XX001" || pgerr.ConstraintName != "audit_cmp003_broadcast_email_intent" {
		notifications := f.number(t, `select count(*) from notifications where source_event_id=$1`, eventID)
		emails := f.number(t, `select count(*) from nats_outbox where event_type='notification.email.requested' and payload->>'title'=$1`, title)
		t.Fatalf("precise requested-email failure acknowledged with err=%v notification=%d emailIntents=%d; event redelivery cannot silently skip missing requested intents", err, notifications, emails)
	}
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, `drop trigger audit_cmp003_broadcast_email_fault on nats_outbox;drop function audit_cmp003_broadcast_email_fault()`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = worker.handleEvent(ctx, raw); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.number(t, `select count(*) from notifications where source_event_id=$1`, eventID); n != 1 {
		t.Fatalf("recovery/duplicate persisted %d notifications want1", n)
	}
	if n := f.number(t, `select count(*) from nats_outbox where event_type='notification.email.requested' and payload->>'title'=$1 and payload->>'recipientId'=$2::text`, title, strconv.FormatInt(f.userIDs[f.editor], 10)); n != 1 {
		t.Fatalf("recovery/duplicate persisted %d requested email intents want1", n)
	}
	t.Log("Actual ordinary HTTP announcement and NotificationWorker: preciseXX001 rolls back notification+requested email intents; recovery/duplicate1notification+1intent; no SMTP/NATS transport claim")
}
