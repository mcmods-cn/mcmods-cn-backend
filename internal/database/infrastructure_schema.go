package database

// infrastructureSchemaStatements contains the durable version and delivery
// primitives shared by authentication caches, settings caches and reliable
// asynchronous events. Redis and NATS remain derived delivery layers.
func infrastructureSchemaStatements() []string {
	return []string{
		`create table if not exists runtime_versions (
			name text primary key,
			version bigint not null default 1 check(version > 0),
			updated_at timestamptz not null default now()
		)`,
		`insert into runtime_versions(name,version) values('rbac',1),('settings',1),('project_acl',1)
		 on conflict(name) do nothing`,
		`create or replace function bump_runtime_version(target_name text) returns void as $$
		begin
			insert into runtime_versions(name,version,updated_at) values(target_name,2,now())
			on conflict(name) do update set version=runtime_versions.version+1,updated_at=now();
		end;
		$$ language plpgsql`,
		`create or replace function bump_rbac_runtime_version() returns trigger as $$
		begin perform bump_runtime_version('rbac'); return null; end;
		$$ language plpgsql`,
		`drop trigger if exists trg_role_permissions_rbac_version on role_permissions`,
		`create trigger trg_role_permissions_rbac_version after insert or update or delete on role_permissions
			for each statement execute function bump_rbac_runtime_version()`,
		`drop trigger if exists trg_roles_rbac_version on roles`,
		`create trigger trg_roles_rbac_version after insert or update or delete on roles
			for each statement execute function bump_rbac_runtime_version()`,
		`drop trigger if exists trg_permissions_rbac_version on permissions`,
		`create trigger trg_permissions_rbac_version after insert or update or delete on permissions
			for each statement execute function bump_rbac_runtime_version()`,
		`create or replace function bump_settings_runtime_version() returns trigger as $$
		begin perform bump_runtime_version('settings'); return null; end;
		$$ language plpgsql`,
		`drop trigger if exists trg_system_settings_runtime_version on system_settings`,
		`create trigger trg_system_settings_runtime_version after insert or update or delete on system_settings
			for each statement execute function bump_settings_runtime_version()`,
		`create or replace function secure_user_auth_material_change() returns trigger as $$
		begin
			if new.status is distinct from old.status or new.password_hash is distinct from old.password_hash then
				new.auth_version=old.auth_version+1;
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`drop trigger if exists trg_users_secure_auth_material on users`,
		`create trigger trg_users_secure_auth_material before update of status,password_hash on users
			for each row execute function secure_user_auth_material_change()`,
		`alter table nats_outbox add column if not exists event_type text not null default ''`,
		`alter table nats_outbox add column if not exists schema_version integer not null default 1 check(schema_version > 0)`,
		`alter table nats_outbox add column if not exists occurred_at timestamptz not null default now()`,
		`alter table nats_outbox add column if not exists trace_id text not null default ''`,
		`alter table nats_outbox add column if not exists status text not null default 'pending'`,
		`alter table nats_outbox add column if not exists available_at timestamptz not null default now()`,
		`alter table nats_outbox add column if not exists locked_at timestamptz`,
		`alter table nats_outbox add column if not exists locked_by text not null default ''`,
		`alter table nats_outbox add column if not exists max_attempts integer not null default 12 check(max_attempts > 0)`,
		`alter table nats_outbox add column if not exists updated_at timestamptz not null default now()`,
		`update nats_outbox set event_type=subject where event_type=''`,
		`update nats_outbox set status=case when published_at is null then 'pending' else 'published' end`,
		`alter table nats_outbox drop constraint if exists nats_outbox_status_check`,
		`alter table nats_outbox add constraint nats_outbox_status_check check(status in ('pending','publishing','published','failed','dead'))`,
		`drop index if exists idx_nats_outbox_pending`,
		`create index idx_nats_outbox_pending on nats_outbox(available_at,id)
			where status in ('pending','failed') and published_at is null`,
		`create index if not exists idx_nats_outbox_status_created on nats_outbox(status,created_at)`,
		`alter table notifications add column if not exists source_event_id text`,
		`create unique index if not exists uq_notifications_source_event on notifications(source_event_id)
			where source_event_id is not null`,
		`create table if not exists processed_events (
			consumer text not null,
			event_id text not null,
			event_type text not null,
			processed_at timestamptz not null default now(),
			primary key(consumer,event_id)
		)`,
		`create index if not exists idx_processed_events_created on processed_events(processed_at)`,
		`create table if not exists dead_letter_events (
			id bigserial primary key,
			event_id text not null,
			event_type text not null,
			subject text not null,
			payload jsonb not null,
			failure_stage text not null,
			attempts integer not null default 0,
			last_error text not null default '',
			failed_at timestamptz not null default now(),
			replayed_at timestamptz,
			unique(event_id,failure_stage)
		)`,
		`create index if not exists idx_dead_letter_events_pending on dead_letter_events(failed_at desc)
			where replayed_at is null`,
	}
}
