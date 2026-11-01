package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/queue"
)

func TestAuditCMP003BroadcastEmailRecipientPolicyAndSingleConnectionWorkersRecoverExactlyOnceIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "notification.system.publish")
	if _, err := f.db.Exec(f.ctx, `update user_notification_settings set email_enabled=false`); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{f.editor, f.reviewer, f.denied, f.otherEditor} {
		if _, err := f.db.Exec(f.ctx, `insert into user_notification_settings(user_id,email_enabled) values($1,true) on conflict(user_id) do update set email_enabled=true`, f.userIDs[identity]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(f.ctx, `update users set status='disabled' where id=$1`, f.userIDs[f.denied]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update users set email_verified=false where id=$1`, f.userIDs[f.otherEditor]); err != nil {
		t.Fatal(err)
	}
	title := "Owned partial broadcast atomicity"
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/notifications", map[string]any{"title": title, "body": "Synthetic email intents only", "sourceLocale": "en-US", "sendEmail": true}, 202)
	var eventID string
	var raw []byte
	if err := f.db.QueryRow(f.ctx, `select event_id,payload from nats_outbox where event_type='notification.system' and payload->>'title'=$1`, title).Scan(&eventID, &raw); err != nil {
		t.Fatal(err)
	}
	label := "audit-broadcast-" + randomCatalogPublicID()
	workers := make([]*NotificationWorker, 0, 2)
	for range 2 {
		config := f.db.Config()
		config.MaxConns, config.MinConns = 1, 1
		config.ConnConfig.RuntimeParams["application_name"] = label
		pool, err := pgxpool.NewWithConfig(f.ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		workers = append(workers, &NotificationWorker{db: pool, cache: f.server.cache})
	}
	// The base fixture also has one slot. The observer must not borrow that
	// slot while its transaction holds the owned advisory barrier.
	observerConfig := f.db.Config().ConnConfig.Copy()
	observerConfig.RuntimeParams["application_name"] = label + "-observer"
	observer, err := pgx.ConnectConfig(f.ctx, observerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close(context.Background())
	if _, err := f.db.Exec(f.ctx, `create sequence audit_broadcast_email_attempt;
 create function audit_broadcast_second_email_fault() returns trigger language plpgsql as $$ begin
 if new.event_type='notification.email.requested' and nextval('audit_broadcast_email_attempt')=2 then
 raise exception 'AUDIT_SECOND_REQUESTED_EMAIL_UNAVAILABLE' using errcode='XX001',constraint='audit_broadcast_second_email_intent';
 end if;return new;end $$;
 create trigger audit_broadcast_second_email_fault before insert on nats_outbox for each row execute function audit_broadcast_second_email_fault()`); err != nil {
		t.Fatal(err)
	}
	ctx := queue.WithEventID(f.ctx, eventID)
	before := f.facts(t)
	err = workers[0].handleEvent(ctx, raw)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "XX001" || pgerr.ConstraintName != "audit_broadcast_second_email_intent" {
		t.Fatalf("second email intent did not reach exact fault: %v", err)
	}
	var attempts int
	if err = f.db.QueryRow(f.ctx, `select last_value from audit_broadcast_email_attempt`).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("first intent did not precede exact second failure: attempts=%d err=%v", attempts, err)
	}
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, `drop trigger audit_broadcast_second_email_fault on nats_outbox;drop function audit_broadcast_second_email_fault()`); err != nil {
		t.Fatal(err)
	}
	// Both real single-slot consumers must reach the exact owned insert barrier
	// before release. A unique source_event_id then permits just one commit.
	key := time.Now().UnixNano()
	if _, err = f.db.Exec(f.ctx, `create function audit_broadcast_delivery_barrier() returns trigger language plpgsql as $$ begin perform pg_advisory_xact_lock(`+strconv.FormatInt(key, 10)+`);return new;end $$;
 create trigger audit_broadcast_delivery_barrier before insert on notifications for each row execute function audit_broadcast_delivery_barrier()`); err != nil {
		t.Fatal(err)
	}
	hold, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(f.ctx)
	var holderPID int
	if err = hold.QueryRow(f.ctx, `select pg_backend_pid() from pg_advisory_xact_lock($1)`, key).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	for _, worker := range workers {
		go func(w *NotificationWorker) { done <- w.handleEvent(ctx, raw) }(worker)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var waitErr error
	for {
		var count int
		if waitErr = observer.QueryRow(f.ctx, `select count(*) from pg_stat_activity where datname=current_database() and application_name=$1 and $2=any(pg_blocking_pids(pid))`, label, holderPID).Scan(&count); waitErr != nil || count == 2 {
			break
		}
		select {
		case <-ticker.C:
		case <-f.ctx.Done():
			waitErr = f.ctx.Err()
		}
		if waitErr != nil {
			break
		}
	}
	releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 5*time.Second)
	err = hold.Rollback(releaseCtx)
	cancelRelease()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	}
	if waitErr != nil {
		t.Fatalf("both exact owned Worker inserts did not reach barrier: %v", waitErr)
	}
	t.Log("Both exact owned actual Worker inserts observed waiting on this holder before release; each product pool still has exactly one slot")
	if n := f.number(t, `select count(*) from notifications where source_event_id=$1`, eventID); n != 1 {
		t.Fatalf("concurrent source notification count=%d want1", n)
	}
	if n := f.number(t, `select count(*) from nats_outbox where event_type='notification.email.requested' and payload->>'title'=$1`, title); n != 2 {
		t.Fatalf("active/verified opted-in recipient intents=%d want2", n)
	}
	for _, identity := range []string{f.editor, f.reviewer} {
		if n := f.number(t, `select count(*) from nats_outbox where event_type='notification.email.requested' and payload->>'title'=$1 and payload->>'recipientId'=$2::text`, title, strconv.FormatInt(f.userIDs[identity], 10)); n != 1 {
			t.Fatalf("eligible recipient intents=%d want1", n)
		}
	}
	t.Log("Second precise email fault rolls back first intent+notification; two exact-barrier actual Workers with one slot each recover1notification+2recipient intents; disabled/unverified/opted-out excluded; no SMTP/NATS transport")
}
