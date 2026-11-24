package database

// notificationVisibilitySchemaStatements installs transaction-based visibility.
// xid8 records the top-level transaction, including writes inside savepoints.
// Snapshot visibility, rather than sequence allocation or statement time,
// determines which notification revision a read-all request actually observed.
func notificationVisibilitySchemaStatements() []string {
	return []string{
		`create table if not exists realtime_connection_leases (
		lease_id text primary key,user_id bigint not null references users(id) on delete cascade,
		session_hash text not null,expires_at timestamptz not null)`,
		`create index if not exists idx_realtime_connection_leases_expiry on realtime_connection_leases(expires_at)`,
		`create or replace function stamp_notification_revision() returns trigger language plpgsql as $$
		begin new.revision_xid := pg_current_xact_id(); return new; end $$`,
		`create or replace trigger trg_notifications_revision before insert or update on notifications
		for each row execute function stamp_notification_revision()`,
	}
}
