package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOSSRehomeEnqueueGenerationAndLeaseSurviveRestartIntegration(t *testing.T) {
	pool := openOSSRehomeStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, commit := range []bool{false, true} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = enqueueOSSRehomeJobTx(ctx, tx, 1); err != nil {
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit(ctx)
		} else {
			err = tx.Rollback(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from oss_rehome_jobs where mod_id=1`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed job count = %d/%v", count, err)
	}

	first := &OSSRehomeWorker{server: &Server{db: pool}, workerID: "first"}
	second := &OSSRehomeWorker{server: &Server{db: pool}, workerID: "second"}
	job, err := first.claim(ctx)
	if err != nil || job.ModID != 1 || job.Generation != 1 || job.Attempts != 1 {
		t.Fatalf("first claim = %#v/%v", job, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = enqueueOSSRehomeJobTx(ctx, tx, 1); err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	state, err := first.complete(ctx, job)
	if err != nil || state != "queued" {
		t.Fatalf("completion after a newer generation = %q/%v", state, err)
	}
	assertOSSRehomeState(t, ctx, pool, job.ID, "queued", 2, 0, "", 0)

	newJob, err := second.claim(ctx)
	if err != nil || newJob.Generation != 2 || newJob.Attempts != 1 {
		t.Fatalf("new generation claim = %#v/%v", newJob, err)
	}
	if _, err = first.complete(ctx, job); !errors.Is(err, errOSSRehomeLeaseLost) {
		t.Fatalf("old owner completion = %v", err)
	}
	if state, err = second.complete(ctx, newJob); err != nil || state != "completed" {
		t.Fatalf("current completion = %q/%v", state, err)
	}

	if _, err = pool.Exec(ctx, `insert into oss_rehome_jobs(id,mod_id,status,generation,claimed_generation,attempts,max_attempts,locked_by,lease_expires_at)
		values(10,2,'processing',1,1,1,3,'crashed',now()-interval '10 minutes')`); err != nil {
		t.Fatal(err)
	}
	restarted, err := second.claim(ctx)
	if err != nil || restarted.ID != 10 || restarted.Attempts != 2 || restarted.LockToken == "crashed" {
		t.Fatalf("restart reclaim = %#v/%v", restarted, err)
	}
}

func TestOSSRehomeDrainPersistsProcessorFailureAndLaterCompletesIntegration(t *testing.T) {
	pool := openOSSRehomeStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_rehome_jobs(id,mod_id,status,max_attempts) values(15,1,'queued',3)`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	worker := &OSSRehomeWorker{server: &Server{db: pool}, workerID: "drain"}
	worker.processFn = func(_ context.Context, modID int64) error {
		calls++
		if modID != 1 {
			t.Fatalf("processed mod %d, want 1", modID)
		}
		if calls == 1 {
			return errors.New("copy temporarily failed")
		}
		return nil
	}
	worker.drain(ctx)
	var status string
	if err := pool.QueryRow(ctx, `select status from oss_rehome_jobs where id=15`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "queued" {
		if _, err := pool.Exec(ctx, `update oss_rehome_jobs set next_attempt_at=now() where id=15`); err != nil {
			t.Fatal(err)
		}
		worker.drain(ctx)
	}
	assertOSSRehomeState(t, ctx, pool, 15, "completed", 1, 2, "", 0)
	if calls != 2 {
		t.Fatalf("processor calls = %d, want 2", calls)
	}
}

func TestOSSRehomeFailureBudgetDeadAlertAndGenerationRaceAreAtomicIntegration(t *testing.T) {
	pool := openOSSRehomeStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_rehome_jobs(
		id,mod_id,status,generation,claimed_generation,attempts,max_attempts,locked_by,lease_expires_at) values
		(20,1,'processing',1,1,1,3,'retry',now()+interval '1 minute'),
		(21,2,'processing',1,1,3,3,'budget',now()+interval '1 minute'),
		(22,3,'processing',1,1,1,8,'auth',now()+interval '1 minute'),
		(23,4,'processing',2,1,8,8,'newer',now()-interval '10 minutes'),
		(24,5,'processing',1,1,8,8,'crashed',now()-interval '10 minutes')`); err != nil {
		t.Fatal(err)
	}
	worker := &OSSRehomeWorker{server: &Server{db: pool}, workerID: "worker"}
	state, err := worker.recordFailure(ctx, ossRehomeJob{ID: 20, ModID: 1, Generation: 1, Attempts: 1, MaxAttempts: 3, LockToken: "retry"}, errors.New("temporary"))
	if err != nil || state != "queued" {
		t.Fatalf("retry failure = %q/%v", state, err)
	}
	assertOSSRehomeState(t, ctx, pool, 20, "queued", 1, 1, "unknown", 0)

	state, err = worker.recordFailure(ctx, ossRehomeJob{ID: 21, ModID: 2, Generation: 1, Attempts: 3, MaxAttempts: 3, LockToken: "budget"}, errors.New("still unavailable"))
	if err != nil || state != "dead" {
		t.Fatalf("budget failure = %q/%v", state, err)
	}
	assertOSSRehomeState(t, ctx, pool, 21, "dead", 1, 3, "unknown", 1)

	state, err = worker.recordFailure(ctx, ossRehomeJob{ID: 22, ModID: 3, Generation: 1, Attempts: 1, MaxAttempts: 8, LockToken: "auth"},
		&aliyunoss.ServiceError{StatusCode: 403, Code: "InvalidAccessKeyId"})
	if err != nil || state != "dead" {
		t.Fatalf("permanent failure = %q/%v", state, err)
	}
	assertOSSRehomeState(t, ctx, pool, 22, "dead", 1, 1, "authentication", 1)

	recovered, err := worker.deadLetterExpiredBudget(ctx)
	if err != nil || recovered != 2 {
		t.Fatalf("expired budget recovery = %d/%v", recovered, err)
	}
	assertOSSRehomeState(t, ctx, pool, 23, "queued", 2, 0, "", 0)
	assertOSSRehomeState(t, ctx, pool, 24, "dead", 1, 8, "worker_lost", 1)
	if recovered, err = worker.deadLetterExpiredBudget(ctx); err != nil || recovered != 0 {
		t.Fatalf("second expired recovery = %d/%v", recovered, err)
	}

	if _, err = pool.Exec(ctx, `insert into oss_rehome_jobs(id,mod_id,status,generation,claimed_generation,attempts,max_attempts,locked_by,lease_expires_at)
		values(25,6,'processing',1,1,8,8,'alert-fault',now()+interval '1 minute');
		alter table app_logs add constraint reject_rehome_alert check(target<>'25') not valid`); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.recordFailure(ctx, ossRehomeJob{ID: 25, ModID: 6, Generation: 1, Attempts: 8, MaxAttempts: 8, LockToken: "alert-fault"}, errors.New("fault")); err == nil {
		t.Fatal("dead alert persistence failure was ignored")
	}
	assertOSSRehomeState(t, ctx, pool, 25, "processing", 1, 8, "", 0)
}

func TestOSSRehomeAdminReplayIsAuditedIntegration(t *testing.T) {
	pool := openOSSRehomeStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_rehome_jobs(id,mod_id,status,generation,attempts,max_attempts,last_error,failure_class,dead_at)
		values(30,1,'dead',4,8,8,'permission denied','authorization',now())`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/infrastructure/oss-rehomes?status=dead", nil)
	listResponse := httptest.NewRecorder()
	server.adminOSSRehomeJobs(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"failureClass":"authorization"`) {
		t.Fatalf("dead rehome list = %d/%s", listResponse.Code, listResponse.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/infrastructure/oss-rehomes/30/replay", nil)
	request.SetPathValue("id", "30")
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 7001}))
	response := httptest.NewRecorder()
	server.adminReplayOSSRehome(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("replay response = %d/%s", response.Code, response.Body.String())
	}
	assertOSSRehomeState(t, ctx, pool, 30, "queued", 5, 0, "", 0)
	var replayCount int
	var actor *int64
	if err := pool.QueryRow(ctx, `select replay_count,last_replayed_by from oss_rehome_jobs where id=30`).Scan(&replayCount, &actor); err != nil || replayCount != 1 || actor == nil || *actor != 7001 {
		t.Fatalf("replay facts = %d/%v/%v", replayCount, actor, err)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `select payload from app_logs where action='oss_rehome_replayed'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var audit map[string]any
	if err := json.Unmarshal(payload, &audit); err != nil || int64(audit["actorUserId"].(float64)) != 7001 || int64(audit["generation"].(float64)) != 4 {
		t.Fatalf("replay audit = %#v/%v", audit, err)
	}
	conflict := httptest.NewRecorder()
	server.adminReplayOSSRehome(conflict, request)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("active replay = %d, want 409", conflict.Code)
	}
}

func TestOSSRehomePlansUseBoundedIndexesIntegration(t *testing.T) {
	pool := openOSSRehomeStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into mods(id) select 1000+value from generate_series(1,100000) value;
		insert into oss_rehome_jobs(mod_id,status,completed_at)
		select 1000+value,'completed',now() from generate_series(1,100000) value;
		insert into oss_rehome_jobs(id,mod_id,status,next_attempt_at) values(200001,1,'queued',now()-interval '1 minute');
		insert into oss_rehome_jobs(id,mod_id,status,generation,claimed_generation,attempts,max_attempts,lease_expires_at)
		values(200002,2,'processing',1,1,8,8,now()-interval '10 minutes');
		insert into oss_rehome_jobs(id,mod_id,status,dead_at) values(200003,3,'dead',now());
		analyze oss_rehome_jobs; set enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	for name, query := range map[string]string{
		"ready": `select id from oss_rehome_jobs where status='queued' and attempts<max_attempts
			and next_attempt_at<=now() order by next_attempt_at,id limit 1`,
		"recovery": `select id from oss_rehome_jobs where status='processing' and attempts<max_attempts
			and lease_expires_at<now() order by lease_expires_at,id limit 2`,
		"exhausted": `select id from oss_rehome_jobs where attempts>=max_attempts and
			(status='queued' or (status='processing' and lease_expires_at<now())) order by updated_at,id limit 2`,
		"dead": `select id from oss_rehome_jobs where status='dead' order by dead_at desc nulls last,id desc limit 50`,
	} {
		plan := explainOSSRehomePlan(t, ctx, pool, query)
		if !strings.Contains(strings.ToLower(plan), "idx_oss_rehome_jobs_"+name) {
			t.Fatalf("%s plan does not use its partial index:\n%s", name, plan)
		}
	}
	claimPlan := explainOSSRehomePlan(t, ctx, pool, `select id from oss_rehome_jobs where attempts<max_attempts and
		((status='queued' and next_attempt_at<=now()) or (status='processing' and lease_expires_at<now()))
		order by case when status='processing' then lease_expires_at else next_attempt_at end,id limit 1`)
	for _, index := range []string{"idx_oss_rehome_jobs_ready", "idx_oss_rehome_jobs_recovery"} {
		if !strings.Contains(strings.ToLower(claimPlan), index) {
			t.Fatalf("claim plan does not use %s:\n%s", index, claimPlan)
		}
	}
}

func openOSSRehomeStateTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to run PostgreSQL integration tests")
	}
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
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
	if _, err = pool.Exec(ctx, `create temp table mods(id bigint primary key);
		insert into mods(id) select value from generate_series(1,10) value;
		create temp table oss_rehome_jobs(
			id bigserial primary key,mod_id bigint not null unique references mods(id) on delete cascade,status text not null default 'queued',
			generation bigint not null default 1,claimed_generation bigint,attempts integer not null default 0,max_attempts integer not null default 8,
			next_attempt_at timestamptz not null default now(),locked_by text not null default '',lease_expires_at timestamptz,last_error text not null default '',
			failure_class text not null default '',created_at timestamptz not null default now(),updated_at timestamptz not null default now(),completed_at timestamptz,
			dead_at timestamptz,replay_count integer not null default 0,last_replayed_at timestamptz,last_replayed_by bigint);
		create index idx_oss_rehome_jobs_ready on oss_rehome_jobs(next_attempt_at,id) where status='queued' and attempts<max_attempts;
		create index idx_oss_rehome_jobs_recovery on oss_rehome_jobs(lease_expires_at,id) where status='processing' and attempts<max_attempts;
		create index idx_oss_rehome_jobs_exhausted on oss_rehome_jobs(updated_at,id) where status in ('queued','processing') and attempts>=max_attempts;
		create index idx_oss_rehome_jobs_dead on oss_rehome_jobs(dead_at desc,id desc) where status='dead';
		create temp table app_logs(id bigserial primary key,category text not null,level text not null,action text not null,target text not null,payload jsonb not null default '{}'::jsonb)`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return pool
}

func assertOSSRehomeState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, status string, generation int64, attempts int, failureClass string, alertCount int) {
	t.Helper()
	var gotStatus, gotFailureClass string
	var gotGeneration int64
	var gotAttempts, gotAlerts int
	if err := pool.QueryRow(ctx, `select status,generation,attempts,failure_class from oss_rehome_jobs where id=$1`, id).
		Scan(&gotStatus, &gotGeneration, &gotAttempts, &gotFailureClass); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from app_logs where action='oss_rehome_dead' and target=$1::bigint::text`, id).Scan(&gotAlerts); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotGeneration != generation || gotAttempts != attempts || gotFailureClass != failureClass || gotAlerts != alertCount {
		t.Fatalf("job %d = %s/gen%d/attempt%d/class %q/alerts%d, want %s/gen%d/attempt%d/class %q/alerts%d",
			id, gotStatus, gotGeneration, gotAttempts, gotFailureClass, gotAlerts, status, generation, attempts, failureClass, alertCount)
	}
}

func explainOSSRehomePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) string {
	t.Helper()
	rows, err := pool.Query(ctx, "explain (analyze,buffers) "+query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
