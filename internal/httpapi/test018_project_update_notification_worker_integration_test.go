package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestTEST018ProjectUpdateWorkerBatchesFiltersRetriesAndRecoversIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("TEST-018 exercises an isolated PostgreSQL project-update notification state machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)

	stamp := time.Now().UnixNano()
	var actorID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,preferred_ui_language)
		values($1,$2,'test018',true,'zh-CN') returning id`, fmt.Sprintf("test018-actor-%d", stamp),
		fmt.Sprintf("test018-actor-%d@example.invalid", stamp)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into users(username,email,password_hash,email_verified,preferred_ui_language,status)
		select $1||'-'||ordinal,$1||'-'||ordinal||'@example.invalid','test018',true,
			case when ordinal%2=0 then 'en-US' else 'zh-CN' end,
			case when ordinal=205 then 'suspended' else 'active' end
		from generate_series(1,205) ordinal`, fmt.Sprintf("test018-recipient-%d", stamp)); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `select id from users where username like $1 order by id`, fmt.Sprintf("test018-recipient-%d-%%", stamp))
	if err != nil {
		t.Fatal(err)
	}
	recipientIDs := make([]int64, 0, 205)
	for rows.Next() {
		var userID int64
		if err = rows.Scan(&userID); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		recipientIDs = append(recipientIDs, userID)
	}
	if err = finishRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(recipientIDs) != 205 {
		t.Fatalf("created %d recipient fixtures, want 205", len(recipientIDs))
	}

	var projectID, routeID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('t018m0001','test018-worker-state','TEST-018 notification project','approved',$1) returning id`, actorID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into project_follows(user_id,project_route_id)
		select id,$1::bigint from users where username like $2
		union all select $3::bigint,$1::bigint`, routeID, fmt.Sprintf("test018-recipient-%d-%%", stamp), actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update project_follows set notifications_enabled=false where user_id=$1`, recipientIDs[201]); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_notification_settings(user_id,project_updates_enabled) values($1,false)`, recipientIDs[202]); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from project_follows where user_id=$1 and project_route_id=$2`, recipientIDs[203], routeID); err != nil {
		t.Fatal(err)
	}

	cache := querycache.New(appconfig.RedisConfig{UnreadTTL: time.Minute})
	cache.SetUnread(ctx, recipientIDs[0], querycache.UnreadSummary{}, 0)
	workerA := NewProjectUpdateNotificationWorker(pool, nil, cache)
	workerB := NewProjectUpdateNotificationWorker(pool, nil, cache)
	eventID := insertTEST018ProjectUpdateTask(t, ctx, pool, routeID, actorID, "initial")

	start := make(chan struct{})
	errorsByWorker := make(chan error, 2)
	var workers sync.WaitGroup
	for _, worker := range []*ProjectUpdateNotificationWorker{workerA, workerB} {
		workers.Add(1)
		go func(value *ProjectUpdateNotificationWorker) {
			defer workers.Done()
			<-start
			errorsByWorker <- value.process(ctx, eventID)
		}(worker)
	}
	close(start)
	workers.Wait()
	close(errorsByWorker)
	for workerErr := range errorsByWorker {
		if workerErr != nil {
			t.Fatal(workerErr)
		}
	}
	assertTEST018Task(t, ctx, pool, eventID, "completed", recipientIDs[200], 201, 0)
	assertTEST018Deliveries(t, ctx, pool, eventID, 201, 101, 100)
	assertTEST018NoDeliveries(t, ctx, pool, eventID, append([]int64{actorID}, recipientIDs[201:]...))
	assertTEST018CachedUnread(t, ctx, cache, recipientIDs[0], 1)

	if err = workerA.process(ctx, eventID); err != nil {
		t.Fatal(err)
	}
	assertTEST018Task(t, ctx, pool, eventID, "completed", recipientIDs[200], 201, 0)
	assertTEST018Deliveries(t, ctx, pool, eventID, 201, 101, 100)
	assertTEST018CachedUnread(t, ctx, cache, recipientIDs[0], 1)

	retryEventID := insertTEST018ProjectUpdateTask(t, ctx, pool, routeID, actorID, "transient")
	if _, err = pool.Exec(ctx, `alter table users rename column preferred_ui_language to test018_broken_locale`); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(projectUpdateTaskMessage{EventID: retryEventID})
	if err = workerA.handle(ctx, raw); err == nil {
		t.Fatal("transient recipient query failure was not returned")
	}
	assertTEST018Task(t, ctx, pool, retryEventID, "pending", 0, 0, 1)
	if _, err = pool.Exec(ctx, `alter table users rename column test018_broken_locale to preferred_ui_language`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update project_update_notification_tasks set next_attempt_at=now() where event_id=$1`, retryEventID); err != nil {
		t.Fatal(err)
	}
	if err = workerA.process(ctx, retryEventID); err != nil {
		t.Fatal(err)
	}
	if err = workerB.process(ctx, retryEventID); err != nil {
		t.Fatal(err)
	}
	assertTEST018Task(t, ctx, pool, retryEventID, "completed", recipientIDs[200], 201, 1)
	assertTEST018Deliveries(t, ctx, pool, retryEventID, 201, 101, 100)
	assertTEST018CachedUnread(t, ctx, cache, recipientIDs[0], 2)

	staleEventID := insertTEST018ProjectUpdateTask(t, ctx, pool, routeID, actorID, "stale-processing")
	if _, err = pool.Exec(ctx, `update project_update_notification_tasks
		set status='processing',updated_at=now()-interval '6 minutes',next_attempt_at=now() where event_id=$1`, staleEventID); err != nil {
		t.Fatal(err)
	}
	if err = workerA.processPending(ctx); err != nil {
		t.Fatal(err)
	}
	if err = workerB.processPending(ctx); err != nil {
		t.Fatal(err)
	}
	assertTEST018Task(t, ctx, pool, staleEventID, "completed", recipientIDs[200], 201, 0)
	assertTEST018Deliveries(t, ctx, pool, staleEventID, 201, 101, 100)
	assertTEST018CachedUnread(t, ctx, cache, recipientIDs[0], 3)

	hiddenEventID := insertTEST018ProjectUpdateTask(t, ctx, pool, routeID, actorID, "hidden-target")
	if _, err = pool.Exec(ctx, `update mods set review_status='pending' where id=$1`, projectID); err != nil {
		t.Fatal(err)
	}
	if err = workerA.process(ctx, hiddenEventID); err != nil {
		t.Fatal(err)
	}
	var hiddenStatus, hiddenError string
	var hiddenCount int64
	if err = pool.QueryRow(ctx, `select status,last_error,notified_count from project_update_notification_tasks where event_id=$1`, hiddenEventID).
		Scan(&hiddenStatus, &hiddenError, &hiddenCount); err != nil {
		t.Fatal(err)
	}
	if hiddenStatus != "completed" || hiddenError != "target unavailable" || hiddenCount != 0 {
		t.Fatalf("hidden task=(%s,%q,%d), want completed target unavailable without delivery", hiddenStatus, hiddenError, hiddenCount)
	}
	assertTEST018CachedUnread(t, ctx, cache, recipientIDs[0], 3)
}

func insertTEST018ProjectUpdateTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID, actorID int64, batch string) int64 {
	t.Helper()
	var eventID int64
	if err := pool.QueryRow(ctx, `insert into project_update_events(
		project_route_id,actor_user_id,update_kind,changed_sections,publication_batch_id)
		values($1,$2,'content_updated',array['description','download_files'],$3) returning id`, routeID, actorID,
		"test018:"+batch).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_update_notification_tasks(event_id,next_attempt_at) values($1,now())`, eventID); err != nil {
		t.Fatal(err)
	}
	return eventID
}

func assertTEST018Task(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID int64, wantStatus string, wantCursor, wantCount int64, wantAttempts int) {
	t.Helper()
	var status string
	var cursor, count int64
	var attempts int
	if err := pool.QueryRow(ctx, `select status,next_user_id,notified_count,attempt_count
		from project_update_notification_tasks where event_id=$1`, eventID).Scan(&status, &cursor, &count, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || cursor != wantCursor || count != wantCount || attempts != wantAttempts {
		t.Fatalf("task %d=(status=%s,cursor=%d,count=%d,attempts=%d), want (%s,%d,%d,%d)",
			eventID, status, cursor, count, attempts, wantStatus, wantCursor, wantCount, wantAttempts)
	}
}

func assertTEST018Deliveries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID int64, wantTotal, wantZH, wantEN int) {
	t.Helper()
	var total, distinctRecipients, zh, en int
	if err := pool.QueryRow(ctx, `select count(*),count(distinct recipient_id),
		count(*) filter(where source_locale='zh-CN'),count(*) filter(where source_locale='en-US')
		from notifications where project_update_event_id=$1`, eventID).Scan(&total, &distinctRecipients, &zh, &en); err != nil {
		t.Fatal(err)
	}
	if total != wantTotal || distinctRecipients != wantTotal || zh != wantZH || en != wantEN {
		t.Fatalf("event %d deliveries=(total=%d,distinct=%d,zh=%d,en=%d), want (%d,%d,%d,%d)",
			eventID, total, distinctRecipients, zh, en, wantTotal, wantTotal, wantZH, wantEN)
	}
}

func assertTEST018NoDeliveries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID int64, userIDs []int64) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from notifications
		where project_update_event_id=$1 and recipient_id=any($2)`, eventID, userIDs).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("event %d delivered %d notifications to excluded recipients", eventID, count)
	}
}

func assertTEST018CachedUnread(t *testing.T, ctx context.Context, cache *querycache.Cache, userID, want int64) {
	t.Helper()
	value, err := cache.LoadUnread(ctx, userID, func(context.Context) (querycache.UnreadSummary, error) {
		return querycache.UnreadSummary{}, fmt.Errorf("cached unread derivative for user %d was unexpectedly invalidated", userID)
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.Notifications != want {
		t.Fatalf("cached unread notifications=%d, want %d", value.Notifications, want)
	}
}

func newTEST018IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test018_project_update_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test018_project_update_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST018 database name %q", databaseName)
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
			t.Errorf("refuse unsafe TEST018 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST018 database: %v", dropErr)
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
