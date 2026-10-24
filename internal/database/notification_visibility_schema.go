package database

import (
	"context"
	"errors"
)

func requireNotificationVisibilitySchema(ctx context.Context, query logRedactionQueryer) error {
	var ready bool
	err := query.QueryRow(ctx, `select
		(select count(*)=3 from information_schema.columns where table_schema='public' and
		(table_name,column_name,udt_name) in (('notifications','revision_xid','xid8'),('notification_receipts','read_revision_xid','xid8'),('notification_read_watermarks','visibility_snapshot','pg_snapshot')))
		and exists(select 1 from pg_trigger where tgrelid='public.notifications'::regclass and tgname='trg_notifications_revision' and tgenabled='O')
		and to_regclass('public.realtime_connection_leases') is not null`).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("notification visibility extension required: run cmd/db-notification-visibility-upgrade explicitly before starting this backend")
	}
	return nil
}

// NotificationVisibilitySchemaStatements is an additive generation-168 extension.
// xid8 records the top-level transaction, including writes inside savepoints.
// Snapshot visibility, rather than sequence allocation or statement time,
// determines which notification revision a read-all request actually observed.
func NotificationVisibilitySchemaStatements() []string {
	return []string{
		`create table if not exists realtime_connection_leases (
		lease_id text primary key,user_id bigint not null references users(id) on delete cascade,
		session_hash text not null,expires_at timestamptz not null)`,
		`create index if not exists idx_realtime_connection_leases_expiry on realtime_connection_leases(expires_at)`,
		`alter table notifications add column if not exists revision_xid xid8 not null default pg_current_xact_id()`,
		`alter table notification_read_watermarks add column if not exists visibility_snapshot pg_snapshot`,
		`alter table notification_receipts add column if not exists read_revision_xid xid8`,
		`create or replace function stamp_notification_revision() returns trigger language plpgsql as $$
		begin new.revision_xid := pg_current_xact_id(); return new; end $$`,
		`create or replace trigger trg_notifications_revision before insert or update on notifications
		for each row execute function stamp_notification_revision()`,
		`update notification_receipts receipt set read_revision_xid=n.revision_xid
		from notifications n where receipt.notification_id=n.id and receipt.read_at is not null and receipt.read_revision_xid is null`,
	}
}
