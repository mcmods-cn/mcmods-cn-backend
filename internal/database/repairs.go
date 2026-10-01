package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Deployed generation-85 installation SQL stays immutable. Small compatible
// repairs are recorded transactionally; dirty existing data stops installation
// with counts and is never deleted or silently merged.
func applySchemaRepairsTx(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `create table if not exists schema_repair_history (
  code text primary key,applied_at timestamptz not null default now())`); err != nil {
		return err
	}
	if err := applyContentTreeAndFavoriteRepairTx(ctx, tx); err != nil {
		return err
	}
	return applyBlueprintLeaseRepairTx(ctx, tx)
}

func applyContentTreeAndFavoriteRepairTx(ctx context.Context, tx pgx.Tx) error {
	const code = "audit-2026-10-content-tree-and-favorite"
	var applied bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from schema_repair_history where code=$1)`, code).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	if _, err := tx.Exec(ctx, `lock table mod_content_sections in share row exclusive mode`); err != nil {
		return err
	}
	var roots, systemKeys, cycles int64
	if err := tx.QueryRow(ctx, `select count(*) from (select version_id,ordinal from mod_content_sections
  where parent_id is null group by version_id,ordinal having count(*)>1) duplicates`).Scan(&roots); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `select count(*) from (select version_id,system_key from mod_content_sections
  where parent_id is null and system_key<>'' and status='active'
  group by version_id,system_key having count(*)>1) duplicates`).Scan(&systemKeys); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `with recursive ancestry as (
  select id origin,id,parent_id,array[id] path,false cycle from mod_content_sections
  union all select ancestry.origin,parent.id,parent.parent_id,ancestry.path||parent.id,parent.id=any(ancestry.path)
  from ancestry join mod_content_sections parent on parent.id=ancestry.parent_id where not ancestry.cycle
 ) select count(distinct origin) from ancestry where cycle`).Scan(&cycles); err != nil {
		return err
	}
	if roots+systemKeys+cycles > 0 {
		return fmt.Errorf("schema repair requires manual data review: duplicate root ordinals=%d, duplicate active root system keys=%d, cyclic section origins=%d", roots, systemKeys, cycles)
	}
	statements := []string{
		`create unique index idx_mod_content_sections_root_ordinal on mod_content_sections(version_id,ordinal) where parent_id is null`,
		`create unique index idx_mod_content_sections_root_system_key on mod_content_sections(version_id,system_key) where parent_id is null and system_key<>'' and status='active'`,
		`create or replace function validate_mod_content_section_tree() returns trigger as $$
   declare parent_mod bigint;parent_version bigint;parent_depth integer;has_cycle boolean;
   begin
    perform pg_advisory_xact_lock(hashtext('mcmods-content-section-tree'),(new.version_id % 2147483647)::integer);
    if new.parent_id is null then return new; end if;
    with recursive parents as (
     select section.id,section.parent_id,section.mod_id,section.version_id,1 depth,array[section.id] path,section.id=new.id cycle
     from mod_content_sections section where section.id=new.parent_id
     union all select section.id,section.parent_id,section.mod_id,section.version_id,parents.depth+1,
      parents.path||section.id,section.id=new.id or section.id=any(parents.path)
     from mod_content_sections section join parents on section.id=parents.parent_id where not parents.cycle and parents.depth<6
    ) select max(mod_id),max(version_id),max(depth),coalesce(bool_or(cycle),false)
     into parent_mod,parent_version,parent_depth,has_cycle from parents;
    if has_cycle then raise exception 'content section tree cannot contain a cycle' using errcode='23514'; end if;
    if parent_mod is null or parent_mod<>new.mod_id then raise exception 'section parent must belong to the same mod' using errcode='23514'; end if;
    if parent_version<>new.version_id then raise exception 'section parent must belong to the same data version' using errcode='23514'; end if;
    if parent_depth>=5 then raise exception 'content category depth exceeds the supported limit' using errcode='23514'; end if;
    return new;
   end; $$ language plpgsql`,
	}
	for _, statement := range ratingSchemaStatements() {
		if strings.HasPrefix(statement, "create or replace function refresh_popularity_from_favorite()") {
			statement = strings.ReplaceAll(statement, "user_id bigint;", "favorite_actor_id bigint;")
			statement = strings.ReplaceAll(statement, "into user_id", "into favorite_actor_id")
			statement = strings.ReplaceAll(statement, "content_user_trust(user_id)", "content_user_trust(favorite_actor_id)")
			statement = strings.ReplaceAll(statement, "if user_id is distinct", "if favorite_actor_id is distinct")
			statement = strings.ReplaceAll(statement, "collection.user_id=user_id", "collection.user_id=favorite_actor_id")
			statements = append(statements, statement)
		}
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply schema repair %s: %w", code, err)
		}
	}
	_, err := tx.Exec(ctx, `insert into schema_repair_history(code) values($1)`, code)
	return err
}

// Stop old blueprint workers before enabling recovery. Old binaries do not
// fence late writes, although they remain schema-compatible with these fields.
func applyBlueprintLeaseRepairTx(ctx context.Context, tx pgx.Tx) error {
	const code = "audit-2026-10-blueprint-worker-lease"
	var applied bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from schema_repair_history where code=$1)`, code).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	if _, err := tx.Exec(ctx, `alter table blueprint_jobs add column run_token text not null default '',add column heartbeat_at timestamptz;
        create index idx_blueprint_jobs_processing_heartbeat on blueprint_jobs(heartbeat_at,id) where status='processing'`); err != nil {
		return fmt.Errorf("apply blueprint lease repair: %w", err)
	}
	_, err := tx.Exec(ctx, `insert into schema_repair_history(code) values($1)`, code)
	return err
}
