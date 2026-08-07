package database

// commentSchemaStatements installs the shared tree-comment model used by every
// public content page. Public IDs are resolved once at the API boundary;
// comment rows and indexes use numeric object IDs exclusively.
func commentSchemaStatements() []string {
	return []string{
		`create table comments (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			target_type text not null,
			target_id bigint not null,
			target_version_id bigint references mod_content_versions(id) on delete cascade,
			author_id bigint not null references users(id) on delete restrict,
			parent_id bigint references comments(id) on delete restrict,
			root_id bigint references comments(id) on delete restrict,
			depth integer not null default 0 check(depth between 0 and 256),
			body text not null,
			status text not null default 'published',
			child_count integer not null default 0 check(child_count >= 0),
			descendant_count integer not null default 0 check(descendant_count >= 0),
			like_count integer not null default 0 check(like_count >= 0),
			unique_reply_users integer not null default 0 check(unique_reply_users >= 0),
			watch_count integer not null default 0 check(watch_count >= 0),
			quality_score numeric(8,6) not null default 0 check(quality_score between 0 and 1),
			hot_score numeric(16,6) not null default 0 check(hot_score >= 0),
			last_reply_at timestamptz,
			idempotency_key text not null default '',
			pinned_at timestamptz,
			pinned_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			deleted_at timestamptz,
			check(target_type ~ '^[a-z][a-z0-9_]{1,63}$'),
			check(status in ('published','pending','hidden','deleted','spam')),
			check((target_type='mod_resource' and target_version_id is not null) or
			      (target_type<>'mod_resource' and target_version_id is null)),
			check((parent_id is null and root_id is null and depth=0) or
			      (parent_id is not null and root_id is not null and depth>0))
		)`,
		`create unique index idx_comments_author_idempotency
			on comments(author_id,idempotency_key) where idempotency_key<>''`,
		`create index idx_comments_author_created
			on comments(author_id,created_at desc)`,
		`create index idx_comments_target_roots
			on comments(target_type,target_id,target_version_id,pinned_at desc,created_at desc,id desc) where parent_id is null`,
		`create index idx_comments_root_created on comments(root_id,created_at,id)`,
		`create index idx_comments_parent_created on comments(parent_id,created_at,id)`,
		`create index idx_comments_target_hot
			on comments(target_type,target_id,target_version_id,pinned_at desc,hot_score desc,id desc)
			where parent_id is null and status='published'`,
		`create table comment_closure (
			ancestor_id bigint not null references comments(id) on delete cascade,
			descendant_id bigint not null references comments(id) on delete cascade,
			depth integer not null check(depth between 0 and 256),
			primary key(ancestor_id,descendant_id)
		)`,
		`create index idx_comment_closure_descendant on comment_closure(descendant_id,depth,ancestor_id)`,
		`create table comment_reactions (
			comment_id bigint not null references comments(id) on delete cascade,
			user_id bigint not null references users(id) on delete cascade,
			reaction text not null,
			created_at timestamptz not null default now(),
			primary key(comment_id,user_id,reaction),
			check(reaction in ('thumbs_up','thumbs_down','laugh','hooray','confused','heart','rocket','eyes'))
		)`,
		`create table comment_watches (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			user_id bigint not null references users(id) on delete cascade,
			comment_id bigint not null references comments(id) on delete cascade,
			status text not null default 'active',
			muted_until timestamptz,
			muted_forever boolean not null default false,
			unread_count integer not null default 0 check(unread_count >= 0),
			watched_reply_count integer not null default 0 check(watched_reply_count >= 0),
			last_activity_at timestamptz not null default now(),
			last_read_comment_id bigint references comments(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			cancelled_at timestamptz,
			unique(user_id,comment_id),
			check(status in ('active','cancelled'))
		)`,
		`create index idx_comment_watches_user_activity
			on comment_watches(user_id,status,last_activity_at desc,id desc)`,
		`create index idx_comment_watches_comment_active
			on comment_watches(comment_id,user_id) where status='active'`,
		`create table comment_watch_replies (
			watch_id bigint not null references comment_watches(id) on delete cascade,
			comment_id bigint not null references comments(id) on delete cascade,
			notification_id bigint references notifications(id) on delete set null,
			read_at timestamptz,
			created_at timestamptz not null default now(),
			primary key(watch_id,comment_id)
		)`,
		`create index idx_comment_watch_replies_unread
			on comment_watch_replies(watch_id,created_at,comment_id) where read_at is null`,
		`create table comment_reports (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			comment_id bigint not null references comments(id) on delete cascade,
			reporter_id bigint not null references users(id) on delete cascade,
			reason text not null,
			detail text not null default '',
			status text not null default 'pending',
			reviewer_id bigint references users(id) on delete set null,
			resolution_note text not null default '',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			resolved_at timestamptz,
			unique(comment_id,reporter_id),
			check(status in ('pending','resolved','dismissed'))
		)`,
		`create index idx_comment_reports_queue on comment_reports(status,created_at,id)`,
		`create table comment_heat_refresh_queue (
			comment_id bigint primary key references comments(id) on delete cascade,
			attempts integer not null default 0 check(attempts>=0),
			available_at timestamptz not null default now(),
			locked_at timestamptz,
			last_error text not null default '',
			updated_at timestamptz not null default now()
		)`,
		`create index idx_comment_heat_refresh_ready
			on comment_heat_refresh_queue(available_at,updated_at,comment_id)`,
		`create or replace function enqueue_comment_heat_refresh(changed_comment_id bigint) returns void as $$
		declare root_comment_id bigint;
		begin
			select coalesce(root_id,id) into root_comment_id from comments where id=changed_comment_id;
			if root_comment_id is null then return; end if;
			insert into comment_heat_refresh_queue(comment_id,available_at,locked_at,last_error,updated_at)
			values(root_comment_id,now(),null,'',now())
			on conflict(comment_id) do update set available_at=least(comment_heat_refresh_queue.available_at,excluded.available_at),
				locked_at=null,last_error='',updated_at=now();
		end;
		$$ language plpgsql`,
		`create or replace function refresh_comment_heat(changed_comment_id bigint) returns void as $$
		declare
			root_comment_id bigint;
			root_author_id bigint;
			root_created_at timestamptz;
			root_body text;
			root_status text;
			likes integer;
			reply_users integer;
			weighted_replies numeric;
			watches integer;
			activity_count bigint;
			author_created_at timestamptz;
			author_status text;
			author_security_score integer;
			author_level integer;
			user_score numeric;
			quality numeric;
			trust numeric;
			decay numeric;
			calculated_hot numeric;
			latest_reply timestamptz;
		begin
			select coalesce(root_id,id) into root_comment_id from comments where id=changed_comment_id;
			if root_comment_id is null then return; end if;
			select author_id,created_at,body,status into root_author_id,root_created_at,root_body,root_status
			from comments where id=root_comment_id and parent_id is null;
			if not found then return; end if;

			select count(distinct user_id) into likes from comment_reactions
			where comment_id=root_comment_id and reaction='thumbs_up' and user_id<>root_author_id;
			select count(*),coalesce(sum(reply_weight),0),max(last_reply)
			into reply_users,weighted_replies,latest_reply
			from (
				select author_id,max(1.0/greatest(depth,1)) reply_weight,max(created_at) last_reply
				from comments where root_id=root_comment_id and status='published' and author_id<>root_author_id
				group by author_id
			) distinct_repliers;
			select count(distinct user_id) into watches from comment_watches
			where comment_id=root_comment_id and status='active' and user_id<>root_author_id;
			select count(*) into activity_count from user_activity_events
			where user_id=root_author_id and occurred_at>=now()-interval '90 days';
			select created_at,status,security_score into author_created_at,author_status,author_security_score
			from users where id=root_author_id;
			select coalesce(level,0) into author_level from user_experience where user_id=root_author_id;
			if not found then author_level:=0; end if;

			user_score:=least(1,ln(1+activity_count::numeric)/ln(51::numeric));
			quality:=least(1,ln(1+char_length(root_body)::numeric)/ln(501::numeric));
			trust:=case
				when author_status<>'active' or author_security_score<40 then 0.2
				when author_created_at>now()-interval '30 days' then 0.5
				when activity_count>=20 or author_level>=5 then 1.2
				else 1 end;
			decay:=power(2::numeric,-extract(epoch from (now()-coalesce(latest_reply,root_created_at)))/1209600.0);
			calculated_hot:=case when root_status='published' then round((
				0.30*ln(1+likes::numeric)+
				0.30*ln(1+weighted_replies)+
				0.15*user_score+0.10*quality+
				0.15*ln(1+watches::numeric)
			)*decay*trust,6) else 0 end;

			update comments set like_count=likes,unique_reply_users=reply_users,watch_count=watches,
				quality_score=quality,hot_score=greatest(0,calculated_hot),last_reply_at=latest_reply
			where id=root_comment_id;
		end;
		$$ language plpgsql`,
		`create or replace function refresh_comment_heat_from_comment() returns trigger as $$
		begin
			perform enqueue_comment_heat_refresh(case when tg_op='DELETE' then coalesce(old.root_id,old.id) else coalesce(new.root_id,new.id) end);
			if tg_op='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_comments_heat after insert or update of body,status or delete on comments
			for each row execute function refresh_comment_heat_from_comment()`,
		`create or replace function refresh_comment_heat_from_reaction() returns trigger as $$
		begin
			perform enqueue_comment_heat_refresh(case when tg_op='DELETE' then old.comment_id else new.comment_id end);
			if tg_op='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_comment_reactions_heat after insert or delete on comment_reactions
			for each row execute function refresh_comment_heat_from_reaction()`,
		`create or replace function refresh_comment_heat_from_watch() returns trigger as $$
		begin
			perform enqueue_comment_heat_refresh(case when tg_op='DELETE' then old.comment_id else new.comment_id end);
			if tg_op='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_comment_watches_heat after insert or update of status or delete on comment_watches
			for each row execute function refresh_comment_heat_from_watch()`,
		`create or replace function register_comment_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id,canonical_path)
			values(new.public_id,'comment',new.id,'/comments/'||new.public_id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_comments_public_route after insert on comments
			for each row execute function register_comment_public_route()`,
	}
}
