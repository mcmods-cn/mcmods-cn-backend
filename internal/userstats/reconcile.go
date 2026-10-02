package userstats

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Options struct {
	UserPublicID string
	BatchSize    int
}

func NormalizeOptions(options Options) (Options, error) {
	if options.BatchSize == 0 {
		options.BatchSize = 200
	}
	if options.BatchSize < 1 || options.BatchSize > 1000 {
		return Options{}, errors.New("batch size must be between 1 and 1000")
	}
	return options, nil
}

func Reconcile(ctx context.Context, db *pgxpool.Pool, options Options) (int, error) {
	options, err := NormalizeOptions(options)
	if err != nil {
		return 0, err
	}
	if options.UserPublicID != "" {
		var userID int64
		if err = db.QueryRow(ctx, `select id from users where public_id=$1`, options.UserPublicID).Scan(&userID); err != nil {
			return 0, fmt.Errorf("resolve user: %w", err)
		}
		if err = reconcileUser(ctx, db, userID); err != nil {
			return 0, err
		}
		return 1, nil
	}
	processed := 0
	lastUserID := int64(0)
	for {
		rows, queryErr := db.Query(ctx, `select id from users where id>$1 order by id limit $2`, lastUserID, options.BatchSize)
		if queryErr != nil {
			return processed, queryErr
		}
		ids := make([]int64, 0, options.BatchSize)
		for rows.Next() {
			var userID int64
			if queryErr = rows.Scan(&userID); queryErr != nil {
				rows.Close()
				return processed, queryErr
			}
			ids = append(ids, userID)
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			return processed, queryErr
		}
		rows.Close()
		if len(ids) == 0 {
			return processed, nil
		}
		for _, userID := range ids {
			if err = reconcileUser(ctx, db, userID); err != nil {
				return processed, fmt.Errorf("reconcile user %d: %w", userID, err)
			}
			processed++
			lastUserID = userID
		}
	}
}

func reconcileUser(ctx context.Context, db *pgxpool.Pool, userID int64) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('mcmods-user-statistics:'||($1::bigint)::text,0))`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, reconcileUserLockSQL, userID); err != nil {
		return err
	}
	for _, statement := range contentFactStatements {
		if _, err = tx.Exec(ctx, statement, userID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, activityDailyBackfillSQL, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, activityTotalReconcileSQL, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const reconcileUserLockSQL = `select 1 from users where id=$1 for update`

var contentFactStatements = []string{
	`insert into user_content_creation_facts(content_type,object_key,user_id,review_status,current_exists,created_at)
	 select 'mod',project_code,submitted_by,review_status,true,created_at from mods where submitted_by=$1
	 on conflict(content_type,object_key) do update set user_id=excluded.user_id,review_status=excluded.review_status,current_exists=true,deleted_at=null,updated_at=now()`,
	`insert into user_content_creation_facts(content_type,object_key,user_id,review_status,current_exists,created_at)
	 select 'modpack',public_id,submitted_by,review_status,true,created_at from modpacks where submitted_by=$1
	 on conflict(content_type,object_key) do update set user_id=excluded.user_id,review_status=excluded.review_status,current_exists=true,deleted_at=null,updated_at=now()`,
	`insert into user_content_creation_facts(content_type,object_key,user_id,review_status,current_exists,created_at)
	 select project_type,public_id,submitted_by,review_status,true,created_at from simple_projects where submitted_by=$1
	 on conflict(content_type,object_key) do update set user_id=excluded.user_id,review_status=excluded.review_status,current_exists=true,deleted_at=null,updated_at=now()`,
	`insert into user_content_creation_facts(content_type,object_key,user_id,review_status,current_exists,created_at)
	 select 'server',public_id,submitted_by,review_status,true,created_at from minecraft_servers where submitted_by=$1
	 on conflict(content_type,object_key) do update set user_id=excluded.user_id,review_status=excluded.review_status,current_exists=true,deleted_at=null,updated_at=now()`,
	`insert into user_content_creation_facts(content_type,object_key,user_id,review_status,current_exists,created_at,deleted_at)
	 select kind,public_id,author_id,review_status,status='active',created_at,case when status='deleted' then updated_at end
	 from community_posts where author_id=$1
	 on conflict(content_type,object_key) do update set user_id=excluded.user_id,review_status=excluded.review_status,
	 current_exists=excluded.current_exists,deleted_at=excluded.deleted_at,updated_at=now()`,
}

const activityDailyBackfillSQL = `with per_action_sources as (
	select (event.occurred_at at time zone 'UTC')::date stat_date,event.action_id,
		count(*) event_count,sum(event.markdown_added_bytes) added_bytes,sum(event.markdown_deleted_bytes) deleted_bytes,
		min(event.occurred_at) first_at,max(event.occurred_at) last_at
	from user_activity_events event where event.user_id=$1
	group by (event.occurred_at at time zone 'UTC')::date,event.action_id
	union all
	select retained.stat_date,retained.action_id,retained.event_count,retained.markdown_added_bytes,
		retained.markdown_deleted_bytes,retained.first_activity_at,retained.last_activity_at
	from user_statistics_retained_actions retained where retained.user_id=$1
), per_action as (
	select source.stat_date,action.code,sum(source.event_count) event_count,sum(source.added_bytes) added_bytes,
		sum(source.deleted_bytes) deleted_bytes,min(source.first_at) first_at,max(source.last_at) last_at
	from per_action_sources source join activity_actions action on action.id=source.action_id
	group by source.stat_date,action.code
), rolled as (
	select stat_date,sum(event_count) action_count,coalesce(sum(event_count) filter(where code='view'),0) view_count,
		coalesce(sum(event_count) filter(where code='edit'),0) edit_count,
		coalesce(sum(event_count) filter(where code='create'),0) create_count,
		coalesce(sum(event_count) filter(where code='delete'),0) delete_count,
		sum(added_bytes) added_bytes,sum(deleted_bytes) deleted_bytes,jsonb_object_agg(code,event_count) action_counts,
		min(first_at) first_at,max(last_at) last_at
	from per_action group by stat_date
), stale_removed as (
	delete from user_statistics_daily daily where daily.user_id=$1
		and not exists(select 1 from rolled where rolled.stat_date=daily.stat_date)
	returning daily.stat_date
)
insert into user_statistics_daily(user_id,stat_date,action_count,view_count,edit_count,create_count,delete_count,
	markdown_added_bytes,markdown_deleted_bytes,action_counts,first_activity_at,last_activity_at)
select $1,stat_date,action_count,view_count,edit_count,create_count,delete_count,
	added_bytes,deleted_bytes,action_counts,first_at,last_at from rolled
on conflict(user_id,stat_date) do update set
	action_count=excluded.action_count,view_count=excluded.view_count,edit_count=excluded.edit_count,
	create_count=excluded.create_count,delete_count=excluded.delete_count,
	markdown_added_bytes=excluded.markdown_added_bytes,markdown_deleted_bytes=excluded.markdown_deleted_bytes,
	action_counts=excluded.action_counts,first_activity_at=excluded.first_activity_at,
	last_activity_at=excluded.last_activity_at,updated_at=now()`

const activityTotalReconcileSQL = `insert into user_statistics_totals(user_id,action_count,view_count,edit_count,create_count,delete_count,
	markdown_added_bytes,markdown_deleted_bytes,action_counts,first_activity_at,last_activity_at,last_edit_at,last_comment_at)
with totals as (
	select coalesce(sum(action_count),0) action_count,coalesce(sum(view_count),0) view_count,
		coalesce(sum(edit_count),0) edit_count,coalesce(sum(create_count),0) create_count,
		coalesce(sum(delete_count),0) delete_count,coalesce(sum(markdown_added_bytes),0) added_bytes,
		coalesce(sum(markdown_deleted_bytes),0) deleted_bytes,min(first_activity_at) first_at,max(last_activity_at) last_at
	from user_statistics_daily where user_id=$1
), per_action as (
	select entry.key code,sum(entry.value::bigint) event_count
	from user_statistics_daily daily cross join lateral jsonb_each_text(daily.action_counts) entry
	where daily.user_id=$1 group by entry.key
), actions as (
	select coalesce(jsonb_object_agg(code,event_count),'{}'::jsonb) action_counts from per_action
), recent as (
	select greatest(
		(select max(event.occurred_at) from user_activity_events event where event.user_id=$1 and event.action_id=1),
		(select max(retained.last_activity_at) from user_statistics_retained_actions retained where retained.user_id=$1 and retained.action_id=1)
	) last_edit_at,greatest(
		(select max(event.occurred_at) from user_activity_events event where event.user_id=$1 and event.action_id=2 and event.object_type_id=8),
		(select max(retained.last_comment_at) from user_statistics_retained_actions retained where retained.user_id=$1)
	) last_comment_at
)
select $1,totals.action_count,totals.view_count,totals.edit_count,totals.create_count,totals.delete_count,
	totals.added_bytes,totals.deleted_bytes,actions.action_counts,totals.first_at,totals.last_at,recent.last_edit_at,recent.last_comment_at
from totals cross join actions cross join recent
on conflict(user_id) do update set action_count=excluded.action_count,view_count=excluded.view_count,
	edit_count=excluded.edit_count,create_count=excluded.create_count,delete_count=excluded.delete_count,
	markdown_added_bytes=excluded.markdown_added_bytes,markdown_deleted_bytes=excluded.markdown_deleted_bytes,
	action_counts=excluded.action_counts,first_activity_at=excluded.first_activity_at,last_activity_at=excluded.last_activity_at,
	last_edit_at=excluded.last_edit_at,last_comment_at=excluded.last_comment_at,updated_at=now()`
