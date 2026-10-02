package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	levelRecalculationBatchSize    = 500
	levelRecalculationScanInterval = time.Second
	levelRecalculationLeaseTTL     = 30 * time.Second
)

const levelRecalculationBatchSQL = `with job as materialized (
	select id,cursor_user_id,level_thresholds,role_track_code,role_ids
	from level_recalculation_jobs
	where id=$1 and status='processing' and locked_by=$2 and lease_expires_at>now()
	for update
), candidates as materialized (
	select experience.user_id,experience.experience
	from user_experience experience join job on experience.user_id>job.cursor_user_id
	order by experience.user_id
	limit $3
	for update of experience
), batch as materialized (
	select user_id,experience from candidates order by user_id limit ($3-1)
), calculated as materialized (
	select batch.user_id,
		(select count(*)::integer from unnest(job.level_thresholds) threshold(value)
			where batch.experience>=threshold.value) level,
		job.role_track_code,job.role_ids
	from batch cross join job
), desired as materialized (
	select user_id,level,role_track_code,
		case when level>0 and level<=cardinality(role_ids) then role_ids[level] end target_role_id
	from calculated
), updated_levels as (
	update user_experience experience set level=desired.level,updated_at=now()
	from desired where experience.user_id=desired.user_id and experience.level is distinct from desired.level
	returning experience.user_id
), removed_roles as (
	delete from user_role_bindings binding using desired
	where binding.user_id=desired.user_id and binding.source='level_track'
		and (desired.target_role_id is null or binding.role_id<>desired.target_role_id
			or binding.source_key<>desired.role_track_code)
	returning binding.user_id
), inserted_roles as (
	insert into user_role_bindings(user_id,role_id,source,source_key)
	select desired.user_id,desired.target_role_id,'level_track',desired.role_track_code
	from desired where desired.target_role_id is not null
		and not exists(select 1 from user_role_bindings binding where binding.user_id=desired.user_id
			and binding.role_id=desired.target_role_id and binding.source='level_track'
			and binding.source_key=desired.role_track_code)
	on conflict do nothing returning user_id
), summary as (
	select count(batch.user_id)::bigint batch_count,
		coalesce(max(batch.user_id),job.cursor_user_id)::bigint next_cursor,
		(select count(*) from candidates)<=($3-1) done,
		(select count(*) from updated_levels) updated_level_count,
		(select count(*) from removed_roles) removed_role_count,
		(select count(*) from inserted_roles) inserted_role_count
	from job left join batch on true group by job.cursor_user_id
), updated_job as (
	update level_recalculation_jobs state set
		cursor_user_id=summary.next_cursor,
		processed_count=state.processed_count+summary.batch_count,
		status=case when summary.done then 'completed' else 'processing' end,
		locked_by=case when summary.done then '' else state.locked_by end,
		lease_expires_at=case when summary.done then null else now()+make_interval(secs=>$4) end,
		last_error='',finished_at=case when summary.done then now() else null end,updated_at=now()
	from summary where state.id=$1 and state.status='processing' and state.locked_by=$2
	returning summary.batch_count,summary.done,state.cursor_user_id,state.processed_count,
		summary.updated_level_count,summary.removed_role_count,summary.inserted_role_count
)
select batch_count,done,cursor_user_id,processed_count,updated_level_count,removed_role_count,inserted_role_count
from updated_job`

type LevelRecalculationWorker struct {
	db       *pgxpool.Pool
	workerID string
}

type claimedLevelRecalculationJob struct {
	id        int64
	lockToken string
}

type levelRecalculationBatchResult struct {
	processed, cursor, processedTotal          int64
	updatedLevels, removedRoles, insertedRoles int64
	done                                       bool
}

func NewLevelRecalculationWorker(db *pgxpool.Pool) *LevelRecalculationWorker {
	return &LevelRecalculationWorker{db: db, workerID: "level-recalculation-" + newExportID()}
}

func (worker *LevelRecalculationWorker) Start(ctx context.Context) {
	if worker == nil || worker.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *LevelRecalculationWorker) run(ctx context.Context) {
	worker.drain(ctx)
	ticker := time.NewTicker(levelRecalculationScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.drain(ctx)
		}
	}
}

func (worker *LevelRecalculationWorker) drain(ctx context.Context) {
	for ctx.Err() == nil {
		job, claimed, err := worker.claim(ctx)
		if err != nil {
			slog.Error("claim level recalculation job", "module", "progression", "error", err)
			return
		}
		if !claimed {
			return
		}
		if err = worker.process(ctx, job); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("process level recalculation job", "module", "progression", "job_id", job.id, "error", err)
			persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			persistErr := worker.fail(persistCtx, job, err)
			cancel()
			if persistErr != nil {
				slog.Error("persist level recalculation failure", "module", "progression", "job_id", job.id, "error", persistErr)
			}
		}
	}
}

func (worker *LevelRecalculationWorker) claim(ctx context.Context) (claimedLevelRecalculationJob, bool, error) {
	job := claimedLevelRecalculationJob{lockToken: worker.workerID + ":" + newExportID()}
	err := worker.db.QueryRow(ctx, `with exhausted as (
		update level_recalculation_jobs set status='dead',locked_by='',lease_expires_at=null,
			last_error=case when last_error='' then 'worker lease expired after retry budget was exhausted' else last_error end,
			finished_at=now(),updated_at=now()
		where status='processing' and lease_expires_at<=now() and attempts>=max_attempts
	), candidate as (
		select id from level_recalculation_jobs
		where attempts<max_attempts and (
			(status='queued' and next_attempt_at<=now()) or
			(status='processing' and lease_expires_at<=now())
		) order by config_version desc,id desc for update skip locked limit 1
	) update level_recalculation_jobs job set status='processing',attempts=job.attempts+1,
		locked_by=$1,lease_expires_at=now()+make_interval(secs=>$2),
		started_at=coalesce(job.started_at,now()),finished_at=null,updated_at=now()
	from candidate where job.id=candidate.id returning job.id`,
		job.lockToken, int(levelRecalculationLeaseTTL/time.Second)).Scan(&job.id)
	if errors.Is(err, pgx.ErrNoRows) {
		return claimedLevelRecalculationJob{}, false, nil
	}
	if err != nil {
		return claimedLevelRecalculationJob{}, false, err
	}
	return job, true, nil
}

func (worker *LevelRecalculationWorker) process(ctx context.Context, job claimedLevelRecalculationJob) error {
	for ctx.Err() == nil {
		result, err := worker.processBatch(ctx, job)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if result.done {
			slog.Info("completed level recalculation job", "module", "progression", "job_id", job.id,
				"processed", result.processedTotal)
			return nil
		}
	}
	return ctx.Err()
}

func (worker *LevelRecalculationWorker) processBatch(ctx context.Context, job claimedLevelRecalculationJob) (levelRecalculationBatchResult, error) {
	var result levelRecalculationBatchResult
	err := worker.db.QueryRow(ctx, levelRecalculationBatchSQL, job.id, job.lockToken,
		levelRecalculationBatchSize+1, int(levelRecalculationLeaseTTL/time.Second)).Scan(
		&result.processed, &result.done, &result.cursor, &result.processedTotal,
		&result.updatedLevels, &result.removedRoles, &result.insertedRoles)
	return result, err
}

func (worker *LevelRecalculationWorker) fail(ctx context.Context, job claimedLevelRecalculationJob, cause error) error {
	message := "level recalculation failed"
	if cause != nil {
		message = strings.TrimSpace(cause.Error())
	}
	if len(message) > 2048 {
		message = message[:2048]
	}
	_, err := worker.db.Exec(ctx, `update level_recalculation_jobs set
		status=case when attempts>=max_attempts then 'dead' else 'queued' end,
		next_attempt_at=case when attempts>=max_attempts then next_attempt_at
			else now()+make_interval(secs=>least(900,5*(1<<least(attempts,7)))) end,
		locked_by='',lease_expires_at=null,last_error=$3,
		finished_at=case when attempts>=max_attempts then now() else null end,updated_at=now()
		where id=$1 and status='processing' and locked_by=$2`, job.id, job.lockToken, message)
	return err
}
