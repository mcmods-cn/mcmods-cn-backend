package httpapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestOPS010PopularityQueuesTerminateRecoverAndClaimOnceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("OPS-010 exercises isolated multi-connection PostgreSQL popularity queues")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newOPS010IsolatedDatabase(t, ctx)

	stamp := time.Now().UnixNano()
	var actorID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'ops010',true) returning id`, fmt.Sprintf("ops010-%d", stamp),
		fmt.Sprintf("ops010-%d@example.invalid", stamp)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		select 't029'||lpad(ordinal::text,5,'0'),'test029-popularity-'||ordinal,'TEST-029 project '||ordinal,'approved',$1
		from generate_series(1,31) ordinal`, actorID); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `select mod.id,route.id from mods mod
		join public_routes route on route.entity_type='mod' and route.internal_id=mod.id
		where mod.project_code like 't029%' order by mod.project_code`)
	if err != nil {
		t.Fatal(err)
	}
	projectIDs := make([]int64, 0, 31)
	routeIDs := make([]int64, 0, 31)
	for rows.Next() {
		var projectID, routeID int64
		if err = rows.Scan(&projectID, &routeID); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		projectIDs = append(projectIDs, projectID)
		routeIDs = append(routeIDs, routeID)
	}
	if err = finishRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(routeIDs) != 31 {
		t.Fatalf("created %d project routes, want 31", len(routeIDs))
	}

	if _, err = pool.Exec(ctx, `truncate content_stats_refresh_queue`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_stats_refresh_queue(object_route_id,refresh_metrics,refresh_popularity)
		select unnest($1::bigint[]),false,false`, routeIDs[:30]); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	claims := make(chan []contentStatsRefreshTask, 2)
	claimErrors := make(chan error, 2)
	var claimers sync.WaitGroup
	for range 2 {
		claimers.Add(1)
		go func() {
			defer claimers.Done()
			<-start
			claimed, claimErr := claimContentStatsTasks(ctx, pool, 30)
			claims <- claimed
			claimErrors <- claimErr
		}()
	}
	close(start)
	claimers.Wait()
	close(claims)
	close(claimErrors)
	for claimErr := range claimErrors {
		if claimErr != nil {
			t.Fatal(claimErr)
		}
	}
	claimedRoutes := map[int64]bool{}
	for batch := range claims {
		for _, task := range batch {
			if claimedRoutes[task.routeID] {
				t.Fatalf("route %d was claimed by both workers", task.routeID)
			}
			claimedRoutes[task.routeID] = true
		}
	}
	if len(claimedRoutes) != 30 {
		t.Fatalf("two workers claimed %d unique routes, want 30", len(claimedRoutes))
	}
	if _, err = pool.Exec(ctx, `delete from content_stats_refresh_queue where object_route_id<>all($1::bigint[])`, []int64{routeIDs[0], routeIDs[1]}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update content_stats_refresh_queue set status='processing',attempts=$1,locked_at=now()-interval '6 minutes'
		where object_route_id=$2`, popularityRefreshMaxAttempts, routeIDs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update content_stats_refresh_queue set status='processing',attempts=3,locked_at=now()-interval '6 minutes'
		where object_route_id=$1`, routeIDs[1]); err != nil {
		t.Fatal(err)
	}
	leaseRecovery, err := claimContentStatsTasks(ctx, pool, 2)
	if err != nil || len(leaseRecovery) != 1 || leaseRecovery[0].routeID != routeIDs[1] || leaseRecovery[0].attempt != 4 {
		t.Fatalf("stale lease recovery=%v/%v, want route %d attempt 4", leaseRecovery, err, routeIDs[1])
	}
	var exhaustedStatus, exhaustedError string
	if err = pool.QueryRow(ctx, `select status,last_error from content_stats_refresh_queue where object_route_id=$1`, routeIDs[0]).
		Scan(&exhaustedStatus, &exhaustedError); err != nil {
		t.Fatal(err)
	}
	if exhaustedStatus != "failed" || exhaustedError == "" {
		t.Fatalf("exhausted stale lease=(%s,%q), want failed with error", exhaustedStatus, exhaustedError)
	}

	var commentID int64
	if err = pool.QueryRow(ctx, `insert into comments(target_type,target_id,author_id,body)
		values('mod',$1,$2,'OPS-010 poison comment') returning id`, projectIDs[30], actorID).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `truncate content_stats_refresh_queue,comment_heat_refresh_queue;
		create table test029_worker_failures(kind text primary key,failing boolean not null);
		insert into test029_worker_failures(kind,failing) values('content',true),('comment',true);
		create or replace function refresh_content_route_metrics(target_route_id bigint) returns void as $$
		begin
			if (select failing from test029_worker_failures where kind='content') then
				raise exception 'TEST029 content refresh failure';
			end if;
		end $$ language plpgsql;
		create or replace function refresh_comment_heat(changed_comment_id bigint) returns void as $$
		begin
			if (select failing from test029_worker_failures where kind='comment') then
				raise exception 'TEST029 comment refresh failure';
			end if;
		end $$ language plpgsql`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_stats_refresh_queue(object_route_id,refresh_metrics,refresh_popularity)
		values($1,true,false)`, routeIDs[30]); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into comment_heat_refresh_queue(comment_id) values($1)`, commentID); err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= popularityRefreshMaxAttempts; attempt++ {
		claimed, claimErr := claimContentStatsTasks(ctx, pool, 1)
		if claimErr != nil || len(claimed) != 1 {
			t.Fatalf("content claim %d=%d/%v", attempt, len(claimed), claimErr)
		}
		if processErr := processContentStatsTask(ctx, pool, claimed[0]); processErr == nil || !strings.Contains(processErr.Error(), "TEST029 content refresh failure") {
			t.Fatalf("content process %d error=%v", attempt, processErr)
		}
		assertOPS010QueueState(t, ctx, pool, "content_stats_refresh_queue", "object_route_id", routeIDs[30], attempt)
		if attempt < popularityRefreshMaxAttempts {
			if _, err = pool.Exec(ctx, `update content_stats_refresh_queue set available_at=now() where object_route_id=$1`, routeIDs[30]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if claimed, claimErr := claimContentStatsTasks(ctx, pool, 1); claimErr != nil || len(claimed) != 0 {
		t.Fatalf("terminal content task remained claimable: %d/%v", len(claimed), claimErr)
	}
	if _, err = pool.Exec(ctx, `select enqueue_content_stats_refresh($1,true,false)`, routeIDs[30]); err != nil {
		t.Fatal(err)
	}
	assertOPS010Reactivated(t, ctx, pool, "content_stats_refresh_queue", "object_route_id", routeIDs[30])
	if _, err = pool.Exec(ctx, `update test029_worker_failures set failing=false where kind='content'`); err != nil {
		t.Fatal(err)
	}
	claimedContent, err := claimContentStatsTasks(ctx, pool, 1)
	if err != nil || len(claimedContent) != 1 {
		t.Fatalf("reactivated content claim=%d/%v", len(claimedContent), err)
	}
	if err = processContentStatsTask(ctx, pool, claimedContent[0]); err != nil {
		t.Fatal(err)
	}
	assertOPS010QueueDeleted(t, ctx, pool, "content_stats_refresh_queue", "object_route_id", routeIDs[30])

	for attempt := 1; attempt <= popularityRefreshMaxAttempts; attempt++ {
		claimed, claimErr := claimCommentHeatTasks(ctx, pool, 1)
		if claimErr != nil || len(claimed) != 1 {
			t.Fatalf("comment claim %d=%d/%v", attempt, len(claimed), claimErr)
		}
		if processErr := processCommentHeatTask(ctx, pool, claimed[0]); processErr == nil || !strings.Contains(processErr.Error(), "TEST029 comment refresh failure") {
			t.Fatalf("comment process %d error=%v", attempt, processErr)
		}
		assertOPS010QueueState(t, ctx, pool, "comment_heat_refresh_queue", "comment_id", commentID, attempt)
		if attempt < popularityRefreshMaxAttempts {
			if _, err = pool.Exec(ctx, `update comment_heat_refresh_queue set available_at=now() where comment_id=$1`, commentID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if claimed, claimErr := claimCommentHeatTasks(ctx, pool, 1); claimErr != nil || len(claimed) != 0 {
		t.Fatalf("terminal comment task remained claimable: %d/%v", len(claimed), claimErr)
	}
	if _, err = pool.Exec(ctx, `select enqueue_comment_heat_refresh($1)`, commentID); err != nil {
		t.Fatal(err)
	}
	assertOPS010Reactivated(t, ctx, pool, "comment_heat_refresh_queue", "comment_id", commentID)
	if _, err = pool.Exec(ctx, `update test029_worker_failures set failing=false where kind='comment'`); err != nil {
		t.Fatal(err)
	}
	claimedComments, err := claimCommentHeatTasks(ctx, pool, 1)
	if err != nil || len(claimedComments) != 1 {
		t.Fatalf("reactivated comment claim=%d/%v", len(claimedComments), err)
	}
	if err = processCommentHeatTask(ctx, pool, claimedComments[0]); err != nil {
		t.Fatal(err)
	}
	assertOPS010QueueDeleted(t, ctx, pool, "comment_heat_refresh_queue", "comment_id", commentID)

	if _, err = pool.Exec(ctx, `update test029_worker_failures set failing=true where kind='content'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_stats_refresh_queue(object_route_id,refresh_metrics,refresh_popularity)
		values($1,true,false)`, routeIDs[30]); err != nil {
		t.Fatal(err)
	}
	brokenRetryTasks, err := claimContentStatsTasks(ctx, pool, 1)
	if err != nil || len(brokenRetryTasks) != 1 {
		t.Fatalf("broken retry claim=%d/%v", len(brokenRetryTasks), err)
	}
	constraintSQL := fmt.Sprintf(`alter table content_stats_refresh_queue add constraint test029_reject_retry
		check(object_route_id<>%d or status='processing')`, routeIDs[30])
	if _, err = pool.Exec(ctx, constraintSQL); err != nil {
		t.Fatal(err)
	}
	processErr := processContentStatsTask(ctx, pool, brokenRetryTasks[0])
	if processErr == nil || !strings.Contains(processErr.Error(), "test029_reject_retry") {
		t.Fatalf("retry persistence failure was swallowed: %v", processErr)
	}
	if _, err = pool.Exec(ctx, `alter table content_stats_refresh_queue drop constraint test029_reject_retry`); err != nil {
		t.Fatal(err)
	}
	if err = retryContentStatsTask(ctx, pool, brokenRetryTasks[0], errors.New("release injected retry failure")); err != nil {
		t.Fatal(err)
	}
}

func assertOPS010QueueState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, idColumn string, id int64, attempt int) {
	t.Helper()
	query := fmt.Sprintf(`select status,attempts,locked_at is null,last_error from %s where %s=$1`, table, idColumn)
	var status, lastError string
	var attempts int
	var unlocked bool
	if err := pool.QueryRow(ctx, query, id).Scan(&status, &attempts, &unlocked, &lastError); err != nil {
		t.Fatal(err)
	}
	wantStatus := "pending"
	if attempt == popularityRefreshMaxAttempts {
		wantStatus = "failed"
	}
	if status != wantStatus || attempts != attempt || !unlocked || lastError == "" {
		t.Fatalf("%s %d=(%s,%d,unlocked=%v,error=%q), want (%s,%d,true,nonempty)",
			table, id, status, attempts, unlocked, lastError, wantStatus, attempt)
	}
}

func assertOPS010Reactivated(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, idColumn string, id int64) {
	t.Helper()
	query := fmt.Sprintf(`select status,attempts,locked_at is null,last_error from %s where %s=$1`, table, idColumn)
	var status, lastError string
	var attempts int
	var unlocked bool
	if err := pool.QueryRow(ctx, query, id).Scan(&status, &attempts, &unlocked, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 0 || !unlocked || lastError != "" {
		t.Fatalf("reactivated %s %d=(%s,%d,unlocked=%v,error=%q)", table, id, status, attempts, unlocked, lastError)
	}
}

func assertOPS010QueueDeleted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, idColumn string, id int64) {
	t.Helper()
	query := fmt.Sprintf(`select count(*) from %s where %s=$1`, table, idColumn)
	var count int
	if err := pool.QueryRow(ctx, query, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("successful %s task %d was not deleted", table, id)
	}
}

func newOPS010IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	requireOCT02DatabaseIntegration(t)
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("ops010_popularity_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^ops010_popularity_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe OPS010 database name %q", databaseName)
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
			t.Errorf("refuse unsafe OPS010 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated OPS010 database: %v", dropErr)
		}
		adminPool.Close()
	})
	scopedConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
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
