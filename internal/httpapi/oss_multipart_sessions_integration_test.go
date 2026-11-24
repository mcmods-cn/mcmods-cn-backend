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
	"mcmods-cn-backend/internal/database"
)

func TestOSSMultipartSessionBindingAndIdempotentSettlementIntegration(t *testing.T) {
	pool := openOSSRecoveryStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	server := &Server{db: pool}
	cfg := ossConfigPayload{Bucket: "bucket", Endpoint: "https://oss.example", Region: "region"}
	direct := ossDirectUploadRequest{OriginalName: "large.zip", ContentType: "application/zip", SizeBytes: 20 << 20,
		SHA256: strings.Repeat("a", 64), Category: "imports", Source: "catalog"}
	if err := server.registerOSSMultipartSession(ctx, 101, cfg, direct, "imports/large.zip", "upload-12345678", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	request := ossCompleteUploadRequest{ObjectKey: "imports/large.zip", OriginalName: direct.OriginalName, ContentType: direct.ContentType,
		SizeBytes: direct.SizeBytes, SHA256: direct.SHA256, Category: direct.Category, Source: direct.Source, MultipartUploadID: "upload-12345678"}
	if _, err := server.beginOSSMultipartSettlement(ctx, 202, cfg, request, "complete"); !errors.Is(err, errOSSMultipartSessionForbidden) {
		t.Fatalf("cross-user completion = %v", err)
	}
	tampered := request
	tampered.SHA256 = strings.Repeat("b", 64)
	if _, err := server.beginOSSMultipartSettlement(ctx, 101, cfg, tampered, "complete"); !errors.Is(err, errOSSMultipartSessionForbidden) {
		t.Fatalf("tampered completion = %v", err)
	}
	tampered = request
	tampered.Category = "other"
	if _, err := server.beginOSSMultipartSettlement(ctx, 101, cfg, tampered, "complete"); !errors.Is(err, errOSSMultipartSessionForbidden) {
		t.Fatalf("category-tampered completion = %v", err)
	}
	settlement, err := server.beginOSSMultipartSettlement(ctx, 101, cfg, request, "complete")
	if err != nil || settlement.Token == "" || settlement.Already {
		t.Fatalf("begin completion = %#v/%v", settlement, err)
	}
	if _, err = server.beginOSSMultipartSettlement(ctx, 101, cfg, request, "abort"); !errors.Is(err, errOSSMultipartSessionConflict) {
		t.Fatalf("concurrent abort = %v", err)
	}
	if err = server.finishOSSMultipartSettlement(ctx, settlement, "complete", nil); err != nil {
		t.Fatal(err)
	}
	repeated, err := server.beginOSSMultipartSettlement(ctx, 101, cfg, request, "complete")
	if err != nil || !repeated.Already {
		t.Fatalf("idempotent completion = %#v/%v", repeated, err)
	}
	assertOSSMultipartState(t, ctx, pool, settlement.ID, "completed", 0, "", 0)

	if _, err = pool.Exec(ctx, `insert into oss_multipart_sessions(id,owner_id,bucket,endpoint,region,object_key,upload_id,
		original_name,content_type,size_bytes,sha256,category,source,status,expires_at)
		values(2,101,'bucket','https://oss.example','region','imports/abort.zip','upload-abort1234',
		'abort.zip','application/zip',$1,$2,'imports','catalog','active',now()+interval '1 hour')`, 20<<20, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	abortRequest := request
	abortRequest.ObjectKey, abortRequest.OriginalName, abortRequest.SHA256, abortRequest.MultipartUploadID = "imports/abort.zip", "abort.zip", strings.Repeat("c", 64), "upload-abort1234"
	abortSettlement, err := server.beginOSSMultipartSettlement(ctx, 101, cfg, abortRequest, "abort")
	if err != nil {
		t.Fatal(err)
	}
	providerErr := errors.New("temporary abort failure")
	if err = server.finishOSSMultipartSettlement(ctx, abortSettlement, "abort", providerErr); err != nil {
		t.Fatal(err)
	}
	assertOSSMultipartState(t, ctx, pool, 2, "cleanup_pending", 0, "unknown", 0)
	abortSettlement, err = server.beginOSSMultipartSettlement(ctx, 101, cfg, abortRequest, "abort")
	if err != nil {
		t.Fatal(err)
	}
	if err = server.finishOSSMultipartSettlement(ctx, abortSettlement, "abort", nil); err != nil {
		t.Fatal(err)
	}
	assertOSSMultipartState(t, ctx, pool, 2, "aborted", 0, "", 0)
}

func TestOSSMultipartCleanupWorkerRetriesDeadLettersAndRecoversLeasesIntegration(t *testing.T) {
	pool := openOSSRecoveryStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sha := strings.Repeat("d", 64)
	if _, err := pool.Exec(ctx, `insert into oss_multipart_sessions(id,owner_id,bucket,endpoint,region,object_key,upload_id,
		original_name,content_type,size_bytes,sha256,category,source,status,attempts,max_attempts,next_attempt_at,lease_expires_at,expires_at,locked_by) values
		(10,101,'b','e','r','expired','upload-expired1','a','x',1,$1,'c','s','active',0,3,now(),null,now()-interval '1 hour',''),
		(11,101,'b','e','r','fresh','upload-fresh001','a','x',1,$1,'c','s','active',0,3,now(),null,now()+interval '1 hour',''),
		(12,101,'b','e','r','retry','upload-retry001','a','x',1,$1,'c','s','cleanup_pending',2,3,now(),null,now()-interval '1 hour',''),
		(13,101,'b','e','r','crashed','upload-crash001','a','x',1,$1,'c','s','aborting',3,3,now(),now()-interval '10 minutes',now()-interval '1 hour','old')`, sha); err != nil {
		t.Fatal(err)
	}
	worker := &OSSMultipartCleanupWorker{server: &Server{db: pool}, workerID: "test"}
	aborted := map[int64]int{}
	worker.abortFn = func(_ context.Context, job ossMultipartCleanupJob) error {
		aborted[job.ID]++
		if job.ID == 12 {
			return &aliyunoss.ServiceError{StatusCode: 403, Code: "AccessDenied"}
		}
		return nil
	}
	worker.drain(ctx)
	assertOSSMultipartState(t, ctx, pool, 10, "aborted", 1, "", 0)
	assertOSSMultipartState(t, ctx, pool, 11, "active", 0, "", 0)
	assertOSSMultipartState(t, ctx, pool, 12, "dead", 3, "authorization", 1)
	assertOSSMultipartState(t, ctx, pool, 13, "dead", 3, "worker_lost", 1)
	if aborted[10] != 1 || aborted[12] != 1 || aborted[11] != 0 || aborted[13] != 0 {
		t.Fatalf("abort calls = %#v", aborted)
	}
}

func TestOSSMultipartDeadAlertFailureRollsBackStateIntegration(t *testing.T) {
	pool := openOSSRecoveryStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_multipart_sessions(id,owner_id,bucket,endpoint,region,object_key,upload_id,
		original_name,content_type,size_bytes,sha256,category,source,status,attempts,max_attempts,expires_at,locked_by,lease_expires_at)
		values(14,101,'b','e','r','rollback','upload-rollback1','a','x',1,$1,'c','s','aborting',3,3,
		now()-interval '1 hour','lease-14',now()+interval '1 minute')`, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `alter table app_logs add constraint reject_multipart_14 check(target <> '14')`); err != nil {
		t.Fatal(err)
	}
	worker := &OSSMultipartCleanupWorker{server: &Server{db: pool}}
	job := ossMultipartCleanupJob{ID: 14, Attempts: 3, MaxAttempts: 3, LockToken: "lease-14"}
	if _, err := worker.recordFailure(ctx, job, errors.New("provider failure")); err == nil {
		t.Fatal("dead alert insert unexpectedly succeeded")
	}
	assertOSSMultipartState(t, ctx, pool, 14, "aborting", 3, "", 0)
}

func TestOSSMultipartDeadReplayIsAuditedIntegration(t *testing.T) {
	pool := openOSSRecoveryStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_multipart_sessions(id,owner_id,bucket,endpoint,region,object_key,upload_id,
		original_name,content_type,size_bytes,sha256,category,source,status,attempts,max_attempts,last_error,failure_class,expires_at,dead_at)
		values(30,101,'b','e','r','dead','upload-dead0001','a','x',1,$1,'c','s','dead',12,12,'denied','authorization',now()-interval '1 hour',now())`, strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Config{JWTSecret: "synthetic-oss-replay"}}
	list := httptest.NewRecorder()
	server.adminOSSMultipartSessions(list, httptest.NewRequest(http.MethodGet, "/api/v1/admin/infrastructure/oss-multipart-sessions?status=dead", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"failureClass":"authorization"`) {
		t.Fatalf("dead list = %d/%s", list.Code, list.Body.String())
	}
	var publicID string
	if err := pool.QueryRow(ctx, `select public_id from oss_multipart_sessions where id=$1`, 30).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	replay := authorizedOSSReplay(t, ctx, server, "oss-multipart-sessions", publicID, server.adminReplayOSSMultipartSession)
	response := replay()
	if response.Code != http.StatusAccepted {
		t.Fatalf("replay = %d/%s", response.Code, response.Body.String())
	}
	assertOSSMultipartState(t, ctx, pool, 30, "cleanup_pending", 0, "", 0)
	var payload []byte
	if err := pool.QueryRow(ctx, `select payload from app_logs where action='oss_multipart_cleanup_replayed'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var audit map[string]any
	if err := json.Unmarshal(payload, &audit); err != nil || int64(audit["actorUserId"].(float64)) != 7001 || audit["failureClass"] != "authorization" {
		t.Fatalf("audit = %#v/%v", audit, err)
	}
	conflict := replay()
	if conflict.Code != http.StatusConflict {
		t.Fatalf("second replay = %d", conflict.Code)
	}
}

func TestOSSMultipartCleanupPlansUsePartialIndexesIntegration(t *testing.T) {
	pool := openOSSRecoveryStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into oss_multipart_sessions(owner_id,bucket,endpoint,region,object_key,upload_id,
		original_name,content_type,size_bytes,sha256,category,source,status,expires_at,completed_at)
		select 101,'b','e','r','done-'||value,'upload-'||value,'a','x',1,$1,'c','s','completed',now(),now()
		from generate_series(1,100000) value`, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into oss_multipart_sessions(id,owner_id,bucket,endpoint,region,object_key,upload_id,original_name,content_type,size_bytes,sha256,category,source,status,attempts,max_attempts,next_attempt_at,lease_expires_at,expires_at,updated_at,dead_at) values
		(200001,101,'b','e','r','expired-plan','upload-expired-plan','a','x',1,$1,'c','s','active',0,12,now(),null,now()-interval '1 hour',now(),null),
		(200002,101,'b','e','r','cleanup-plan','upload-cleanup-plan','a','x',1,$1,'c','s','cleanup_pending',1,12,now()-interval '1 minute',null,now()-interval '1 hour',now(),null),
		(200003,101,'b','e','r','lease-plan','upload-lease-plan','a','x',1,$1,'c','s','aborting',1,12,now(),now()-interval '1 minute',now()-interval '1 hour',now(),null),
		(200004,101,'b','e','r','exhausted-plan','upload-exhausted-plan','a','x',1,$1,'c','s','cleanup_pending',12,12,now(),null,now()-interval '1 hour',now()-interval '1 minute',null),
		(200005,101,'b','e','r','dead-plan','upload-dead-plan','a','x',1,$1,'c','s','dead',12,12,now(),null,now()-interval '1 hour',now(),now())`, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `analyze oss_multipart_sessions; set enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	type planCase struct {
		query string
		args  []any
	}
	queries := map[string]planCase{
		"idx_oss_multipart_sessions_expired":   {query: `select id from oss_multipart_sessions where status='active' and attempts<max_attempts and expires_at<=now() order by expires_at,id limit 1`},
		"idx_oss_multipart_sessions_cleanup":   {query: `select id from oss_multipart_sessions where status='cleanup_pending' and attempts<max_attempts and next_attempt_at<=now() order by next_attempt_at,id limit 1`},
		"idx_oss_multipart_sessions_lease":     {query: `select id from oss_multipart_sessions where status in ('completing','aborting') and attempts<max_attempts and lease_expires_at<now() order by lease_expires_at,id limit 1`},
		"idx_oss_multipart_sessions_exhausted": {query: `select id from oss_multipart_sessions where status in ('active','completing','cleanup_pending','aborting') and attempts>=max_attempts order by updated_at,id limit 1`},
		"idx_oss_multipart_sessions_dead": {query: `select id from oss_multipart_sessions where status=$1
			order by case when status='dead' then dead_at else updated_at end desc nulls last,id desc limit $2`, args: []any{"dead", 50}},
	}
	for indexName, testCase := range queries {
		rows, queryErr := pool.Query(ctx, "explain (analyze,buffers) "+testCase.query, testCase.args...)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		var lines []string
		for rows.Next() {
			var line string
			if queryErr = rows.Scan(&line); queryErr != nil {
				rows.Close()
				t.Fatal(queryErr)
			}
			lines = append(lines, line)
		}
		rows.Close()
		plan := strings.ToLower(strings.Join(lines, "\n"))
		if !strings.Contains(plan, indexName) {
			t.Fatalf("plan misses %s:\n%s", indexName, plan)
		}
	}
}

// Run the real route prerequisite with a live session. A temporarily unavailable
// permission store after admission must leave the dead job available for retry.
func authorizedOSSReplay(t *testing.T, ctx context.Context, server *Server, route, publicID string, handler http.HandlerFunc) func() *httptest.ResponseRecorder {
	t.Helper()
	grantTEST044Permissions(t, ctx, server.db, 7001, "admin.config.write")
	token := mintTEST048Token(t, ctx, server.db, server.cfg, 7001)
	path := "/api/v1/admin/infrastructure/" + route + "/" + publicID + "/replay"
	request := func(next http.HandlerFunc) *httptest.ResponseRecorder {
		return performTEST044Request(ctx, server.requirePermission("admin.config.write", next), token, http.MethodPost, path, "", map[string]string{"id": publicID})
	}
	result := request(func(w http.ResponseWriter, r *http.Request) {
		if _, err := server.db.Exec(ctx, `alter table auth_sessions rename column revoked_at to unavailable_replay_timestamp`); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := server.db.Exec(ctx, `alter table auth_sessions rename column unavailable_replay_timestamp to revoked_at`); err != nil {
				t.Fatal(err)
			}
		}()
		handler(w, r)
	})
	if result.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable permission store = %d/%s", result.Code, result.Body.String())
	}
	return func() *httptest.ResponseRecorder { return request(handler) }
}

func openOSSRecoveryStateTestDB(t *testing.T) *pgxpool.Pool {
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
	fixture, err := database.NewIsolatedTestSchema(ctx, databaseURL, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := fixture.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	pool := fixture.Pool
	if _, err = pool.Exec(ctx, `insert into users(id,username,email,password_hash) values(101,'multipart-state-owner','multipart-owner@example.invalid','synthetic'),(202,'multipart-state-other','multipart-other@example.invalid','synthetic'),(7001,'multipart-state-reviewer','multipart-reviewer@example.invalid','synthetic')`); err != nil {
		t.Fatal(err)
	}

	return pool
}

func assertOSSMultipartState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, status string, attempts int, failureClass string, alerts int) {
	t.Helper()
	var gotStatus, gotClass string
	var gotAttempts, gotAlerts int
	if err := pool.QueryRow(ctx, `select status,attempts,failure_class from oss_multipart_sessions where id=$1`, id).Scan(&gotStatus, &gotAttempts, &gotClass); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from app_logs where action='oss_multipart_cleanup_dead' and target=$1::integer::text`, id).Scan(&gotAlerts); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotAttempts != attempts || gotClass != failureClass || gotAlerts != alerts {
		t.Fatalf("session %d = %s/%d/%q/alerts%d, want %s/%d/%q/alerts%d", id, gotStatus, gotAttempts, gotClass, gotAlerts, status, attempts, failureClass, alerts)
	}
}
