package progression

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestTEST031ProgressionRewardsRolesVersionsAndFailureReplayIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute the TEST031 progression state-machine proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newTEST031IsolatedDatabase(t, ctx)
	suffix := time.Now().UnixNano()

	var concurrentUserID, replayUserID int64
	for label, target := range map[string]*int64{"concurrent": &concurrentUserID, "replay": &replayUserID} {
		if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
			values($1,$2,'test031-password',true) returning id`,
			fmt.Sprintf("test031-%s-%d", label, suffix), fmt.Sprintf("test031-%s-%d@example.invalid", label, suffix)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	roleIDs := make([]int64, 3)
	for index := range roleIDs {
		if err := pool.QueryRow(ctx, `insert into roles(code,name,status) values($1,$2,'active') returning id`,
			fmt.Sprintf("test031_level_%d_%d", index+1, suffix), fmt.Sprintf("TEST031 level %d", index+1)).Scan(&roleIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	trackCode := fmt.Sprintf("test031_track_%d", suffix)
	setupStatements := []struct {
		query string
		args  []any
	}{
		{`insert into permission_role_tracks(code,name) values($1,'TEST031 track')`, []any{trackCode}},
		{`insert into permission_role_track_roles(track_code,role_id,position)
			values($1,$2,0),($1,$3,1),($1,$4,2)`, []any{trackCode, roleIDs[0], roleIDs[1], roleIDs[2]}},
		{`update level_system_config set role_track_code=$1,level_thresholds=array[10,20,50]::bigint[],version=version+1`, []any{trackCode}},
		{`update task_definitions set status='disabled'`, nil},
		{`insert into user_role_bindings(user_id,role_id,source,source_key)
			values($1,$3,'manual',''),($2,$3,'manual','')`, []any{concurrentUserID, replayUserID, roleIDs[1]}},
	}
	for _, statement := range setupStatements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	concurrentVersion := test031PermissionVersion(t, ctx, pool, concurrentUserID)
	replayVersion := test031PermissionVersion(t, ctx, pool, replayUserID)

	var concurrentTaskID int64
	if err := pool.QueryRow(ctx, `insert into task_definitions(code,name,condition,rewards,status)
		values($1,'TEST031 concurrent',
		'{"action":"create","objectType":"user","metric":"count","target":2}'::jsonb,
		'{"experience":25,"currencies":{"diamond":3}}'::jsonb,'active') returning id`,
		fmt.Sprintf("test031-concurrent-%d", suffix)).Scan(&concurrentTaskID); err != nil {
		t.Fatal(err)
	}
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	concurrentCacheKey := querycache.UserPermissionVersionKey(concurrentUserID)
	cache.Set(ctx, concurrentCacheKey, []byte("stale"), time.Minute)
	if _, exists := cache.Get(ctx, concurrentCacheKey); !exists {
		t.Fatal("could not prime TEST031 permission-version cache")
	}
	service := NewService(pool, cache)
	eventTime := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	event := activity.Event{
		UserID: concurrentUserID, ActionID: activity.ActionCreate, ObjectTypeID: activity.ObjectUser, OccurredAt: eventTime,
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- service.ProcessActivityBatch(ctx, []activity.Event{event})
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent progression batch: %v", err)
		}
	}
	assertTEST031TaskFacts(t, ctx, pool, concurrentUserID, concurrentTaskID, 2, 1, 25, 2, 3)
	assertTEST031RewardTransactions(t, ctx, pool, concurrentUserID, concurrentTaskID, 1, 1)
	assertTEST031RoleSources(t, ctx, pool, concurrentUserID, roleIDs[1], roleIDs[1])
	if version := test031PermissionVersion(t, ctx, pool, concurrentUserID); version != concurrentVersion+1 {
		t.Fatalf("concurrent permission version=%d want %d", version, concurrentVersion+1)
	}
	if _, exists := cache.Get(ctx, concurrentCacheKey); exists {
		t.Fatal("successful progression commit retained a stale permission-version cache entry")
	}

	cache.Set(ctx, concurrentCacheKey, []byte("stale-again"), time.Minute)
	if _, exists := cache.Get(ctx, concurrentCacheKey); !exists {
		t.Fatal("could not re-prime TEST031 permission-version cache")
	}
	if err := service.ProcessActivityBatch(ctx, []activity.Event{event, event}); err != nil {
		t.Fatalf("duplicate completed task replay: %v", err)
	}
	assertTEST031TaskFacts(t, ctx, pool, concurrentUserID, concurrentTaskID, 4, 1, 25, 2, 3)
	assertTEST031RewardTransactions(t, ctx, pool, concurrentUserID, concurrentTaskID, 1, 1)
	if version := test031PermissionVersion(t, ctx, pool, concurrentUserID); version != concurrentVersion+1 {
		t.Fatalf("completed replay changed permission version to %d", version)
	}
	if _, exists := cache.Get(ctx, concurrentCacheKey); exists {
		t.Fatal("completed replay did not invalidate its activity user's version cache")
	}

	if _, err := pool.Exec(ctx, `update task_definitions set status='disabled' where id=$1`, concurrentTaskID); err != nil {
		t.Fatal(err)
	}
	var replayTaskID int64
	if err := pool.QueryRow(ctx, `insert into task_definitions(code,name,condition,rewards,status)
		values($1,'TEST031 replay',
		'{"action":"create","objectType":"user","metric":"count","target":1}'::jsonb,
		'{"experience":100,"currencies":{"diamond":7}}'::jsonb,'active') returning id`,
		fmt.Sprintf("test031-replay-%d", suffix)).Scan(&replayTaskID); err != nil {
		t.Fatal(err)
	}
	replayCacheKey := querycache.UserPermissionVersionKey(replayUserID)
	cache.Set(ctx, replayCacheKey, []byte("must-survive-rollback"), time.Minute)
	if _, exists := cache.Get(ctx, replayCacheKey); !exists {
		t.Fatal("could not prime TEST031 replay cache")
	}
	if _, err := pool.Exec(ctx, `create or replace function test031_reject_currency_projection() returns trigger as $$
		begin raise exception 'TEST031 currency projection rejected'; end;
		$$ language plpgsql;
		create trigger test031_reject_currency_projection before insert on currency_transactions
		for each row execute function test031_reject_currency_projection()`); err != nil {
		t.Fatal(err)
	}
	replayEvent := event
	replayEvent.UserID = replayUserID
	if err := service.ProcessActivityBatch(ctx, []activity.Event{replayEvent}); err == nil ||
		!regexp.MustCompile(`TEST031 currency projection rejected`).MatchString(err.Error()) {
		t.Fatalf("projection failure error=%v", err)
	}
	assertTEST031TaskFacts(t, ctx, pool, replayUserID, replayTaskID, 0, 0, 0, 0, 0)
	assertTEST031RewardTransactions(t, ctx, pool, replayUserID, replayTaskID, 0, 0)
	assertTEST031RoleSources(t, ctx, pool, replayUserID, roleIDs[1], 0)
	if version := test031PermissionVersion(t, ctx, pool, replayUserID); version != replayVersion {
		t.Fatalf("rolled-back projection changed permission version to %d want %d", version, replayVersion)
	}
	if cached, exists := cache.Get(ctx, replayCacheKey); !exists || string(cached) != "must-survive-rollback" {
		t.Fatalf("failed transaction invalidated cache: value=%q exists=%v", cached, exists)
	}
	if _, err := pool.Exec(ctx, `drop trigger test031_reject_currency_projection on currency_transactions;
		drop function test031_reject_currency_projection()`); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessActivityBatch(ctx, []activity.Event{replayEvent}); err != nil {
		t.Fatalf("progression replay after projection repair: %v", err)
	}
	assertTEST031TaskFacts(t, ctx, pool, replayUserID, replayTaskID, 1, 1, 100, 3, 7)
	assertTEST031RewardTransactions(t, ctx, pool, replayUserID, replayTaskID, 1, 1)
	assertTEST031RoleSources(t, ctx, pool, replayUserID, roleIDs[1], roleIDs[2])
	if version := test031PermissionVersion(t, ctx, pool, replayUserID); version != replayVersion+1 {
		t.Fatalf("replayed permission version=%d want %d", version, replayVersion+1)
	}
	if _, exists := cache.Get(ctx, replayCacheKey); exists {
		t.Fatal("successful replay retained stale permission-version cache")
	}
}

func assertTEST031TaskFacts(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, taskID,
	wantProgress, wantRewarded, wantExperience int64, wantLevel int, wantCurrency int64,
) {
	t.Helper()
	var progress, rewarded, experience, currency int64
	var level int
	if err := pool.QueryRow(ctx, `select
		coalesce((select progress from user_task_progress where user_id=$1 and task_id=$2 and period_key='all'),0),
		coalesce((select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null),0),
		coalesce((select experience from user_experience where user_id=$1),0),
		coalesce((select level from user_experience where user_id=$1),0),
		coalesce((select balance from user_currency_balances balance join currencies currency on currency.id=balance.currency_id
			where balance.user_id=$1 and currency.code='diamond'),0)`, userID, taskID).
		Scan(&progress, &rewarded, &experience, &level, &currency); err != nil {
		t.Fatal(err)
	}
	if progress != wantProgress || rewarded != wantRewarded || experience != wantExperience ||
		level != wantLevel || currency != wantCurrency {
		t.Fatalf("task facts progress/rewarded/experience/level/currency=(%d,%d,%d,%d,%d) want (%d,%d,%d,%d,%d)",
			progress, rewarded, experience, level, currency,
			wantProgress, wantRewarded, wantExperience, wantLevel, wantCurrency)
	}
}

func assertTEST031RewardTransactions(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, taskID, wantExperience, wantCurrency int64,
) {
	t.Helper()
	referenceKey := fmt.Sprintf("%d:all", taskID)
	var experienceCount, currencyCount int64
	if err := pool.QueryRow(ctx, `select
		(select count(*) from experience_transactions where user_id=$1 and reference_type='task' and reference_key=$2),
		(select count(*) from currency_transactions where user_id=$1 and reference_type='task' and reference_key=$2)`,
		userID, referenceKey).Scan(&experienceCount, &currencyCount); err != nil {
		t.Fatal(err)
	}
	if experienceCount != wantExperience || currencyCount != wantCurrency {
		t.Fatalf("reward transaction counts=(%d,%d) want (%d,%d)",
			experienceCount, currencyCount, wantExperience, wantCurrency)
	}
}

func assertTEST031RoleSources(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, manualRoleID, levelRoleID int64,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `select role_id,source,source_key from user_role_bindings
		where user_id=$1 order by source,role_id`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	bindings := make(map[string]int64)
	for rows.Next() {
		var roleID int64
		var source, sourceKey string
		if err = rows.Scan(&roleID, &source, &sourceKey); err != nil {
			t.Fatal(err)
		}
		bindings[source+":"+sourceKey] = roleID
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if bindings["manual:"] != manualRoleID {
		t.Fatalf("manual binding=%v want role %d", bindings, manualRoleID)
	}
	if levelRoleID == 0 {
		if _, exists := bindings["level_track:"]; exists || len(bindings) != 1 {
			t.Fatalf("unexpected rolled-back level binding: %v", bindings)
		}
		return
	}
	if bindings["level_track:"+test031TrackKey(t, ctx, pool)] != levelRoleID || len(bindings) != 2 {
		t.Fatalf("role bindings=%v want manual %d and level %d", bindings, manualRoleID, levelRoleID)
	}
}

func test031TrackKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var trackCode string
	if err := pool.QueryRow(ctx, `select role_track_code from level_system_config where singleton`).Scan(&trackCode); err != nil {
		t.Fatal(err)
	}
	return trackCode
}

func test031PermissionVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64) int64 {
	t.Helper()
	var version int64
	if err := pool.QueryRow(ctx, `select permission_version from users where id=$1`, userID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func newTEST031IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test031_progression_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test031_progression_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST031 database name %q", databaseName)
	}
	quotedName := pgx.Identifier{databaseName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create database "+quotedName); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if !namePattern.MatchString(databaseName) {
			t.Errorf("refuse unsafe TEST031 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST031 database: %v", dropErr)
		}
		adminPool.Close()
	})
	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.Database = databaseName
	scopedConfig.MaxConns, scopedConfig.MinConns = 8, 1
	pool, err = pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
