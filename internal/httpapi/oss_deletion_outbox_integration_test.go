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
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOSSDeletionFailureBudgetLeaseAndDeadAlertAreAtomicIntegration(t *testing.T) {
	pool := openOSSDeletionStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_object_deletion_outbox(
		id,bucket,endpoint,region,object_key,status,attempts,max_attempts,locked_at,locked_by) values
		(1,'bucket','oss.example','region','retry','processing',1,3,now(),'worker:retry'),
		(2,'bucket','oss.example','region','budget','processing',3,3,now(),'worker:budget'),
		(3,'bucket','oss.example','region','auth','processing',1,12,now(),'worker:auth'),
		(4,'bucket','oss.example','region','fault','processing',12,12,now(),'worker:fault'),
		(5,'bucket','oss.example','region','stale','processing',2,2,now()-interval '10 minutes','dead-worker')`); err != nil {
		t.Fatal(err)
	}
	worker := &OSSDeletionWorker{server: &Server{db: pool}, workerID: "test-worker"}
	dead, err := worker.recordFailure(ctx, ossDeletionJob{ID: 1, Attempts: 1, MaxAttempts: 3, LockToken: "worker:retry"}, errors.New("temporary transport failure"))
	if err != nil || dead {
		t.Fatalf("retryable failure = dead %t/error %v", dead, err)
	}
	assertOSSDeletionState(t, ctx, pool, 1, "pending", 1, "unknown", 0)

	dead, err = worker.recordFailure(ctx, ossDeletionJob{ID: 2, Attempts: 3, MaxAttempts: 3, LockToken: "worker:budget"}, errors.New("still unavailable"))
	if err != nil || !dead {
		t.Fatalf("budget exhaustion = dead %t/error %v", dead, err)
	}
	assertOSSDeletionState(t, ctx, pool, 2, "dead", 3, "unknown", 1)

	dead, err = worker.recordFailure(ctx, ossDeletionJob{ID: 3, Attempts: 1, MaxAttempts: 12, LockToken: "worker:auth"},
		&aliyunoss.ServiceError{StatusCode: 403, Code: "InvalidAccessKeyId"})
	if err != nil || !dead {
		t.Fatalf("permanent authentication failure = dead %t/error %v", dead, err)
	}
	assertOSSDeletionState(t, ctx, pool, 3, "dead", 1, "authentication", 1)

	if _, err = pool.Exec(ctx, `alter table app_logs add constraint reject_fault_dead_alert
		check(target<>'4') not valid`); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.recordFailure(ctx, ossDeletionJob{ID: 4, Attempts: 12, MaxAttempts: 12, LockToken: "worker:fault"}, errors.New("fault")); err == nil {
		t.Fatal("dead alert persistence failure was ignored")
	}
	assertOSSDeletionState(t, ctx, pool, 4, "processing", 12, "", 0)

	recovered, err := worker.deadLetterExhaustedLeases(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("exhausted lease recovery = %d/%v, want 1/nil", recovered, err)
	}
	assertOSSDeletionState(t, ctx, pool, 5, "dead", 2, "worker_lost", 1)
	if recovered, err = worker.deadLetterExhaustedLeases(ctx); err != nil || recovered != 0 {
		t.Fatalf("second exhausted recovery = %d/%v", recovered, err)
	}
}

func TestOSSDeletionClaimUsesOwnerTokenAndCompletionCASIntegration(t *testing.T) {
	pool := openOSSDeletionStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_object_deletion_outbox(
		id,bucket,endpoint,region,object_key,status,max_attempts) values
		(10,'bucket','oss.example','region','claimed','pending',2),
		(11,'bucket','oss.example','region','completed','pending',2)`); err != nil {
		t.Fatal(err)
	}
	first := &OSSDeletionWorker{server: &Server{db: pool}, workerID: "first"}
	second := &OSSDeletionWorker{server: &Server{db: pool}, workerID: "second"}
	job, err := first.claim(ctx)
	if err != nil || job.ID != 10 || job.Attempts != 1 || job.LockToken == "" {
		t.Fatalf("first claim = %#v/%v", job, err)
	}
	if _, err = second.claim(ctx); err != nil {
		// The second pending row is intentionally claimable; finish it below.
		t.Fatal(err)
	}
	if err = first.complete(ctx, ossDeletionJob{ID: 10, LockToken: "wrong"}); !errors.Is(err, errOSSDeletionLeaseLost) {
		t.Fatalf("wrong completion token = %v", err)
	}
	if _, err = pool.Exec(ctx, `update oss_object_deletion_outbox set locked_at=now()-interval '10 minutes' where id=10`); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := second.claim(ctx)
	if err != nil || reclaimed.ID != 10 || reclaimed.Attempts != 2 || reclaimed.LockToken == job.LockToken {
		t.Fatalf("reclaim = %#v/%v", reclaimed, err)
	}
	if err = first.complete(ctx, job); !errors.Is(err, errOSSDeletionLeaseLost) {
		t.Fatalf("stale owner completion = %v", err)
	}
	dead, err := second.recordFailure(ctx, reclaimed, errors.New("last transient failure"))
	if err != nil || !dead {
		t.Fatalf("reclaimed final failure = %t/%v", dead, err)
	}
	assertOSSDeletionState(t, ctx, pool, 10, "dead", 2, "unknown", 1)
}

func TestOSSDeletionReplayAndCompletedKeyReuseAreAuditedIntegration(t *testing.T) {
	pool := openOSSDeletionStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_object_deletion_outbox(
		id,oss_file_id,bucket,endpoint,region,object_key,reason,status,attempts,max_attempts,last_error,failure_class,deleted_at,dead_at) values
		(20,20,'bucket','oss.example','region','reused','old','completed',4,12,'old error','unknown',now(),null),
		(21,21,'bucket','oss.example','region','repeat','old','completed',1,12,'','','now',null),
		(22,22,'bucket','oss.example','region','dead','manual','dead',12,12,'permission denied','authorization',null,now());
		insert into oss_files(id,bucket,endpoint,region,object_key,status) values
		(20,'bucket','oss.example','region','reused','deleted'),(21,'bucket','oss.example','region','repeat','deleted'),
		(22,'bucket','oss.example','region','dead','deleted'),(23,'bucket','oss.example','region-new','reused','active')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.tombstoneOSSFileTx(ctx, tx, 23, "new-incarnation"); err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	assertOSSDeletionState(t, ctx, pool, 20, "pending", 0, "", 0)
	var currentFileID int64
	var currentRegion string
	if err = pool.QueryRow(ctx, `select oss_file_id,region from oss_object_deletion_outbox where id=20`).Scan(&currentFileID, &currentRegion); err != nil {
		t.Fatal(err)
	}
	if currentFileID != 23 || currentRegion != "region-new" {
		t.Fatalf("reused key lineage = file %d/region %q, want 23/region-new", currentFileID, currentRegion)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.tombstoneOSSFileTx(ctx, tx, 21, "repeat-delete"); err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	assertOSSDeletionState(t, ctx, pool, 21, "completed", 1, "", 0)
	if _, err = pool.Exec(ctx, `update oss_files set status='active' where id=21`); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.tombstoneOSSFileTx(ctx, tx, 21, "new-delete-cycle"); err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	assertOSSDeletionState(t, ctx, pool, 21, "pending", 0, "", 0)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/infrastructure/oss-deletions?status=dead", nil)
	listResponse := httptest.NewRecorder()
	server.adminOSSDeletionJobs(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"failureClass":"authorization"`) {
		t.Fatalf("dead deletion list = %d/%s", listResponse.Code, listResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/infrastructure/oss-deletions/22/replay", nil)
	request.SetPathValue("id", "22")
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 7001}))
	response := httptest.NewRecorder()
	server.adminReplayOSSDeletion(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("replay response = %d/%s", response.Code, response.Body.String())
	}
	assertOSSDeletionState(t, ctx, pool, 22, "pending", 0, "", 0)
	var replayCount int
	var actor *int64
	if err = pool.QueryRow(ctx, `select replay_count,last_replayed_by from oss_object_deletion_outbox where id=22`).Scan(&replayCount, &actor); err != nil || replayCount != 1 || actor == nil || *actor != 7001 {
		t.Fatalf("replay facts = count %d/actor %v/error %v", replayCount, actor, err)
	}
	var payload []byte
	if err = pool.QueryRow(ctx, `select payload from app_logs where action='oss_deletion_replayed' and target='22'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var audit map[string]any
	if err = json.Unmarshal(payload, &audit); err != nil || int64(audit["actorUserId"].(float64)) != 7001 || audit["failureClass"] != "authorization" {
		t.Fatalf("replay audit = %#v/error %v", audit, err)
	}
	secondResponse := httptest.NewRecorder()
	server.adminReplayOSSDeletion(secondResponse, request)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("active replay response = %d, want conflict", secondResponse.Code)
	}
	if _, err = pool.Exec(ctx, `update oss_object_deletion_outbox set status='dead',attempts=12,
		failure_class='authorization',last_error='still denied',dead_at=now() where id=22`); err != nil {
		t.Fatal(err)
	}
	thirdRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/infrastructure/oss-deletions/22/replay", nil)
	thirdRequest.SetPathValue("id", "22")
	thirdRequest = thirdRequest.WithContext(context.WithValue(thirdRequest.Context(), claimsContextKey, security.Claims{Subject: 7002}))
	thirdResponse := httptest.NewRecorder()
	server.adminReplayOSSDeletion(thirdResponse, thirdRequest)
	if thirdResponse.Code != http.StatusAccepted {
		t.Fatalf("second dead cycle replay = %d/%s", thirdResponse.Code, thirdResponse.Body.String())
	}
	if err = pool.QueryRow(ctx, `select replay_count,last_replayed_by from oss_object_deletion_outbox where id=22`).Scan(&replayCount, &actor); err != nil || replayCount != 2 || actor == nil || *actor != 7002 {
		t.Fatalf("second replay facts = count %d/actor %v/error %v", replayCount, actor, err)
	}
}

func TestOSSDeletionRecoveryAndAdminPlansUsePartialIndexesIntegration(t *testing.T) {
	pool := openOSSDeletionStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_object_deletion_outbox(
		id,bucket,endpoint,region,object_key,status,attempts,max_attempts,deleted_at)
		select 1000+value,'bucket','oss.example','region','completed-'||value,'completed',1,12,now()
		from generate_series(1,100000) value;
		insert into oss_object_deletion_outbox(
		id,bucket,endpoint,region,object_key,status,attempts,max_attempts,locked_at,dead_at) values
		(200001,'bucket','oss.example','region','exhausted','processing',12,12,now()-interval '10 minutes',null),
		(200002,'bucket','oss.example','region','dead-plan','dead',12,12,null,now());
		analyze oss_object_deletion_outbox`); err != nil {
		t.Fatal(err)
	}
	exhaustedPlan := explainOSSDeletionPlan(t, ctx, pool, `select id,attempts,max_attempts from oss_object_deletion_outbox
		where attempts>=max_attempts and (status='pending' or
			(status='processing' and locked_at<now()-make_interval(secs => 300)))
		order by updated_at,id for update skip locked limit 32`)
	if !strings.Contains(strings.ToLower(exhaustedPlan), "test_oss_deletion_exhausted") {
		t.Fatalf("exhausted recovery plan missed partial index:\n%s", exhaustedPlan)
	}
	deadPlan := explainOSSDeletionPlan(t, ctx, pool, `select id from oss_object_deletion_outbox
		where status='dead' order by dead_at desc nulls last,id desc limit 50`)
	if !strings.Contains(strings.ToLower(deadPlan), "test_oss_deletion_dead") {
		t.Fatalf("dead admin plan missed partial index:\n%s", deadPlan)
	}
}

func openOSSDeletionStateTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify OSS deletion lifecycle")
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
	if _, err = pool.Exec(ctx, `create temp table oss_object_deletion_outbox(
		id bigserial primary key,oss_file_id bigint,bucket text not null,endpoint text not null,region text not null,use_cname boolean not null default false,
		object_key text not null,reason text not null default '',status text not null default 'pending',attempts integer not null default 0,max_attempts integer not null default 12,
		next_attempt_at timestamptz not null default now(),locked_at timestamptz,locked_by text not null default '',last_error text not null default '',failure_class text not null default '',
		created_at timestamptz not null default now(),updated_at timestamptz not null default now(),deleted_at timestamptz,dead_at timestamptz,replay_count integer not null default 0,
		last_replayed_at timestamptz,last_replayed_by bigint,unique(bucket,endpoint,object_key));
		create index test_oss_deletion_pending on oss_object_deletion_outbox(next_attempt_at,id) where status in ('pending','processing');
		create index test_oss_deletion_dead on oss_object_deletion_outbox(dead_at desc,id desc) where status='dead';
		create index test_oss_deletion_exhausted on oss_object_deletion_outbox(updated_at,id)
			where status in ('pending','processing') and attempts>=max_attempts;
		create temp table oss_files(id bigint primary key,bucket text not null,endpoint text not null,region text not null,object_key text not null,status text not null,updated_at timestamptz not null default now());
		create temp table log_shares(source_file_id bigint,status text not null default 'active',deleted_at timestamptz);
		create temp table app_logs(id bigserial primary key,category text not null,level text not null,action text not null,target text not null,payload jsonb not null default '{}'::jsonb)`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return pool
}

func explainOSSDeletionPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) string {
	t.Helper()
	rows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+query)
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
	return plan.String()
}

func assertOSSDeletionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, wantStatus string, wantAttempts int, wantClass string, wantAlerts int) {
	t.Helper()
	var status, failureClass string
	var attempts, alerts int
	if err := pool.QueryRow(ctx, `select status,attempts,failure_class,
		(select count(*) from app_logs where action='oss_deletion_dead' and target=($1::bigint)::text)
		from oss_object_deletion_outbox where id=$1::bigint`, id).Scan(&status, &attempts, &failureClass, &alerts); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || attempts != wantAttempts || failureClass != wantClass || alerts != wantAlerts {
		t.Fatalf("job %d = %s/attempts %d/class %q/alerts %d, want %s/%d/%q/%d",
			id, status, attempts, failureClass, alerts, wantStatus, wantAttempts, wantClass, wantAlerts)
	}
}
