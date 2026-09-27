package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	contentStatsPollInterval     = 2 * time.Second
	popularityDecayInterval      = 15 * time.Minute
	siteCounterInterval          = time.Minute
	siteMetricsInterval          = 6 * time.Hour
	contentStatsBatchSize        = 100
	popularityRefreshMaxAttempts = 8
)

const popularityDueStatsEnqueueSQL = `insert into content_stats_refresh_queue(object_route_id,refresh_metrics,refresh_popularity)
	select stats.object_route_id,false,true from content_popularity_stats stats
	where stats.next_decay_at<=now()
	on conflict(object_route_id) do update set refresh_popularity=true,
		status='pending',attempts=case when content_stats_refresh_queue.status='failed' then 0 else content_stats_refresh_queue.attempts end,
		available_at=least(content_stats_refresh_queue.available_at,now()),locked_at=null,updated_at=now()`

type contentStatsRefreshTask struct {
	routeID           int64
	refreshMetrics    bool
	refreshPopularity bool
	attempt           int
}

type commentHeatRefreshTask struct {
	commentID int64
	attempt   int
}

// StartPopularityRefreshScheduler drains durable, coalescing queues. Request
// transactions only increment lightweight counters and mark a numeric route
// dirty; expensive joins and time-decay calculations never block a user write.
// SKIP LOCKED claims make the same queue safe across multiple backend replicas.
func StartPopularityRefreshScheduler(ctx context.Context, db *pgxpool.Pool) {
	if db == nil {
		return
	}
	go func() {
		enqueueTimeDependentStats(ctx, db, true)
		drainStatsQueues(ctx, db)
		refreshSiteDailyMetrics(ctx, db, true)
		refreshSiteCurrentCounters(ctx, db)
		pollTicker := time.NewTicker(contentStatsPollInterval)
		decayTicker := time.NewTicker(popularityDecayInterval)
		siteCounterTicker := time.NewTicker(siteCounterInterval)
		siteMetricsTicker := time.NewTicker(siteMetricsInterval)
		defer pollTicker.Stop()
		defer decayTicker.Stop()
		defer siteCounterTicker.Stop()
		defer siteMetricsTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-pollTicker.C:
				drainStatsQueues(ctx, db)
			case <-decayTicker.C:
				enqueueTimeDependentStats(ctx, db, false)
			case <-siteCounterTicker.C:
				refreshSiteCurrentCounters(ctx, db)
			case <-siteMetricsTicker.C:
				refreshSiteDailyMetrics(ctx, db, false)
			}
		}
	}()
}

func refreshSiteCurrentCounters(ctx context.Context, db *pgxpool.Pool) {
	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := db.Exec(refreshCtx, `select refresh_site_current_counters()`); err != nil {
		log.Printf("refresh administration current counters: %v", err)
	}
}

func refreshSiteDailyMetrics(ctx context.Context, db *pgxpool.Pool, includeToday bool) {
	refreshCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dates := []int{1}
	if includeToday {
		dates = append(dates, 0)
	}
	if _, err := db.Exec(refreshCtx, `select refresh_site_daily_metrics(current_date-day_offset)
		from unnest($1::integer[]) day_offset`, dates); err != nil {
		log.Printf("refresh administration site metrics: %v", err)
	}
}

func drainStatsQueues(ctx context.Context, db *pgxpool.Pool) {
	for batch := 0; batch < 8 && ctx.Err() == nil; batch++ {
		tasks, err := claimContentStatsTasks(ctx, db, contentStatsBatchSize)
		if err != nil {
			log.Printf("claim content statistics refresh tasks: %v", err)
			break
		}
		for _, task := range tasks {
			if err = processContentStatsTask(ctx, db, task); err != nil {
				log.Printf("refresh content statistics for route %d: %v", task.routeID, err)
			}
		}
		if len(tasks) < contentStatsBatchSize {
			break
		}
	}
	for batch := 0; batch < 4 && ctx.Err() == nil; batch++ {
		tasks, err := claimCommentHeatTasks(ctx, db, contentStatsBatchSize)
		if err != nil {
			log.Printf("claim comment heat refresh tasks: %v", err)
			break
		}
		for _, task := range tasks {
			if err = processCommentHeatTask(ctx, db, task); err != nil {
				log.Printf("refresh comment heat for comment %d: %v", task.commentID, err)
			}
		}
		if len(tasks) < contentStatsBatchSize {
			break
		}
	}
}

func claimContentStatsTasks(ctx context.Context, db *pgxpool.Pool, limit int) ([]contentStatsRefreshTask, error) {
	rows, err := db.Query(ctx, `with exhausted as (
		update content_stats_refresh_queue set status='failed',locked_at=null,
			last_error=case when last_error='' then 'processing lease expired at retry limit' else last_error end,updated_at=now()
		where status='processing' and attempts>=$2 and locked_at<now()-interval '5 minutes'
	), candidates as (
		select object_route_id from content_stats_refresh_queue
		where (status='pending' and available_at<=now())
			or (status='processing' and attempts<$2 and locked_at<now()-interval '5 minutes')
		order by available_at,updated_at,object_route_id
		for update skip locked limit $1
	) update content_stats_refresh_queue queue set status='processing',locked_at=now(),attempts=queue.attempts+1,updated_at=now()
	from candidates where queue.object_route_id=candidates.object_route_id
	returning queue.object_route_id,queue.refresh_metrics,queue.refresh_popularity,queue.attempts`, limit, popularityRefreshMaxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]contentStatsRefreshTask, 0, limit)
	for rows.Next() {
		var task contentStatsRefreshTask
		if err = rows.Scan(&task.routeID, &task.refreshMetrics, &task.refreshPopularity, &task.attempt); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func processContentStatsTask(ctx context.Context, db *pgxpool.Pool, task contentStatsRefreshTask) error {
	taskCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := db.Begin(taskCtx)
	if err == nil {
		if task.refreshMetrics {
			_, err = tx.Exec(taskCtx, `select refresh_content_route_metrics($1)`, task.routeID)
		}
		if err == nil && task.refreshPopularity {
			_, err = tx.Exec(taskCtx, `select refresh_content_popularity($1)`, task.routeID)
		}
		if err == nil {
			_, err = tx.Exec(taskCtx, `delete from content_stats_refresh_queue
				where object_route_id=$1 and attempts=$2 and status='processing' and locked_at is not null`, task.routeID, task.attempt)
		}
		if err == nil {
			err = tx.Commit(taskCtx)
		} else {
			_ = tx.Rollback(taskCtx)
		}
	}
	if err == nil {
		return nil
	}
	return errors.Join(err, retryContentStatsTask(ctx, db, task, err))
}

func retryContentStatsTask(ctx context.Context, db *pgxpool.Pool, task contentStatsRefreshTask, cause error) error {
	delaySeconds := 1 << min(task.attempt, 8)
	_, err := db.Exec(ctx, `update content_stats_refresh_queue set locked_at=null,
		status=case when attempts>=$5 then 'failed' else 'pending' end,
		available_at=case when attempts>=$5 then available_at else now()+make_interval(secs=>$3) end,
		last_error=$4,updated_at=now()
		where object_route_id=$1 and attempts=$2 and status='processing'`, task.routeID, task.attempt, delaySeconds,
		truncateWorkerError(cause), popularityRefreshMaxAttempts)
	if err != nil {
		return fmt.Errorf("persist content statistics retry: %w", err)
	}
	return nil
}

func claimCommentHeatTasks(ctx context.Context, db *pgxpool.Pool, limit int) ([]commentHeatRefreshTask, error) {
	rows, err := db.Query(ctx, `with exhausted as (
		update comment_heat_refresh_queue set status='failed',locked_at=null,
			last_error=case when last_error='' then 'processing lease expired at retry limit' else last_error end,updated_at=now()
		where status='processing' and attempts>=$2 and locked_at<now()-interval '5 minutes'
	), candidates as (
		select comment_id from comment_heat_refresh_queue
		where (status='pending' and available_at<=now())
			or (status='processing' and attempts<$2 and locked_at<now()-interval '5 minutes')
		order by available_at,updated_at,comment_id
		for update skip locked limit $1
	) update comment_heat_refresh_queue queue set status='processing',locked_at=now(),attempts=queue.attempts+1,updated_at=now()
	from candidates where queue.comment_id=candidates.comment_id
	returning queue.comment_id,queue.attempts`, limit, popularityRefreshMaxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]commentHeatRefreshTask, 0, limit)
	for rows.Next() {
		var task commentHeatRefreshTask
		if err = rows.Scan(&task.commentID, &task.attempt); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func processCommentHeatTask(ctx context.Context, db *pgxpool.Pool, task commentHeatRefreshTask) error {
	taskCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(taskCtx)
	if err == nil {
		_, err = tx.Exec(taskCtx, `select refresh_comment_heat($1)`, task.commentID)
		if err == nil {
			_, err = tx.Exec(taskCtx, `delete from comment_heat_refresh_queue
				where comment_id=$1 and attempts=$2 and status='processing' and locked_at is not null`, task.commentID, task.attempt)
		}
		if err == nil {
			err = tx.Commit(taskCtx)
		} else {
			_ = tx.Rollback(taskCtx)
		}
	}
	if err == nil {
		return nil
	}
	return errors.Join(err, retryCommentHeatTask(ctx, db, task, err))
}

func retryCommentHeatTask(ctx context.Context, db *pgxpool.Pool, task commentHeatRefreshTask, cause error) error {
	delaySeconds := 1 << min(task.attempt, 8)
	_, err := db.Exec(ctx, `update comment_heat_refresh_queue set locked_at=null,
		status=case when attempts>=$5 then 'failed' else 'pending' end,
		available_at=case when attempts>=$5 then available_at else now()+make_interval(secs=>$3) end,
		last_error=$4,updated_at=now()
		where comment_id=$1 and attempts=$2 and status='processing'`, task.commentID, task.attempt, delaySeconds,
		truncateWorkerError(cause), popularityRefreshMaxAttempts)
	if err != nil {
		return fmt.Errorf("persist comment heat retry: %w", err)
	}
	return nil
}

func enqueueTimeDependentStats(ctx context.Context, db *pgxpool.Pool, includeMissing bool) {
	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if includeMissing {
		if _, err := db.Exec(refreshCtx, `insert into content_stats_refresh_queue(object_route_id,refresh_metrics,refresh_popularity)
			select route.id,true,true from public_routes route
			left join content_popularity_stats stats on stats.object_route_id=route.id
			where route.entity_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon','minecraft_server')
			and stats.object_route_id is null
			on conflict(object_route_id) do update set refresh_metrics=true,refresh_popularity=true,
			status='pending',attempts=case when content_stats_refresh_queue.status='failed' then 0 else content_stats_refresh_queue.attempts end,
			available_at=least(content_stats_refresh_queue.available_at,now()),locked_at=null,updated_at=now()`); err != nil {
			log.Printf("enqueue missing project statistics: %v", err)
		}
	}
	if _, err := db.Exec(refreshCtx, popularityDueStatsEnqueueSQL); err != nil {
		log.Printf("enqueue due time-dependent project statistics: %v", err)
	}
	if _, err := db.Exec(refreshCtx, `insert into comment_heat_refresh_queue(comment_id)
		select comment.id from comments comment where comment.parent_id is null and comment.status='published'
		and (comment.created_at>=now()-interval '90 days' or comment.last_reply_at>=now()-interval '90 days')
		on conflict(comment_id) do update set available_at=least(comment_heat_refresh_queue.available_at,now()),
			status='pending',attempts=case when comment_heat_refresh_queue.status='failed' then 0 else comment_heat_refresh_queue.attempts end,
			locked_at=null,updated_at=now()`); err != nil {
		log.Printf("enqueue time-dependent comment heat: %v", err)
	}
}

func truncateWorkerError(err error) string {
	if err == nil {
		return ""
	}
	message := fmt.Sprintf("%v", err)
	if len(message) > 1000 {
		return message[:1000]
	}
	return message
}
