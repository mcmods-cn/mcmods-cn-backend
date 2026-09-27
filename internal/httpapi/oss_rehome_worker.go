package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

const (
	ossRehomePollInterval = 15 * time.Second
	ossRehomeLeaseTimeout = 3 * time.Minute
	ossRehomeRunTimeout   = 2 * time.Minute
	ossRehomeBatchSize    = 2
)

var (
	errOSSRehomeLeaseLost     = errors.New("OSS rehome job lease was lost")
	errOSSRehomeNotReplayable = errors.New("OSS rehome job is not dead")
)

type ossRehomeJob struct {
	ID          int64
	ModID       int64
	Generation  int64
	Attempts    int
	MaxAttempts int
	LockToken   string
}

type OSSRehomeWorker struct {
	server    *Server
	workerID  string
	processFn func(context.Context, int64) error
}

func NewOSSRehomeWorker(cfg config.Config, db *pgxpool.Pool) *OSSRehomeWorker {
	server := &Server{cfg: cfg, db: db}
	worker := &OSSRehomeWorker{server: server, workerID: "oss-rehome-worker-" + newExportID()}
	worker.processFn = server.rehomeModGalleryOSSObjects
	return worker
}

func (worker *OSSRehomeWorker) Start(ctx context.Context) {
	if worker == nil || worker.server == nil || worker.server.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *OSSRehomeWorker) run(ctx context.Context) {
	ticker := time.NewTicker(ossRehomePollInterval)
	defer ticker.Stop()
	worker.drain(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.drain(ctx)
		}
	}
}

func (worker *OSSRehomeWorker) drain(ctx context.Context) {
	if _, err := worker.deadLetterExpiredBudget(ctx); err != nil {
		log.Printf("dead-letter exhausted OSS rehome leases: %v", err)
		return
	}
	for processed := 0; processed < ossRehomeBatchSize; processed++ {
		job, err := worker.claim(ctx)
		if errors.Is(err, pgx.ErrNoRows) || ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("claim OSS rehome job: %v", err)
			return
		}
		runContext, cancel := context.WithTimeout(ctx, ossRehomeRunTimeout)
		err = worker.processFn(runContext, job.ModID)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			state, persistErr := worker.recordFailure(ctx, job, err)
			if persistErr != nil {
				log.Printf("persist OSS rehome failure for job %d: %v", job.ID, persistErr)
			} else if state == "dead" {
				log.Printf("OSS rehome job %d entered dead state after attempt %d/%d: %v", job.ID, job.Attempts, job.MaxAttempts, err)
			}
			continue
		}
		if _, err = worker.complete(ctx, job); err != nil {
			log.Printf("complete OSS rehome job %d: %v", job.ID, err)
		}
	}
}

func enqueueOSSRehomeJobTx(ctx context.Context, tx pgx.Tx, modID int64) error {
	if modID <= 0 {
		return errors.New("invalid mod ID for OSS rehome")
	}
	_, err := tx.Exec(ctx, `insert into oss_rehome_jobs(mod_id) values($1)
		on conflict(mod_id) do update set
			generation=oss_rehome_jobs.generation+1,
			status=case when oss_rehome_jobs.status='processing' then 'processing' else 'queued' end,
			attempts=case when oss_rehome_jobs.status='processing' then oss_rehome_jobs.attempts else 0 end,
			next_attempt_at=case when oss_rehome_jobs.status='processing' then oss_rehome_jobs.next_attempt_at else now() end,
			locked_by=case when oss_rehome_jobs.status='processing' then oss_rehome_jobs.locked_by else '' end,
			lease_expires_at=case when oss_rehome_jobs.status='processing' then oss_rehome_jobs.lease_expires_at else null end,
			last_error=case when oss_rehome_jobs.status='processing' then oss_rehome_jobs.last_error else '' end,
			failure_class=case when oss_rehome_jobs.status='processing' then oss_rehome_jobs.failure_class else '' end,
			completed_at=null,dead_at=null,updated_at=now()`, modID)
	return err
}

func (worker *OSSRehomeWorker) claim(ctx context.Context) (ossRehomeJob, error) {
	job := ossRehomeJob{LockToken: worker.workerID + ":" + newExportID()}
	err := worker.server.db.QueryRow(ctx, `update oss_rehome_jobs set status='processing',attempts=attempts+1,
		locked_by=$2,lease_expires_at=now()+make_interval(secs => $1),claimed_generation=generation,
		completed_at=null,dead_at=null,updated_at=now()
		where id=(select id from oss_rehome_jobs where attempts<max_attempts and
			((status='queued' and next_attempt_at<=now()) or (status='processing' and lease_expires_at<now()))
			order by case when status='processing' then lease_expires_at else next_attempt_at end,id
			for update skip locked limit 1)
		returning id,mod_id,generation,attempts,max_attempts`, int(ossRehomeLeaseTimeout/time.Second), job.LockToken).
		Scan(&job.ID, &job.ModID, &job.Generation, &job.Attempts, &job.MaxAttempts)
	return job, err
}

func (worker *OSSRehomeWorker) complete(ctx context.Context, job ossRehomeJob) (string, error) {
	var state string
	err := worker.server.db.QueryRow(ctx, `update oss_rehome_jobs set
		status=case when generation=$3 then 'completed' else 'queued' end,
		attempts=case when generation=$3 then attempts else 0 end,
		next_attempt_at=now(),locked_by='',lease_expires_at=null,claimed_generation=null,
		last_error='',failure_class='',completed_at=case when generation=$3 then now() else null end,
		dead_at=null,updated_at=now()
		where id=$1 and status='processing' and locked_by=$2 returning status`, job.ID, job.LockToken, job.Generation).
		Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errOSSRehomeLeaseLost
	}
	return state, err
}

func (worker *OSSRehomeWorker) recordFailure(ctx context.Context, job ossRehomeJob, processErr error) (string, error) {
	decision := classifyOSSDeletionFailure(processErr)
	dead := !decision.retryable || job.Attempts >= job.MaxAttempts
	nextAttempt := time.Now().Add(ossDeletionRetryDelay(job.Attempts))
	lastError := truncateOSSDeletionError(processErr)
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var state string
	err = tx.QueryRow(ctx, `update oss_rehome_jobs set
		status=case when generation<>$3 then 'queued' when $4 then 'dead' else 'queued' end,
		attempts=case when generation<>$3 then 0 else attempts end,
		next_attempt_at=case when generation<>$3 then now() else $5 end,
		locked_by='',lease_expires_at=null,claimed_generation=null,
		last_error=case when generation<>$3 then '' else $6 end,
		failure_class=case when generation<>$3 then '' else $7 end,
		completed_at=null,dead_at=case when generation=$3 and $4 then now() else null end,updated_at=now()
		where id=$1 and status='processing' and locked_by=$2 returning status`,
		job.ID, job.LockToken, job.Generation, dead, nextAttempt, lastError, decision.class).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errOSSRehomeLeaseLost
	}
	if err != nil {
		return "", err
	}
	if state == "dead" {
		if err = insertOSSRehomeDeadAlertTx(ctx, tx, job, decision.class, lastError); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return state, nil
}

func (worker *OSSRehomeWorker) deadLetterExpiredBudget(ctx context.Context) (int, error) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select id,mod_id,generation,coalesce(claimed_generation,0),attempts,max_attempts
		from oss_rehome_jobs where attempts>=max_attempts and
			(status='queued' or (status='processing' and lease_expires_at<now()))
		order by case when status='processing' then lease_expires_at else next_attempt_at end,id
		for update skip locked limit $1`, ossRehomeBatchSize)
	if err != nil {
		return 0, err
	}
	jobs := make([]struct {
		id, modID, generation, claimedGeneration int64
		attempts, maxAttempts                    int
	}, 0, ossRehomeBatchSize)
	for rows.Next() {
		var job struct {
			id, modID, generation, claimedGeneration int64
			attempts, maxAttempts                    int
		}
		if err = rows.Scan(&job.id, &job.modID, &job.generation, &job.claimedGeneration, &job.attempts, &job.maxAttempts); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, job)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	const lastError = "OSS rehome worker lease expired after the maximum number of attempts"
	for _, job := range jobs {
		if job.claimedGeneration > 0 && job.generation != job.claimedGeneration {
			if _, err = tx.Exec(ctx, `update oss_rehome_jobs set status='queued',attempts=0,next_attempt_at=now(),
				locked_by='',lease_expires_at=null,claimed_generation=null,last_error='',failure_class='',updated_at=now()
				where id=$1`, job.id); err != nil {
				return 0, err
			}
			continue
		}
		if _, err = tx.Exec(ctx, `update oss_rehome_jobs set status='dead',dead_at=now(),locked_by='',
			lease_expires_at=null,claimed_generation=null,last_error=$2,failure_class='worker_lost',updated_at=now()
			where id=$1`, job.id, lastError); err != nil {
			return 0, err
		}
		alertJob := ossRehomeJob{ID: job.id, ModID: job.modID, Generation: job.generation, Attempts: job.attempts, MaxAttempts: job.maxAttempts}
		if err = insertOSSRehomeDeadAlertTx(ctx, tx, alertJob, "worker_lost", lastError); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(jobs), nil
}

func insertOSSRehomeDeadAlertTx(ctx context.Context, tx pgx.Tx, job ossRehomeJob, failureClass, lastError string) error {
	_, err := tx.Exec(ctx, `insert into app_logs(category,level,action,target,payload)
		values('infrastructure','error','oss_rehome_dead',$1::text,jsonb_build_object(
			'modId',$2::bigint,'generation',$3::bigint,'attempts',$4::integer,'maxAttempts',$5::integer,
			'failureClass',$6::text,'lastError',$7::text))`,
		fmt.Sprintf("%d", job.ID), job.ModID, job.Generation, job.Attempts, job.MaxAttempts, failureClass, lastError)
	return err
}
