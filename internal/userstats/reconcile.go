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
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('mcmods-user-statistics:'||$1::text,0))`, userID); err != nil {
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

const activityDailyBackfillSQL = `with per_action as (
	select (event.occurred_at at time zone 'UTC')::date stat_date,action.code,
		count(*) event_count,sum(event.markdown_added_bytes) added_bytes,sum(event.markdown_deleted_bytes) deleted_bytes
	from user_activity_events event join activity_actions action on action.id=event.action_id
	where event.user_id=$1 group by (event.occurred_at at time zone 'UTC')::date,action.code
), rolled as (
	select stat_date,sum(event_count) action_count,sum(event_count) filter(where code='view') view_count,
		sum(event_count) filter(where code='edit') edit_count,sum(event_count) filter(where code='create') create_count,
		sum(event_count) filter(where code='delete') delete_count,sum(added_bytes) added_bytes,sum(deleted_bytes) deleted_bytes,
		jsonb_object_agg(code,event_count) action_counts
	from per_action group by stat_date
), bounds as (
	select (occurred_at at time zone 'UTC')::date stat_date,min(occurred_at) first_at,max(occurred_at) last_at
	from user_activity_events where user_id=$1 group by (occurred_at at time zone 'UTC')::date
)
insert into user_statistics_daily(user_id,stat_date,action_count,view_count,edit_count,create_count,delete_count,
	markdown_added_bytes,markdown_deleted_bytes,action_counts,first_activity_at,last_activity_at)
select $1,rolled.stat_date,action_count,coalesce(view_count,0),coalesce(edit_count,0),coalesce(create_count,0),coalesce(delete_count,0),
	added_bytes,deleted_bytes,action_counts,bounds.first_at,bounds.last_at from rolled join bounds using(stat_date)
on conflict(user_id,stat_date) do update set
	action_count=greatest(user_statistics_daily.action_count,excluded.action_count),
	view_count=greatest(user_statistics_daily.view_count,excluded.view_count),edit_count=greatest(user_statistics_daily.edit_count,excluded.edit_count),
	create_count=greatest(user_statistics_daily.create_count,excluded.create_count),delete_count=greatest(user_statistics_daily.delete_count,excluded.delete_count),
	markdown_added_bytes=greatest(user_statistics_daily.markdown_added_bytes,excluded.markdown_added_bytes),
	markdown_deleted_bytes=greatest(user_statistics_daily.markdown_deleted_bytes,excluded.markdown_deleted_bytes),
	action_counts=case when excluded.action_count>user_statistics_daily.action_count then excluded.action_counts else user_statistics_daily.action_counts end,
	first_activity_at=least(user_statistics_daily.first_activity_at,excluded.first_activity_at),
	last_activity_at=greatest(user_statistics_daily.last_activity_at,excluded.last_activity_at),updated_at=now()`

const activityTotalReconcileSQL = `insert into user_statistics_totals(user_id,action_count,view_count,edit_count,create_count,delete_count,
	markdown_added_bytes,markdown_deleted_bytes,first_activity_at,last_activity_at,last_edit_at,last_comment_at)
select $1,coalesce(sum(action_count),0),coalesce(sum(view_count),0),coalesce(sum(edit_count),0),coalesce(sum(create_count),0),
	coalesce(sum(delete_count),0),coalesce(sum(markdown_added_bytes),0),coalesce(sum(markdown_deleted_bytes),0),
	min(first_activity_at),max(last_activity_at),
	(select max(event.occurred_at) from user_activity_events event where event.user_id=$1 and event.action_id=1),
	(select max(event.occurred_at) from user_activity_events event
		where event.user_id=$1 and event.action_id=2 and event.object_type_id=8)
from user_statistics_daily where user_id=$1
on conflict(user_id) do update set action_count=greatest(user_statistics_totals.action_count,excluded.action_count),
	view_count=greatest(user_statistics_totals.view_count,excluded.view_count),edit_count=greatest(user_statistics_totals.edit_count,excluded.edit_count),
	create_count=greatest(user_statistics_totals.create_count,excluded.create_count),delete_count=greatest(user_statistics_totals.delete_count,excluded.delete_count),
	markdown_added_bytes=greatest(user_statistics_totals.markdown_added_bytes,excluded.markdown_added_bytes),
	markdown_deleted_bytes=greatest(user_statistics_totals.markdown_deleted_bytes,excluded.markdown_deleted_bytes),
	first_activity_at=least(user_statistics_totals.first_activity_at,excluded.first_activity_at),
	last_activity_at=greatest(user_statistics_totals.last_activity_at,excluded.last_activity_at),
	last_edit_at=greatest(user_statistics_totals.last_edit_at,excluded.last_edit_at),
	last_comment_at=greatest(user_statistics_totals.last_comment_at,excluded.last_comment_at),updated_at=now()`
