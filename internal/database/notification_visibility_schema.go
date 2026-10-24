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
		and exists(select 1 from pg_trigger trigger_row join pg_proc routine on routine.oid=trigger_row.tgfoid
        join pg_namespace routine_namespace on routine_namespace.oid=routine.pronamespace
        where trigger_row.tgrelid='public.notifications'::regclass and trigger_row.tgname='trg_notifications_revision'
        and trigger_row.tgenabled='O' and trigger_row.tgtype=23 and trigger_row.tgqual is null and not trigger_row.tgisinternal
        and routine_namespace.nspname='public' and routine.proname='stamp_notification_revision' and routine.pronargs=0
        and routine.prorettype='pg_catalog.trigger'::regtype
        and routine.prolang=(select oid from pg_language where lanname='plpgsql')
        and regexp_replace(lower(routine.prosrc),'[[:space:]]','','g')='beginnew.revision_xid:=pg_current_xact_id();returnnew;end')
        and (select count(*)=4 from (values ('lease_id','pg_catalog.text'::regtype::oid),('user_id','pg_catalog.int8'::regtype::oid),
        ('session_hash','pg_catalog.text'::regtype::oid),('expires_at','pg_catalog.timestamptz'::regtype::oid)) expected(name,type_oid)
        join pg_attribute attribute on attribute.attrelid=to_regclass('public.realtime_connection_leases') and attribute.attname=expected.name
        and attribute.atttypid=expected.type_oid and attribute.attnotnull and not attribute.attisdropped)`).Scan(&ready)
	if err != nil {
		return err
	}
	if ready {
		ready, err = NotificationVisibilityIndexReady(ctx, query)
		if err != nil {
			return err
		}
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
		`create index if not exists idx_notifications_broadcast_revision on notifications(revision_xid,id) include(updated_at) where recipient_id is null`,
		`alter table notification_receipts add column if not exists read_revision_xid xid8`,
		`create or replace function stamp_notification_revision() returns trigger language plpgsql as $$
		begin new.revision_xid := pg_current_xact_id(); return new; end $$`,
		`create or replace trigger trg_notifications_revision before insert or update on notifications
		for each row execute function stamp_notification_revision()`,
		`update notification_receipts receipt set read_revision_xid=n.revision_xid
		from notifications n where receipt.notification_id=n.id and receipt.read_at is not null and receipt.read_revision_xid is null`,
	}
}

// NotificationVisibilityIndexReady is shared by startup and the explicit
// migration command. A same-named index with another table/order/predicate is
// not evidence that the snapshot-candidate query has its required access path.
func NotificationVisibilityIndexReady(ctx context.Context, query logRedactionQueryer) (bool, error) {
	var ready bool
	err := query.QueryRow(ctx, `select exists(select 1 from pg_index index_row
 join pg_class index_relation on index_relation.oid=index_row.indexrelid
 join pg_am method on method.oid=index_relation.relam
 join pg_attribute revision on revision.attrelid=index_row.indrelid and revision.attname='revision_xid'
 join pg_attribute identity on identity.attrelid=index_row.indrelid and identity.attname='id'
 join pg_attribute updated on updated.attrelid=index_row.indrelid and updated.attname='updated_at'
 where index_row.indexrelid=to_regclass('public.idx_notifications_broadcast_revision')
 and index_row.indrelid=to_regclass('public.notifications') and index_row.indisvalid and index_row.indisready
 and method.amname='btree' and index_row.indnkeyatts=2 and index_row.indnatts=3
 and index_row.indkey[0]=revision.attnum and index_row.indkey[1]=identity.attnum and index_row.indkey[2]=updated.attnum
 and index_row.indoption[0]=0 and index_row.indoption[1]=0 and index_row.indexprs is null
 and regexp_replace(lower(pg_get_expr(index_row.indpred,index_row.indrelid)),'[[:space:]()]','','g')='recipient_idisnull')`).Scan(&ready)
	return ready, err
}
