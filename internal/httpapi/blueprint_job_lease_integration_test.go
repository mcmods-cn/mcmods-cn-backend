package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestBlueprintJobLeaseRecoveryIsBoundedAndAtomicIntegration(t *testing.T) {
	pool := openBlueprintJobStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into blueprints(id,public_id,owner_id,status,title) values
		(1,'blueprint1',7,'processing','Recover'),
		(2,'blueprint2',7,'processing','Active'),
		(3,'blueprint3',7,'processing','Exhausted');
		insert into blueprint_jobs(id,public_id,blueprint_id,operation,status,attempts,max_attempts,locked_by,lease_expires_at) values
		(10,'stalejob1',1,'normalize','processing',1,3,'stopped-worker',now()-interval '1 minute'),
		(11,'activejob',2,'normalize','processing',1,3,'live-worker',now()+interval '1 hour'),
		(12,'deadjob12',3,'normalize','processing',3,3,'stopped-worker',now()-interval '1 minute');
		insert into oss_files(id,object_key,status) values
		(100,'blueprints/stale-normalized.json','pending'),
		(101,'blueprints/live-normalized.json','pending'),
		(102,'blueprints/exhausted-normalized.json','pending');
		insert into blueprint_job_artifacts(id,job_id,attempt,role,file_id,bucket,endpoint,region,object_key,status) values
		(1000,10,1,'normalized',100,'bucket','oss.example','region','blueprints/stale-normalized.json','pending'),
		(1001,11,1,'normalized',101,'bucket','oss.example','region','blueprints/live-normalized.json','pending'),
		(1002,12,3,'normalized',102,'bucket','oss.example','region','blueprints/exhausted-normalized.json','pending')`); err != nil {
		t.Fatal(err)
	}
	worker := &BlueprintWorker{db: pool, api: &Server{db: pool}, workerID: "recovery-worker"}
	recovered, exhausted, err := worker.recoverStaleBlueprintJobs(ctx)
	if err != nil || recovered != 1 || exhausted != 1 {
		t.Fatalf("recovery result = %d/%d/%v, want 1/1/nil", recovered, exhausted, err)
	}
	assertBlueprintJobLeaseState(t, ctx, pool, 10, "queued", "", nil, 1)
	assertBlueprintJobLeaseState(t, ctx, pool, 11, "processing", "live-worker", boolPointer(true), 1)
	assertBlueprintJobLeaseState(t, ctx, pool, 12, "failed", "", nil, 3)
	assertBlueprintArtifactState(t, ctx, pool, 1000, "abandoned", "deleted", 1)
	assertBlueprintArtifactState(t, ctx, pool, 1001, "pending", "pending", 0)
	assertBlueprintArtifactState(t, ctx, pool, 1002, "abandoned", "deleted", 1)
	var eventCount int
	if err = pool.QueryRow(ctx, `select count(*) from nats_outbox where event_type='blueprint.conversion.recovered'
		and aggregate_type='blueprint_job' and aggregate_id='stalejob1'`).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("recovery outbox count = %d, error = %v", eventCount, err)
	}
	var exhaustedBlueprintStatus string
	if err = pool.QueryRow(ctx, `select status from blueprints where id=3`).Scan(&exhaustedBlueprintStatus); err != nil || exhaustedBlueprintStatus != "failed" {
		t.Fatalf("exhausted normalize blueprint status = %q, error = %v", exhaustedBlueprintStatus, err)
	}
	if recovered, exhausted, err = worker.recoverStaleBlueprintJobs(ctx); err != nil || recovered != 0 || exhausted != 0 {
		t.Fatalf("second recovery result = %d/%d/%v, want idempotent zero", recovered, exhausted, err)
	}

	if _, err = pool.Exec(ctx, `insert into blueprints(id,public_id,owner_id,status,title) values(4,'blueprint4',7,'processing','Rollback');
		insert into blueprint_jobs(id,public_id,blueprint_id,operation,status,attempts,max_attempts,locked_by,lease_expires_at)
		values(13,'rollback1',4,'normalize','processing',1,3,'stopped-worker',now()-interval '1 minute');
		insert into oss_files(id,object_key,status) values(103,'blueprints/rollback-normalized.json','pending');
		insert into blueprint_job_artifacts(id,job_id,attempt,role,file_id,bucket,endpoint,region,object_key,status)
		values(1003,13,1,'normalized',103,'bucket','oss.example','region','blueprints/rollback-normalized.json','pending');
		alter table nats_outbox add constraint reject_blueprint_recovery check(event_type<>'blueprint.conversion.recovered') not valid`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = worker.recoverStaleBlueprintJobs(ctx); err == nil {
		t.Fatal("recovery event persistence failure was ignored")
	}
	assertBlueprintJobLeaseState(t, ctx, pool, 13, "processing", "stopped-worker", boolPointer(false), 1)
	var rollbackBlueprintStatus string
	if err = pool.QueryRow(ctx, `select status from blueprints where id=4`).Scan(&rollbackBlueprintStatus); err != nil || rollbackBlueprintStatus != "processing" {
		t.Fatalf("failed recovery changed blueprint status = %q, error = %v", rollbackBlueprintStatus, err)
	}
	assertBlueprintArtifactState(t, ctx, pool, 1003, "pending", "pending", 0)
}

func TestBlueprintArtifactActivationCompletionAndFailureCompensationAreAtomicIntegration(t *testing.T) {
	pool := openBlueprintJobStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into blueprints(id,public_id,owner_id,status,title) values
		(50,'artifact1',7,'processing','Artifact Success'),(51,'artifact2',7,'processing','Artifact Rollback');
		insert into blueprint_jobs(id,public_id,blueprint_id,operation,status,attempts,max_attempts,locked_by,lease_expires_at) values
		(70,'artifact7',50,'normalize','processing',1,3,'worker:success',now()+interval '1 minute'),
		(71,'artifact8',51,'normalize','processing',1,3,'worker:failure',now()+interval '1 minute');
		insert into oss_files(id,object_key,status) values
		(200,'blueprints/job-70-attempt-1-normalized.json','pending'),
		(201,'blueprints/job-71-attempt-1-normalized.json','pending');
		insert into blueprint_job_artifacts(id,job_id,attempt,role,file_id,bucket,endpoint,region,object_key,status) values
		(2000,70,1,'normalized',200,'bucket','oss.example','region','blueprints/job-70-attempt-1-normalized.json','pending'),
		(2001,71,1,'normalized',201,'bucket','oss.example','region','blueprints/job-71-attempt-1-normalized.json','pending')`); err != nil {
		t.Fatal(err)
	}
	successArtifact := blueprintArtifact{id: 2000, fileID: 200, jobID: 70, attempt: 1, role: blueprintArtifactNormalized}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = activateBlueprintArtifactTx(ctx, tx, successArtifact); err == nil {
		_, err = tx.Exec(ctx, `update blueprints set normalized_file_id=$2 where id=$1`, 50, 200)
	}
	if err == nil {
		err = completeBlueprintJobTx(ctx, tx, 70, "worker:success")
	}
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if err != nil {
		t.Fatalf("activate artifact and complete job: %v", err)
	}
	assertBlueprintArtifactState(t, ctx, pool, 2000, "active", "active", 0)
	assertBlueprintJobLeaseState(t, ctx, pool, 70, "completed", "", nil, 1)

	if _, err = pool.Exec(ctx, `alter table oss_files add constraint reject_artifact_201_activation
		check(id<>201 or status<>'active') not valid`); err != nil {
		t.Fatal(err)
	}
	failureArtifact := blueprintArtifact{id: 2001, fileID: 201, jobID: 71, attempt: 1, role: blueprintArtifactNormalized}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = activateBlueprintArtifactTx(ctx, tx, failureArtifact)
	if err == nil {
		err = completeBlueprintJobTx(ctx, tx, 71, "worker:failure")
	}
	if err == nil {
		t.Fatal("fault-injected artifact activation unexpectedly succeeded")
	}
	_ = tx.Rollback(ctx)
	assertBlueprintArtifactState(t, ctx, pool, 2001, "pending", "pending", 0)
	assertBlueprintJobLeaseState(t, ctx, pool, 71, "processing", "worker:failure", boolPointer(true), 1)

	worker := &BlueprintWorker{db: pool, api: &Server{db: pool}, workerID: "worker"}
	terminal, err := worker.failBlueprintJob(ctx, claimedBlueprintJob{
		id: 71, blueprintID: 51, operation: "normalize", attempts: 1, maxAttempts: 3, runToken: "worker:failure",
	}, errors.New("business transaction failed"))
	if err != nil || terminal {
		t.Fatalf("failed artifact compensation = terminal %t/error %v", terminal, err)
	}
	assertBlueprintArtifactState(t, ctx, pool, 2001, "abandoned", "deleted", 1)
	assertBlueprintJobLeaseState(t, ctx, pool, 71, "queued", "", nil, 1)
}

func TestBlueprintArtifactObjectKeyIsAttemptIdempotent(t *testing.T) {
	first := blueprintArtifactObjectKey("root", "blueprints/release", 42, 2, blueprintArtifactConversion, "SCHEM")
	second := blueprintArtifactObjectKey("root", "blueprints/release", 42, 2, blueprintArtifactConversion, ".schem")
	nextAttempt := blueprintArtifactObjectKey("root", "blueprints/release", 42, 3, blueprintArtifactConversion, "schem")
	if first != second || first == nextAttempt || !strings.Contains(first, "job-42-attempt-2-conversion.schem") {
		t.Fatalf("artifact keys are not stable per attempt: %q / %q / %q", first, second, nextAttempt)
	}
}

func TestBlueprintOrphanArtifactRecoveryIsBoundedAndAtomicIntegration(t *testing.T) {
	pool := openBlueprintJobStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into blueprints(id,public_id,owner_id,status,title) values
		(60,'orphanbp1',7,'ready','Orphan'),(61,'orphanbp2',7,'processing','Live');
		insert into blueprint_jobs(id,public_id,blueprint_id,operation,status,attempts,max_attempts,locked_by,lease_expires_at) values
		(80,'orphan080',60,'normalize','completed',1,3,'',null),
		(81,'orphan081',61,'normalize','processing',1,3,'live-worker',now()+interval '1 minute');
		insert into oss_files(id,object_key,status) values
		(210,'blueprints/deleted-job.json','pending'),(211,'blueprints/completed-job.json','pending'),
		(212,'blueprints/live-job.json','pending'),(214,'blueprints/deleted-blueprint.json','active');
		insert into blueprint_job_artifacts(id,job_id,blueprint_id,attempt,role,file_id,bucket,endpoint,region,object_key,status) values
		(2100,null,null,1,'normalized',210,'bucket','oss.example','region','blueprints/deleted-job.json','pending'),
		(2101,80,60,1,'normalized',211,'bucket','oss.example','region','blueprints/completed-job.json','pending'),
		(2102,81,61,1,'normalized',212,'bucket','oss.example','region','blueprints/live-job.json','pending'),
		(2104,null,null,1,'conversion',214,'bucket','oss.example','region','blueprints/deleted-blueprint.json','active')`); err != nil {
		t.Fatal(err)
	}
	worker := &BlueprintWorker{db: pool, api: &Server{db: pool}, workerID: "recovery-worker"}
	recovered, err := worker.recoverOrphanedBlueprintArtifacts(ctx)
	if err != nil || recovered != 3 {
		t.Fatalf("orphan artifact recovery = %d/%v, want 3/nil", recovered, err)
	}
	assertBlueprintArtifactState(t, ctx, pool, 2100, "abandoned", "deleted", 1)
	assertBlueprintArtifactState(t, ctx, pool, 2101, "abandoned", "deleted", 1)
	assertBlueprintArtifactState(t, ctx, pool, 2102, "pending", "pending", 0)
	assertBlueprintArtifactState(t, ctx, pool, 2104, "abandoned", "deleted", 1)
	if recovered, err = worker.recoverOrphanedBlueprintArtifacts(ctx); err != nil || recovered != 0 {
		t.Fatalf("second orphan recovery = %d/%v, want idempotent zero", recovered, err)
	}

	if _, err = pool.Exec(ctx, `insert into oss_files(id,object_key,status) values(213,'blueprints/orphan-rollback.json','pending');
		insert into blueprint_job_artifacts(id,job_id,blueprint_id,attempt,role,file_id,bucket,endpoint,region,object_key,status)
		values(2103,null,null,1,'normalized',213,'bucket','oss.example','region','blueprints/orphan-rollback.json','pending');
		alter table oss_object_deletion_outbox add constraint reject_orphan_compensation
		check(reason<>'blueprint-job-artifact-orphaned') not valid`); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.recoverOrphanedBlueprintArtifacts(ctx); err == nil {
		t.Fatal("orphan compensation persistence failure was ignored")
	}
	assertBlueprintArtifactState(t, ctx, pool, 2103, "pending", "pending", 0)
}

func TestBlueprintOrphanArtifactRecoveryPlanUsesBoundedIndexesIntegration(t *testing.T) {
	pool := openBlueprintJobStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into blueprint_job_artifacts(
		id,job_id,blueprint_id,attempt,role,file_id,bucket,endpoint,region,object_key,status)
		select 10000+value,null,1,1,'normalized',10000+value,'bucket','oss.example','region',
			'blueprints/historical-'||value||'.json','active'
		from generate_series(1,100000) value;
		insert into blueprint_job_artifacts(
		id,job_id,blueprint_id,attempt,role,file_id,bucket,endpoint,region,object_key,status) values
		(200001,null,null,1,'normalized',200001,'bucket','oss.example','region','blueprints/orphan-pending.json','pending'),
		(200002,null,null,1,'conversion',200002,'bucket','oss.example','region','blueprints/orphan-active.json','active');
		analyze blueprint_job_artifacts`); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select artifact.id,artifact.file_id,artifact.status,artifact.bucket,artifact.endpoint,
			artifact.region,artifact.use_cname,artifact.object_key
		from blueprint_job_artifacts artifact
		left join blueprint_jobs job on job.id=artifact.job_id
		where (artifact.status='pending'
		       and (job.id is null or job.status<>'processing' or job.attempts<>artifact.attempt))
		   or (artifact.status='active' and artifact.blueprint_id is null)
		order by artifact.id for update of artifact skip locked limit $1`, blueprintRecoveryBatchLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	definition := strings.ToLower(plan.String())
	for _, indexName := range []string{"test_blueprint_artifacts_pending", "test_blueprint_artifacts_active_orphan"} {
		if !strings.Contains(definition, indexName) {
			t.Fatalf("orphan recovery plan does not use %s:\n%s", indexName, plan.String())
		}
	}
}

func TestBlueprintJobLeaseOwnershipAndHeartbeatCASIntegration(t *testing.T) {
	pool := openBlueprintJobStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into blueprints(id,public_id,owner_id,status,title) values(10,'claimbp10',7,'queued','Claim');
		insert into blueprint_jobs(id,public_id,blueprint_id,operation,status,max_attempts)
		values(30,'claimjob3',10,'normalize','queued',3)`); err != nil {
		t.Fatal(err)
	}
	firstWorker := &BlueprintWorker{db: pool, api: &Server{db: pool}, workerID: "first-worker"}
	first, claimed, err := firstWorker.claimBlueprintJob(ctx, 30)
	if err != nil || !claimed || first.attempts != 1 || first.runToken == "" {
		t.Fatalf("first claim = %#v/%t/%v", first, claimed, err)
	}
	if err = firstWorker.renewBlueprintJobLease(ctx, 30, "wrong-token"); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("wrong-owner heartbeat = %v, want lease lost", err)
	}
	if err = firstWorker.renewBlueprintJobLease(ctx, 30, first.runToken); err != nil {
		t.Fatalf("owner heartbeat failed: %v", err)
	}
	assertBlueprintJobLeaseState(t, ctx, pool, 30, "processing", first.runToken, boolPointer(true), 1)
	secondWorker := &BlueprintWorker{db: pool, api: &Server{db: pool}, workerID: "second-worker"}
	if _, claimed, err = secondWorker.claimBlueprintJob(ctx, 30); claimed || !errors.Is(err, errBlueprintJobLeaseActive) {
		t.Fatalf("active lease duplicate claim = %t/%v", claimed, err)
	}
	if _, err = pool.Exec(ctx, `update blueprint_jobs set lease_expires_at=now()-interval '1 second' where id=30`); err != nil {
		t.Fatal(err)
	}
	second, claimed, err := secondWorker.claimBlueprintJob(ctx, 30)
	if err != nil || !claimed || second.attempts != 2 || second.runToken == first.runToken {
		t.Fatalf("expired lease reclaim = %#v/%t/%v", second, claimed, err)
	}
	if err = firstWorker.completeBlueprintJob(ctx, 30, first.runToken); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("stale owner completion = %v, want lease lost", err)
	}
	if _, err = firstWorker.failBlueprintJob(ctx, first, errors.New("late failure")); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("stale owner failure = %v, want lease lost", err)
	}
	if err = secondWorker.completeBlueprintJob(ctx, 30, second.runToken); err != nil {
		t.Fatal(err)
	}
	assertBlueprintJobLeaseState(t, ctx, pool, 30, "completed", "", nil, 2)
}

func TestBlueprintJobRetryBudgetKeepsOptionalConversionAvailableIntegration(t *testing.T) {
	pool := openBlueprintJobStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into blueprints(id,public_id,owner_id,status,title) values
		(20,'convert20',7,'ready','Ready Blueprint'),(21,'normaliz1',7,'processing','Normalize Blueprint');
		insert into blueprint_jobs(id,public_id,blueprint_id,operation,target_format,status,max_attempts) values
		(40,'convert40',20,'convert','schem','queued',2),(41,'normalj41',21,'normalize','','queued',1)`); err != nil {
		t.Fatal(err)
	}
	worker := &BlueprintWorker{db: pool, api: &Server{db: pool}, workerID: "failure-worker"}
	convertFirst, claimed, err := worker.claimBlueprintJob(ctx, 40)
	if err != nil || !claimed {
		t.Fatalf("claim first conversion: %t/%v", claimed, err)
	}
	terminal, err := worker.failBlueprintJob(ctx, convertFirst, errors.New("optional conversion failed"))
	if err != nil || terminal {
		t.Fatalf("first conversion failure = %t/%v", terminal, err)
	}
	assertBlueprintStatus(t, ctx, pool, 20, "ready")
	convertSecond, claimed, err := worker.claimBlueprintJob(ctx, 40)
	if err != nil || !claimed {
		t.Fatalf("claim second conversion: %t/%v", claimed, err)
	}
	terminal, err = worker.failBlueprintJob(ctx, convertSecond, errors.New("optional conversion failed again"))
	if err != nil || !terminal {
		t.Fatalf("terminal conversion failure = %t/%v", terminal, err)
	}
	assertBlueprintJobLeaseState(t, ctx, pool, 40, "failed", "", nil, 2)
	assertBlueprintStatus(t, ctx, pool, 20, "ready")
	if _, claimed, err = worker.claimBlueprintJob(ctx, 40); claimed || err == nil {
		t.Fatalf("exhausted conversion claim = %t/%v, want a retryable delivery error for dead-lettering", claimed, err)
	}

	normalize, claimed, err := worker.claimBlueprintJob(ctx, 41)
	if err != nil || !claimed {
		t.Fatalf("claim normalization: %t/%v", claimed, err)
	}
	terminal, err = worker.failBlueprintJob(ctx, normalize, errors.New("normalization failed"))
	if err != nil || !terminal {
		t.Fatalf("terminal normalization failure = %t/%v", terminal, err)
	}
	assertBlueprintStatus(t, ctx, pool, 21, "failed")
}

func TestBlueprintRetryAndOutboxCommitOrRollbackTogetherIntegration(t *testing.T) {
	pool := openBlueprintJobStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into blueprints(id,public_id,owner_id,status,title,last_error) values
		(30,'retrysucc',7,'failed','Retry Success','old error'),
		(31,'retryfail',7,'failed','Retry Failure','old error'),
		(32,'retrybusy',7,'failed','Retry Busy','old error');
		insert into blueprint_jobs(id,public_id,blueprint_id,operation,status) values(50,'active050',32,'normalize','queued')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	jobPublicID, err := server.retryBlueprintJob(ctx, "retrysucc", 7, 9)
	if err != nil || jobPublicID == "" {
		t.Fatalf("successful retry = %q/%v", jobPublicID, err)
	}
	assertBlueprintStatus(t, ctx, pool, 30, "queued")
	var jobs, events int
	if err = pool.QueryRow(ctx, `select count(*) from blueprint_jobs where blueprint_id=30 and status='queued'`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_type='blueprint_job' and aggregate_id=$1`, jobPublicID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || events != 1 {
		t.Fatalf("successful retry facts = jobs %d/events %d", jobs, events)
	}

	if _, err = pool.Exec(ctx, `alter table nats_outbox add constraint reject_retry_event check(event_type<>'blueprint.conversion.requested') not valid`); err != nil {
		t.Fatal(err)
	}
	if _, err = server.retryBlueprintJob(ctx, "retryfail", 7, 9); err == nil {
		t.Fatal("outbox rejection did not fail retry")
	}
	assertBlueprintStatus(t, ctx, pool, 31, "failed")
	if err = pool.QueryRow(ctx, `select count(*) from blueprint_jobs where blueprint_id=31`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("failed retry job count = %d, error = %v", jobs, err)
	}
	if _, err = server.retryBlueprintJob(ctx, "retrybusy", 7, 9); !errors.Is(err, errBlueprintNotRetryable) {
		t.Fatalf("active duplicate retry = %v, want conflict", err)
	}
	assertBlueprintStatus(t, ctx, pool, 32, "failed")
}

func openBlueprintJobStateTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify blueprint leases")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `create temp table blueprints(
		id bigint primary key,public_id text not null unique,owner_id bigint not null,status text not null,title text not null default '',
		normalized_file_id bigint,last_error text not null default '',updated_at timestamptz not null default now());
		create temp table blueprint_jobs(
		id bigserial primary key,public_id text not null unique default substring(md5(random()::text),1,9),blueprint_id bigint not null,
		operation text not null,target_format text not null default '',status text not null default 'queued',progress integer not null default 0,
		attempts integer not null default 0,max_attempts integer not null default 3,last_error text not null default '',locked_by text not null default '',
		lease_expires_at timestamptz,created_by bigint,created_at timestamptz not null default now(),started_at timestamptz,finished_at timestamptz,
		updated_at timestamptz not null default now());
		create unique index test_blueprint_jobs_active_operation on blueprint_jobs(blueprint_id,operation,target_format)
			where status in ('queued','processing');
		create temp table oss_files(
			id bigint primary key,object_key text not null unique,status text not null,bucket text not null default 'bucket',
			endpoint text not null default 'public.example',region text not null default 'region',updated_at timestamptz not null default now());
		create temp table blueprint_job_artifacts(
			id bigint primary key,job_id bigint,blueprint_id bigint,attempt integer not null,role text not null,file_id bigint not null unique,
			bucket text not null,endpoint text not null,region text not null,use_cname boolean not null default false,object_key text not null unique,
			status text not null default 'pending',created_at timestamptz not null default now(),updated_at timestamptz not null default now(),
			activated_at timestamptz,abandoned_at timestamptz,unique(job_id,attempt,role));
		create index test_blueprint_artifacts_pending on blueprint_job_artifacts(id,job_id,attempt) where status='pending';
		create index test_blueprint_artifacts_active_orphan on blueprint_job_artifacts(id) where status='active' and blueprint_id is null;
		create temp table oss_object_deletion_outbox(
			id bigserial primary key,oss_file_id bigint,bucket text not null,endpoint text not null,region text not null,use_cname boolean not null default false,
			object_key text not null,reason text not null default '',status text not null default 'pending',attempts integer not null default 0,max_attempts integer not null default 12,
			next_attempt_at timestamptz not null default now(),locked_at timestamptz,locked_by text not null default '',last_error text not null default '',failure_class text not null default '',
			created_at timestamptz not null default now(),updated_at timestamptz not null default now(),deleted_at timestamptz,dead_at timestamptz,
			replay_count integer not null default 0,last_replayed_at timestamptz,last_replayed_by bigint,unique(bucket,endpoint,object_key));
		create temp table nats_outbox(
		id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
		subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',payload jsonb not null,
		occurred_at timestamptz not null default now(),status text not null default 'pending',available_at timestamptz not null default now(),
		published_at timestamptz,locked_at timestamptz,locked_by text not null default '',attempts bigint not null default 0,
		max_attempts bigint not null default 12,last_error text not null default '',updated_at timestamptz not null default now())`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return pool
}

func assertBlueprintJobLeaseState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID int64, wantStatus, wantOwner string, leaseInFuture *bool, wantAttempts int) {
	t.Helper()
	var status, owner string
	var lease *time.Time
	var attempts int
	if err := pool.QueryRow(ctx, `select status,locked_by,lease_expires_at,attempts from blueprint_jobs where id=$1`, jobID).
		Scan(&status, &owner, &lease, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || owner != wantOwner || attempts != wantAttempts {
		t.Fatalf("job %d state = %s/%q/%d, want %s/%q/%d", jobID, status, owner, attempts, wantStatus, wantOwner, wantAttempts)
	}
	if leaseInFuture == nil {
		if lease != nil {
			t.Fatalf("job %d lease = %v, want nil", jobID, lease)
		}
	} else if lease == nil || lease.After(time.Now()) != *leaseInFuture {
		t.Fatalf("job %d lease = %v, future want %t", jobID, lease, *leaseInFuture)
	}
}

func assertBlueprintStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, blueprintID int64, want string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `select status from blueprints where id=$1`, blueprintID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("blueprint %d status = %q, want %q", blueprintID, status, want)
	}
}

func assertBlueprintArtifactState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, artifactID int64, wantArtifact, wantFile string, wantDeletionJobs int) {
	t.Helper()
	var artifactStatus, fileStatus string
	var deletionJobs int
	if err := pool.QueryRow(ctx, `select artifact.status,file.status,
		(select count(*) from oss_object_deletion_outbox deletion where deletion.oss_file_id=artifact.file_id)
		from blueprint_job_artifacts artifact join oss_files file on file.id=artifact.file_id where artifact.id=$1`, artifactID).
		Scan(&artifactStatus, &fileStatus, &deletionJobs); err != nil {
		t.Fatal(err)
	}
	if artifactStatus != wantArtifact || fileStatus != wantFile || deletionJobs != wantDeletionJobs {
		t.Fatalf("artifact %d state = %s/%s/deletions=%d, want %s/%s/%d",
			artifactID, artifactStatus, fileStatus, deletionJobs, wantArtifact, wantFile, wantDeletionJobs)
	}
}

func boolPointer(value bool) *bool { return &value }
