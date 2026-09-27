package database

// stickerReferenceSchemaStatements installs the current structured projection
// of sticker tokens found in published and immutable historical Markdown. It
// runs after every content table has been created so trigger coverage is
// derived from the authoritative schema instead of a hand-maintained table
// list.
func stickerReferenceSchemaStatements() []string {
	return []string{
		`create table sticker_content_references (
			source_type text not null,
			source_key text not null,
			source_field text not null,
			pack_code text not null check(pack_code ~ '^[a-z0-9][a-z0-9_-]{0,47}$'),
			sticker_code text not null check(sticker_code ~ '^[a-z0-9][a-z0-9_-]{0,47}$'),
			created_at timestamptz not null default now(),
			primary key(source_type,source_key,source_field,pack_code,sticker_code)
		)`,
		`create index idx_sticker_content_references_token on sticker_content_references(pack_code,sticker_code)`,
		`create or replace function sticker_reference_source_key(row_data jsonb,key_columns text)
		returns text language sql immutable strict as $$
			select jsonb_object_agg(key_column,row_data -> key_column)::text
			from unnest(string_to_array(key_columns,',')) as key_column
		$$`,
		`create or replace function sync_sticker_content_references() returns trigger as $$
		declare
			old_data jsonb;
			new_data jsonb;
			old_key text;
			new_key text;
			old_markdown text := '';
			new_markdown text := '';
			token_row record;
		begin
			if tg_op <> 'INSERT' then
				old_data := to_jsonb(old);
				old_key := sticker_reference_source_key(old_data,tg_argv[1]);
				old_markdown := coalesce(old_data ->> tg_argv[0],'');
			end if;
			if tg_op <> 'DELETE' then
				new_data := to_jsonb(new);
				new_key := sticker_reference_source_key(new_data,tg_argv[1]);
				new_markdown := coalesce(new_data ->> tg_argv[0],'');
			end if;

			for token_row in
				select distinct token_set.pack_code,token_set.sticker_code
				from (
					select old_match[1] pack_code,old_match[2] sticker_code
					from regexp_matches(old_markdown,$pattern$\[sticker:([a-z0-9][a-z0-9_-]{0,47}):([a-z0-9][a-z0-9_-]{0,47})\]$pattern$,'g') old_match
					union
					select new_match[1] pack_code,new_match[2] sticker_code
					from regexp_matches(new_markdown,$pattern$\[sticker:([a-z0-9][a-z0-9_-]{0,47}):([a-z0-9][a-z0-9_-]{0,47})\]$pattern$,'g') new_match
				) token_set
				order by token_set.pack_code,token_set.sticker_code
			loop
				perform pg_advisory_xact_lock(hashtext('sticker-reference'),hashtext(token_row.pack_code||':'||token_row.sticker_code));
			end loop;

			if tg_op <> 'INSERT' then
				delete from sticker_content_references
				where source_type=tg_table_name and source_key=old_key and source_field=tg_argv[0];
			end if;
			if tg_op <> 'DELETE' then
				insert into sticker_content_references(source_type,source_key,source_field,pack_code,sticker_code)
				select tg_table_name,new_key,tg_argv[0],new_match[1],new_match[2]
				from regexp_matches(new_markdown,$pattern$\[sticker:([a-z0-9][a-z0-9_-]{0,47}):([a-z0-9][a-z0-9_-]{0,47})\]$pattern$,'g') new_match
				on conflict do nothing;
			end if;
			return null;
		end
		$$ language plpgsql`,
		`do $$
		declare
			column_row record;
			trigger_suffix text;
		begin
			for column_row in
				select column_info.table_name,column_info.column_name,
					(
						select string_agg(attribute_row.attname,',' order by key_row.position)
						from pg_constraint constraint_row
						cross join lateral unnest(constraint_row.conkey) with ordinality key_row(attnum,position)
						join pg_attribute attribute_row
						  on attribute_row.attrelid=constraint_row.conrelid
						 and attribute_row.attnum=key_row.attnum
						where constraint_row.contype='p'
						  and constraint_row.conrelid=format('%I.%I',column_info.table_schema,column_info.table_name)::regclass
					) key_columns
				from information_schema.columns column_info
				where column_info.table_schema='public'
				  and column_info.data_type in ('text','jsonb')
				  and (
					column_info.column_name like '%\_markdown' escape '\'
					or (column_info.table_name='comments' and column_info.column_name='body')
					or (column_info.table_name='content_revisions' and column_info.column_name='snapshot')
				  )
				order by column_info.table_name,column_info.column_name
			loop
				if column_row.key_columns is null then
					raise exception 'sticker reference source %.% has no primary key',column_row.table_name,column_row.column_name;
				end if;
				trigger_suffix := substr(md5(column_row.table_name||':'||column_row.column_name),1,16);
				execute format(
					'create trigger %I after insert on public.%I for each row execute function sync_sticker_content_references(%L,%L)',
					'trg_sticker_ref_i_'||trigger_suffix,column_row.table_name,column_row.column_name,column_row.key_columns
				);
				execute format(
					'create trigger %I after update of %I on public.%I for each row execute function sync_sticker_content_references(%L,%L)',
					'trg_sticker_ref_u_'||trigger_suffix,column_row.column_name,column_row.table_name,column_row.column_name,column_row.key_columns
				);
				execute format(
					'create trigger %I after delete on public.%I for each row execute function sync_sticker_content_references(%L,%L)',
					'trg_sticker_ref_d_'||trigger_suffix,column_row.table_name,column_row.column_name,column_row.key_columns
				);
			end loop;
		end $$`,
	}
}
