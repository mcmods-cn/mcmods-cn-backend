package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestLevelRecalculationScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify bounded level recalculation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var operatorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values('level_operator','level-operator@example.invalid','test-only',true,'active') returning id`).Scan(&operatorID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into roles(code,name,weight) values
		('level_low','Level low',10),('level_high','Level high',20);
		insert into permission_role_tracks(code,name) values('level_scale','Level scale');
		insert into permission_role_track_roles(track_code,role_id,position)
		select 'level_scale',id,case code when 'level_low' then 0 else 1 end
		from roles where code in ('level_low','level_high');
		insert into users(username,email,password_hash,email_verified,status)
		select 'level_scale_'||value,'level-scale-'||value||'@example.invalid','test-only',true,'active'
		from generate_series(1,100000) value;
		insert into user_experience(user_id,experience,level)
		select id,(row_number() over(order by id)-1)%1500,0 from users where username like 'level_scale_%';
		insert into user_role_bindings(user_id,role_id,source,source_key)
		select account.id,role.id,'level_track','legacy-track' from users account cross join roles role
		where account.username like 'level_scale_%' and role.code='level_low'
		order by account.id limit 1000`); err != nil {
		t.Fatal(err)
	}
	var manualUserID, lowRoleID int64
	if err = pool.QueryRow(ctx, `select account.id,role.id from users account cross join roles role
		where account.username='level_scale_1' and role.code='level_low'`).Scan(&manualUserID, &lowRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_role_bindings(user_id,role_id,source,source_key)
		values($1,$2,'manual','')`, manualUserID, lowRoleID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	firstQueries, firstElapsed := invokeLevelConfigUpdateIntegration(t, ctx, server, counter, operatorID, `[50,500]`)
	if firstQueries > 6 {
		t.Fatalf("configuration request used %d database statements for 100000 users, want <=6", firstQueries)
	}
	worker := NewLevelRecalculationWorker(pool)
	firstJob, claimed, err := worker.claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim first level recalculation: claimed=%t err=%v", claimed, err)
	}
	firstBatchQueries := counter.queries.Load()
	firstBatch, err := worker.processBatch(ctx, firstJob)
	if err != nil {
		t.Fatal(err)
	}
	firstBatchQueries = counter.queries.Load() - firstBatchQueries
	if firstBatchQueries != 1 || firstBatch.processed != levelRecalculationBatchSize || firstBatch.done {
		t.Fatalf("first batch statements=%d processed=%d done=%t", firstBatchQueries, firstBatch.processed, firstBatch.done)
	}

	secondQueries, secondElapsed := invokeLevelConfigUpdateIntegration(t, ctx, server, counter, operatorID, `[100,1000]`)
	if secondQueries > 6 {
		t.Fatalf("replacement configuration request used %d database statements, want <=6", secondQueries)
	}
	if _, err = worker.processBatch(ctx, firstJob); !errorsIsNoRows(err) {
		t.Fatalf("superseded worker retained its lease: %v", err)
	}
	var firstStatus, firstLock string
	if err = pool.QueryRow(ctx, `select status,locked_by from level_recalculation_jobs where id=$1`, firstJob.id).
		Scan(&firstStatus, &firstLock); err != nil || firstStatus != "superseded" || firstLock != "" {
		t.Fatalf("first job status=%q lock=%q err=%v", firstStatus, firstLock, err)
	}

	job, claimed, err := worker.claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim replacement level recalculation: claimed=%t err=%v", claimed, err)
	}
	batchQueriesBefore := counter.queries.Load()
	batchCount := 0
	var final levelRecalculationBatchResult
	for {
		final, err = worker.processBatch(ctx, job)
		if err != nil {
			t.Fatal(err)
		}
		batchCount++
		if final.processed > levelRecalculationBatchSize {
			t.Fatalf("batch processed %d users, limit %d", final.processed, levelRecalculationBatchSize)
		}
		if final.done {
			break
		}
	}
	batchQueries := counter.queries.Load() - batchQueriesBefore
	if batchCount != 200 || batchQueries != int64(batchCount) || final.processedTotal != 100000 {
		t.Fatalf("batches=%d statements=%d processed=%d", batchCount, batchQueries, final.processedTotal)
	}

	var wrongLevels, wrongRoles, derivedRoles, expectedDerived, manualRoles int64
	if err = pool.QueryRow(ctx, `select count(*) from user_experience where level<>case
		when experience>=1000 then 2 when experience>=100 then 1 else 0 end`).Scan(&wrongLevels); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from user_role_bindings binding
		join user_experience experience on experience.user_id=binding.user_id
		join permission_role_track_roles track_role on track_role.track_code='level_scale'
			and track_role.role_id=binding.role_id and track_role.position=experience.level-1
		where binding.source='level_track' and binding.source_key='level_scale'`).Scan(&derivedRoles); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from user_experience where experience>=100`).Scan(&expectedDerived); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from user_role_bindings binding
		where binding.source='level_track' and not exists(
			select 1 from user_experience experience join permission_role_track_roles track_role
				on track_role.track_code='level_scale' and track_role.position=experience.level-1
			where experience.user_id=binding.user_id and track_role.role_id=binding.role_id
				and binding.source_key='level_scale')`).Scan(&wrongRoles); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from user_role_bindings
		where user_id=$1 and role_id=$2 and source='manual' and source_key=''`, manualUserID, lowRoleID).Scan(&manualRoles); err != nil {
		t.Fatal(err)
	}
	if wrongLevels != 0 || wrongRoles != 0 || derivedRoles != expectedDerived || manualRoles != 1 {
		t.Fatalf("wrongLevels=%d wrongRoles=%d derived=%d expected=%d manual=%d",
			wrongLevels, wrongRoles, derivedRoles, expectedDerived, manualRoles)
	}

	rows, err := pool.Query(ctx, `explain(analyze,buffers,format text)
		select user_id,experience from user_experience where user_id>$1 order by user_id limit 501`, manualUserID+50000)
	if err != nil {
		t.Fatal(err)
	}
	var planLines []string
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			break
		}
		planLines = append(planLines, line)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.ToLower(strings.Join(planLines, "\n"))
	if !strings.Contains(plan, "user_experience_pkey") || strings.Contains(plan, "seq scan on user_experience") {
		t.Fatalf("deep batch did not use the user experience primary-key index:\n%s", plan)
	}
	if _, err = pool.Exec(ctx, `insert into level_recalculation_jobs(
		config_version,status,attempts,max_attempts,locked_by,lease_expires_at)
		values(1000,'processing',8,8,'exhausted-worker',now()-interval '1 second')`); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err = worker.claim(ctx); err != nil || claimed {
		t.Fatalf("exhausted lease claim: claimed=%t err=%v", claimed, err)
	}
	if _, err = pool.Exec(ctx, `insert into level_recalculation_jobs(
		config_version,status,attempts,max_attempts,locked_by,lease_expires_at)
		values(999,'processing',1,8,'lost-worker',now()-interval '1 second')`); err != nil {
		t.Fatal(err)
	}
	recovered, claimed, err := worker.claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("recover expired lease: claimed=%t err=%v", claimed, err)
	}
	failure := "transient " + strings.Repeat("等", 700)
	if err = worker.fail(ctx, recovered, errors.New(failure)); err != nil {
		t.Fatal(err)
	}
	var recoveredStatus, recoveredLock, recoveredError, exhaustedStatus string
	var recoveredAttempts int
	if err = pool.QueryRow(ctx, `select status,attempts,locked_by,last_error from level_recalculation_jobs where id=$1`, recovered.id).
		Scan(&recoveredStatus, &recoveredAttempts, &recoveredLock, &recoveredError); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select status from level_recalculation_jobs where config_version=1000`).Scan(&exhaustedStatus); err != nil {
		t.Fatal(err)
	}
	if recoveredStatus != "queued" || recoveredAttempts != 2 || recoveredLock != "" ||
		!strings.HasPrefix(failure, recoveredError) || len(recoveredError) < 2046 || len(recoveredError) > 2048 || exhaustedStatus != "dead" {
		t.Fatalf("recovery status=%q attempts=%d lock=%q error=%q exhausted=%q",
			recoveredStatus, recoveredAttempts, recoveredLock, recoveredError, exhaustedStatus)
	}
	t.Logf("100000 users: first request=%s/%d statements replacement=%s/%d statements batches=%d/%d statements",
		firstElapsed, firstQueries, secondElapsed, secondQueries, batchCount, batchQueries)
}

func invokeLevelConfigUpdateIntegration(
	t *testing.T,
	ctx context.Context,
	server *Server,
	counter *integrationQueryCounter,
	operatorID int64,
	thresholds string,
) (int64, time.Duration) {
	t.Helper()
	body := fmt.Sprintf(`{"roleTrackCode":"level_scale","levelThresholds":%s}`, thresholds)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/levels/config", strings.NewReader(body))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: operatorID}))
	response := httptest.NewRecorder()
	before := counter.queries.Load()
	started := time.Now()
	server.updateLevelConfig(response, request)
	elapsed := time.Since(started)
	queries := counter.queries.Load() - before
	if response.Code != http.StatusOK {
		t.Fatalf("update level configuration status=%d body=%s", response.Code, response.Body.String())
	}
	return queries, elapsed
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func TestLevelRecalculationSettlesOnlyAfterTakingTheLiveJobLock(t *testing.T) {
	f := newTEST013Fixture(t)
	worker := NewLevelRecalculationWorker(f.db)
	var job claimedLevelRecalculationJob
	job.lockToken = "test-level-lease"
	var deadline time.Time
	if err := f.db.QueryRow(f.ctx, `insert into level_recalculation_jobs(config_version,status,attempts,locked_by,lease_expires_at)
		values(1042,'processing',1,$1,clock_timestamp()+interval '3 seconds') returning id,lease_expires_at`, job.lockToken).Scan(&job.id, &deadline); err != nil {
		t.Fatal(err)
	}
	holder, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(f.ctx)
	if _, err = holder.Exec(f.ctx, "select id from level_recalculation_jobs where id=$1 for no key update", job.id); err != nil {
		t.Fatal(err)
	}
	batch, failure := make(chan error, 1), make(chan error, 1)
	go func() { _, err := worker.processBatch(f.ctx, job); batch <- err }()
	go func() { failure <- worker.fail(f.ctx, job, errors.New("stale worker")) }()
	auditCompletionAwaitLocks(t, f, 2)
	var enteredBeforeExpiry bool
	if err = holder.QueryRow(f.ctx, "select clock_timestamp()<$1::timestamptz", deadline).Scan(&enteredBeforeExpiry); err != nil || !enteredBeforeExpiry {
		t.Fatalf("operations did not reach the held row before expiry: %v", err)
	}
	if _, err = holder.Exec(f.ctx, "select pg_sleep(greatest(0,extract(epoch from $1::timestamptz-clock_timestamp())))", deadline); err != nil {
		t.Fatal(err)
	}
	if err = holder.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-batch; !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expired batch: %v", err)
	}
	if err = <-failure; err != nil {
		t.Fatalf("expired failure attempt: %v", err)
	}
	var status, lastError string
	var count int
	if err = f.db.QueryRow(f.ctx, "select status,last_error,processed_count from level_recalculation_jobs where id=$1", job.id).Scan(&status, &lastError, &count); err != nil || status != "processing" || lastError != "" || count != 0 {
		t.Fatalf("expired owner changed job: %q %q %d %v", status, lastError, count, err)
	}
	fresh, claimed, err := worker.claim(f.ctx)
	if err != nil || !claimed || fresh.id != job.id || fresh.lockToken == job.lockToken {
		t.Fatalf("recovery: %#v %v %v", fresh, claimed, err)
	}
	if _, err = worker.processBatch(f.ctx, fresh); err != nil {
		t.Fatalf("new lease: %v", err)
	}
}
