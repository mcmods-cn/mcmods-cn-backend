package database

import (
	"fmt"
	"strings"

	"mcmods-cn-backend/internal/activitycatalog"
)

func activityDictionarySeed(table string, definitions []activitycatalog.Entry) string {
	values := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		code := strings.ReplaceAll(definition.Code, "'", "''")
		name := strings.ReplaceAll(definition.Name, "'", "''")
		values = append(values, fmt.Sprintf("(%d,'%s','%s')", definition.ID, code, name))
	}
	return "insert into " + table + "(id,code,name) values\n\t\t\t" + strings.Join(values, ",") +
		"\n\t\t on conflict(id) do update set code=excluded.code,name=excluded.name"
}

func communitySchemaStatements() []string {
	return []string{
		`create table if not exists creator_role_definitions (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			code text not null unique,
			name text not null,
			description text not null default '',
			translations jsonb not null default '{}'::jsonb,
			is_custom boolean not null default false,
			permission_granting boolean not null default false,
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`insert into creator_role_definitions(id,code,name,description,is_custom,permission_granting) values
			(1,'owner','Owner','Project owner or primary maintainer',false,true),
			(2,'developer','Developer','Software developer',false,true),
			(3,'artist','Artist','Art and visual design',false,false),
			(4,'mascot','Mascot','Team mascot',false,false),
			(5,'former_developer','Former developer','Former software developer',false,false),
			(6,'former_artist','Former artist','Former art and visual designer',false,false),
			(7,'former_owner','Former owner','Former project owner',false,false),
			(8,'sponsor','Sponsor','Team sponsor',false,false),
			(9,'leader','Leader','Team leader',false,false),
			(10,'contributor','Contributor','General contribution without project access',false,false),
			(11,'maintainer','Maintainer','Software maintainer with project access',false,true)
		 on conflict(id) do update set code=excluded.code,name=excluded.name,description=excluded.description,
			permission_granting=excluded.permission_granting`,
		`select setval(pg_get_serial_sequence('creator_role_definitions','id'),
			greatest(11,coalesce((select max(id) from creator_role_definitions),11)),true)`,
		`create or replace function register_creator_role_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,'creator_role',new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_creator_roles_public_route after insert on creator_role_definitions
			for each row execute function register_creator_role_public_route()`,
		`create or replace function remove_creator_role_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='creator_role';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_creator_roles_remove_public_route after delete on creator_role_definitions
			for each row execute function remove_creator_role_public_route()`,
		`insert into public_routes(public_id,entity_type,internal_id)
			select public_id,'creator_role',id from creator_role_definitions
			on conflict do nothing`,
		`create table if not exists creators (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			kind text not null check(kind in ('author','team')),
			name text not null,
			normalized_name text not null,
			description_markdown text not null default '',
			avatar_url text not null default '',
			avatar_file_id bigint references oss_files(id) on delete set null,
			created_by bigint references users(id) on delete set null,
			review_status text not null default 'pending'
				check(review_status in ('pending','approved','rejected')),
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create index if not exists idx_creators_kind_normalized_name
			on creators(kind,normalized_name)`,
		`create index if not exists idx_creators_catalog
			on creators(kind,review_status,lower(name),id)`,
		`create table if not exists creator_links (
			id bigserial primary key,
			creator_id bigint not null references creators(id) on delete cascade,
			link_type text not null,
			url text not null,
			label text not null default '',
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			unique(creator_id,link_type,url)
		)`,
		`create index if not exists idx_creator_links_order
			on creator_links(creator_id,display_order,id)`,
		`create table if not exists creator_team_members (
			team_id bigint not null references creators(id) on delete cascade,
			member_creator_id bigint not null references creators(id) on delete cascade,
			role_id bigint not null references creator_role_definitions(id) on delete restrict,
			title text not null default '',
			status text not null default 'pending'
				check(status in ('pending','approved','rejected','revoked')),
			created_by bigint references users(id) on delete set null,
			approved_by bigint references users(id) on delete set null,
			approved_at timestamptz,
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(team_id,member_creator_id,role_id),
			check(team_id <> member_creator_id)
		)`,
		`create index if not exists idx_creator_team_members_member
			on creator_team_members(member_creator_id,team_id)`,
		`create or replace function enforce_team_author_membership() returns trigger as $$
		declare team_kind text; member_kind text;
		begin
			select kind into team_kind from creators where id=new.team_id;
			select kind into member_kind from creators where id=new.member_creator_id;
			if team_kind is distinct from 'team' or member_kind is distinct from 'author' then
				raise exception 'team memberships must connect a team to a personal author' using errcode='23514';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_creator_team_members_kinds
			before insert or update of team_id,member_creator_id on creator_team_members
			for each row execute function enforce_team_author_membership()`,
		`create table if not exists creator_claims (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			creator_id bigint not null references creators(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			proof_markdown text not null default '',
			status text not null default 'pending'
				check(status in ('pending','approved','rejected','withdrawn','revoked')),
			reviewed_by bigint references users(id) on delete set null,
			review_note text not null default '',
			created_at timestamptz not null default now(),
			reviewed_at timestamptz,
			revoked_at timestamptz
		)`,
		`create table if not exists creator_claim_attachments (
			claim_id bigint not null references creator_claims(id) on delete cascade,
			oss_file_id bigint not null references oss_files(id) on delete restrict,
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			primary key(claim_id,oss_file_id)
		)`,
		`create index if not exists idx_creator_claim_attachments_file
			on creator_claim_attachments(oss_file_id,claim_id)`,
		`create unique index if not exists idx_creator_claims_open
			on creator_claims(creator_id,user_id) where status='pending'`,
		`create unique index uq_creator_claims_approved_author
			on creator_claims(creator_id) where status='approved'`,
		`create index if not exists idx_creator_claims_queue
			on creator_claims(status,created_at,id)`,
		`create or replace function enforce_personal_author_claim() returns trigger as $$
		declare creator_kind text;
		begin
			select kind into creator_kind from creators where id=new.creator_id;
			if creator_kind is distinct from 'author' then
				raise exception 'only personal authors can be claimed' using errcode='23514';
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_creator_claims_personal_author
			before insert or update of creator_id on creator_claims
			for each row execute function enforce_personal_author_claim()`,
		`create or replace function register_creator_claim_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,'creator_claim',new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_creator_claims_public_route after insert on creator_claims
			for each row execute function register_creator_claim_public_route()`,
		`create or replace function remove_creator_claim_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and entity_type='creator_claim';
			return old;
		end;
		$$ language plpgsql`,
		`create trigger trg_creator_claims_remove_public_route after delete on creator_claims
			for each row execute function remove_creator_claim_public_route()`,
		`do $$ begin
			alter table content_creator_bindings add constraint fk_content_creator_bindings_creator
				foreign key(creator_id) references creators(id) on delete restrict;
			exception when duplicate_object then null; end $$`,
		`do $$ begin
			alter table content_creator_bindings add constraint fk_content_creator_bindings_role
				foreign key(role_id) references creator_role_definitions(id) on delete set null;
			exception when duplicate_object then null; end $$`,
		`create index if not exists idx_content_creator_bindings_creator
			on content_creator_bindings(creator_id,subject_type,subject_id)`,
		`create or replace function register_creator_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,new.kind,new.id,
				case when new.kind='team' then '/teams/' else '/authors/' end || new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`drop trigger if exists trg_creators_public_route on creators`,
		`create trigger trg_creators_public_route after insert on creators
			for each row execute function register_creator_public_route()`,
		`create or replace function remove_creator_public_route() returns trigger as $$
		begin
			delete from public_routes where public_id=old.public_id and internal_id=old.id;
			return old;
		end;
		$$ language plpgsql`,
		`drop trigger if exists trg_creators_remove_public_route on creators`,
		`create trigger trg_creators_remove_public_route after delete on creators
			for each row execute function remove_creator_public_route()`,

		`create table if not exists activity_actions (
			id smallint primary key,
			code text not null unique,
			name text not null
		)`,
		activityDictionarySeed("activity_actions", activitycatalog.ActionDefinitions()),
		`create table if not exists activity_object_types (
			id smallint primary key,
			code text not null unique,
			name text not null
		)`,
		activityDictionarySeed("activity_object_types", activitycatalog.ObjectTypeDefinitions()),
		`create table if not exists user_activity_events (
			id bigserial primary key,
			user_id bigint references users(id) on delete set null,
			action_id smallint not null references activity_actions(id) on delete restrict,
			object_type_id smallint not null references activity_object_types(id) on delete restrict,
			object_route_id bigint references public_routes(id) on delete set null,
			markdown_added_bytes integer not null default 0 check(markdown_added_bytes >= 0),
			occurred_at timestamptz not null
		)`,
		`create index if not exists idx_activity_user_time
			on user_activity_events(user_id,occurred_at desc,id desc)`,
		`create index if not exists idx_activity_object_time
			on user_activity_events(object_type_id,object_route_id,occurred_at desc,id desc)`,
		`create index if not exists idx_activity_time_brin
			on user_activity_events using brin(occurred_at)`,
		`create index if not exists idx_change_requests_contributor
			on change_requests(submitted_by,status,entity_type,entity_id)
			where submitted_by is not null`,
		`create table if not exists user_daily_contributions (
			user_id bigint not null references users(id) on delete cascade,
			contribution_date date not null,
			contribution_count integer not null check(contribution_count > 0),
			primary key(user_id,contribution_date)
		)`,
		`create or replace function sync_user_daily_contribution() returns trigger as $$
		declare
			old_date date;
			new_date date;
		begin
			if tg_op='UPDATE' and old.status='approved' and old.submitted_by is not null and old.entity_type is not null then
				old_date := (coalesce(old.resolved_at,old.submitted_at) at time zone 'UTC')::date;
				delete from user_daily_contributions
				where user_id=old.submitted_by and contribution_date=old_date and contribution_count=1;
				update user_daily_contributions
				set contribution_count=contribution_count-1
				where user_id=old.submitted_by and contribution_date=old_date and contribution_count>1;
			end if;
			if new.status='approved' and new.submitted_by is not null and new.entity_type is not null then
				new_date := (coalesce(new.resolved_at,new.submitted_at) at time zone 'UTC')::date;
				insert into user_daily_contributions(user_id,contribution_date,contribution_count)
				values(new.submitted_by,new_date,1)
				on conflict(user_id,contribution_date) do update
				set contribution_count=user_daily_contributions.contribution_count+1;
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`drop trigger if exists trg_change_requests_daily_contribution on change_requests`,
		`create trigger trg_change_requests_daily_contribution
			after insert or update of status,resolved_at,submitted_by,entity_type on change_requests
			for each row execute function sync_user_daily_contribution()`,
		`insert into user_daily_contributions(user_id,contribution_date,contribution_count)
		 select submitted_by,(coalesce(resolved_at,submitted_at) at time zone 'UTC')::date,count(*)::integer
		 from change_requests
		 where status='approved' and submitted_by is not null and entity_type is not null
		 group by submitted_by,(coalesce(resolved_at,submitted_at) at time zone 'UTC')::date
		 on conflict(user_id,contribution_date) do update
		 set contribution_count=excluded.contribution_count`,

		`create table if not exists currencies (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			code text not null unique,
			name text not null,
			description text not null default '',
			icon text not null default '',
			translations jsonb not null default '{}'::jsonb,
			transfer_tax_bps integer not null default 0 check(transfer_tax_bps between 0 and 10000),
			status text not null default 'active' check(status in ('active','disabled')),
			display_order integer not null default 0,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`insert into currencies(code,name,description,icon,display_order) values
			('gold_nugget','Gold Nugget','Default small-value currency','minecraft:gold_nugget',10),
			('diamond','Diamond','Default high-value currency','minecraft:diamond',20),
			('emerald','Emerald','Default trade currency','minecraft:emerald',30)
		 on conflict(code) do nothing`,
		`create table if not exists user_currency_balances (
			user_id bigint not null references users(id) on delete cascade,
			currency_id bigint not null references currencies(id) on delete restrict,
			balance bigint not null default 0 check(balance between 0 and 100000000000000),
			updated_at timestamptz not null default now(),
			primary key(user_id,currency_id)
		)`,
		`create table if not exists currency_transactions (
			id bigserial primary key,
			user_id bigint not null references users(id) on delete cascade,
			currency_id bigint not null references currencies(id) on delete restrict,
			amount_delta bigint not null check(amount_delta between -100000000000000 and 100000000000000),
			balance_after bigint not null check(balance_after between 0 and 100000000000000),
			transaction_type text not null,
			counterparty_user_id bigint references users(id) on delete set null,
			reference_type text not null default '',
			reference_key text not null default '',
			metadata jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_currency_transactions_user
			on currency_transactions(user_id,currency_id,created_at desc,id desc)`,
		`create index if not exists idx_currency_transactions_reference
			on currency_transactions(reference_type,reference_key,created_at desc)`,
		`create table if not exists content_download_counters (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			owner_id bigint not null references users(id) on delete cascade,
			downloads bigint not null default 0 check(downloads between 0 and 100000000000000),
			last_download_at timestamptz,
			primary key(object_route_id,owner_id)
		)`,
		`create index if not exists idx_content_download_counters_owner
			on content_download_counters(owner_id,downloads desc)`,
		`create table if not exists content_download_reward_counters (
			object_route_id bigint not null references public_routes(id) on delete cascade,
			owner_id bigint not null references users(id) on delete cascade,
			currency_id bigint not null references currencies(id) on delete restrict,
			rewarded_steps bigint not null default 0 check(rewarded_steps between 0 and 100000000000000),
			updated_at timestamptz not null default now(),
			primary key(object_route_id,owner_id,currency_id)
		)`,
		`create table if not exists user_checkins (
			user_id bigint not null references users(id) on delete cascade,
			local_date date not null,
			timezone text not null,
			claimed_at timestamptz not null default now(),
			primary key(user_id,local_date)
		)`,
		`create index if not exists idx_user_checkins_claimed
			on user_checkins(user_id,claimed_at desc)`,
		`create table if not exists user_timezone_changes (
			id bigserial primary key,
			user_id bigint not null references users(id) on delete cascade,
			old_timezone text not null,
			new_timezone text not null,
			changed_at timestamptz not null default now()
		)`,
		`create index if not exists idx_user_timezone_changes
			on user_timezone_changes(user_id,changed_at desc)`,
		`create table if not exists shop_items (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			code text not null unique,
			item_type text not null check(item_type in ('profile_background','project_heat_boost','server_heat_boost')),
			name text not null,
			description text not null default '',
			icon text not null default '',
			translations jsonb not null default '{}'::jsonb,
			price_currency_id bigint not null references currencies(id) on delete restrict,
			price_amount bigint not null default 0 check(price_amount between 0 and 1000000000000),
			purchase_permission text not null default '',
			use_permission text not null default '',
			config jsonb not null default '{}'::jsonb,
			status text not null default 'active' check(status in ('active','disabled')),
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`insert into shop_items(code,item_type,name,description,icon,price_currency_id,price_amount,purchase_permission,use_permission)
		 select 'profile_background','profile_background','Profile background',
				'Allows one custom profile background image','image',
				currency.id,10,'shop.profile_background.purchase','shop.profile_background.use'
		 from currencies currency where currency.code='diamond'
		 on conflict(code) do nothing`,
		`insert into shop_items(code,item_type,name,description,icon,translations,price_currency_id,price_amount,purchase_permission,use_permission,config)
		 select item.code,item.item_type,item.name,item.description,'fire',item.translations::jsonb,currency.id,5,item.purchase_permission,item.use_permission,
			'{"power":1,"halfLifeHours":72,"durationHours":432}'::jsonb
		 from currencies currency cross join (values
			('project_heat_boost','project_heat_boost','Project heat boost','Temporarily raises one project''s discovery heat',
			 '{"zh-CN":{"name":"项目热度提升","description":"临时提升一个模组、整合包或其他大型资源的展示热度"},"en-US":{"name":"Project heat boost","description":"Temporarily raises one project''s discovery heat"}}',
			 'shop.project_heat_boost.purchase','shop.project_heat_boost.use'),
			('server_heat_boost','server_heat_boost','Server heat boost','Temporarily raises one Minecraft server''s discovery heat',
			 '{"zh-CN":{"name":"服务器热度提升","description":"临时提升一个 Minecraft 服务器的展示热度"},"en-US":{"name":"Server heat boost","description":"Temporarily raises one Minecraft server''s discovery heat"}}',
			 'shop.server_heat_boost.purchase','shop.server_heat_boost.use')
		 ) item(code,item_type,name,description,translations,purchase_permission,use_permission)
		 where currency.code='diamond' on conflict(code) do nothing`,
		`create table if not exists user_inventory (
			user_id bigint not null references users(id) on delete cascade,
			shop_item_id bigint not null references shop_items(id) on delete restrict,
			quantity integer not null default 0 check(quantity between 0 and 1000000),
			updated_at timestamptz not null default now(),
			primary key(user_id,shop_item_id)
		)`,
		`create table if not exists shop_purchases (
			id bigserial primary key,
			user_id bigint not null references users(id) on delete cascade,
			shop_item_id bigint not null references shop_items(id) on delete restrict,
			currency_id bigint not null references currencies(id) on delete restrict,
			quantity integer not null check(quantity between 1 and 100),
			unit_price bigint not null check(unit_price between 0 and 1000000000000),
			total_price bigint not null check(total_price between 0 and 100000000000000),
			created_at timestamptz not null default now(),
			check(total_price=unit_price*quantity::bigint)
		)`,
		`alter table users add column if not exists profile_background_url text not null default ''`,
		`alter table users add column if not exists profile_background_file_id bigint references oss_files(id) on delete set null`,

		`create table if not exists user_experience (
			user_id bigint primary key references users(id) on delete cascade,
			experience bigint not null default 0 check(experience >= 0),
			level integer not null default 0 check(level >= 0),
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists experience_transactions (
			id bigserial primary key,
			user_id bigint not null references users(id) on delete cascade,
			amount_delta bigint not null,
			experience_after bigint not null check(experience_after >= 0),
			reason text not null,
			reference_type text not null default '',
			reference_key text not null default '',
			metadata jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now()
		)`,
		`create index if not exists idx_experience_transactions_user
			on experience_transactions(user_id,created_at desc,id desc)`,
		`create table if not exists level_system_config (
			singleton boolean primary key default true check(singleton),
			role_track_code text references permission_role_tracks(code) on delete set null,
			level_thresholds bigint[] not null default '{}'::bigint[],
			version bigint not null default 1 check(version > 0),
			updated_by bigint references users(id) on delete set null,
			updated_at timestamptz not null default now()
		)`,
		`insert into level_system_config(singleton) values(true) on conflict(singleton) do nothing`,
		`create table if not exists level_recalculation_jobs (
			id bigserial primary key,
			config_version bigint not null unique check(config_version > 0),
			role_track_code text not null default '',
			level_thresholds bigint[] not null default '{}'::bigint[],
			role_ids bigint[] not null default '{}'::bigint[],
			status text not null default 'queued'
				check(status in ('queued','processing','completed','superseded','dead')),
			cursor_user_id bigint not null default 0 check(cursor_user_id >= 0),
			processed_count bigint not null default 0 check(processed_count >= 0),
			attempts integer not null default 0 check(attempts >= 0),
			max_attempts integer not null default 8 check(max_attempts between 1 and 20),
			next_attempt_at timestamptz not null default now(),
			locked_by text not null default '',
			lease_expires_at timestamptz,
			last_error text not null default '',
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			started_at timestamptz,
			finished_at timestamptz,
			updated_at timestamptz not null default now(),
			check(cardinality(level_thresholds)=cardinality(role_ids)),
			check((status='processing' and locked_by<>'' and lease_expires_at is not null)
				or (status<>'processing' and locked_by='' and lease_expires_at is null))
		)`,
		`create index idx_level_recalculation_jobs_ready
			on level_recalculation_jobs(next_attempt_at,id)
			where status='queued' and attempts<max_attempts`,
		`create index idx_level_recalculation_jobs_lease
			on level_recalculation_jobs(lease_expires_at,id)
			where status='processing'`,
		`create unique index idx_level_recalculation_jobs_active
			on level_recalculation_jobs((true))
			where status in ('queued','processing')`,
		`create table if not exists task_definitions (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			code text not null unique,
			name text not null,
			description text not null default '',
			icon text not null default '',
			translations jsonb not null default '{}'::jsonb,
			refresh_period text not null default 'never'
				check(refresh_period in ('never','daily','weekly','monthly')),
			condition jsonb not null default '{}'::jsonb,
			rewards jsonb not null default '{}'::jsonb,
			status text not null default 'active' check(status in ('active','disabled')),
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		)`,
		`create table if not exists user_task_progress (
			user_id bigint not null references users(id) on delete cascade,
			task_id bigint not null references task_definitions(id) on delete cascade,
			period_key text not null,
			progress bigint not null default 0 check(progress >= 0),
			completed_at timestamptz,
			rewarded_at timestamptz,
			updated_at timestamptz not null default now(),
			primary key(user_id,task_id,period_key)
		)`,
		`create index if not exists idx_user_task_progress_active
			on user_task_progress(user_id,updated_at desc)`,
		`insert into system_settings(key,value) values
			('economy.config','{"checkin":{"enabled":true,"currency":"gold_nugget","amount":1,"minimumHours":20},"downloadRewards":[]}'::jsonb)
		 on conflict(key) do nothing`,

		`create or replace function register_community_public_route() returns trigger as $$
		declare route_type text;
		declare route_path text;
		begin
			route_type := TG_ARGV[0];
			route_path := TG_ARGV[1] || new.public_id;
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,route_type,new.id,route_path);
			return new;
		end;
		$$ language plpgsql`,
		`drop trigger if exists trg_currencies_public_route on currencies`,
		`create trigger trg_currencies_public_route after insert on currencies
			for each row execute function register_community_public_route('currency','/economy/currencies/')`,
		`drop trigger if exists trg_shop_items_public_route on shop_items`,
		`create trigger trg_shop_items_public_route after insert on shop_items
			for each row execute function register_community_public_route('shop_item','/shop/')`,
		`drop trigger if exists trg_task_definitions_public_route on task_definitions`,
		`create trigger trg_task_definitions_public_route after insert on task_definitions
			for each row execute function register_community_public_route('task','/tasks/')`,
		`insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		 select public_id,'currency',id,'/economy/currencies/'||public_id from currencies
		 on conflict(public_id) do nothing`,
		`insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		 select public_id,'shop_item',id,'/shop/'||public_id from shop_items
		 on conflict(public_id) do nothing`,
		`insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		 select public_id,'task',id,'/tasks/'||public_id from task_definitions
		 on conflict(public_id) do nothing`,
	}
}
