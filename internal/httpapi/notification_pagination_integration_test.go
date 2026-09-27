package httpapi

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestNotificationPagesAndReadWatermarkStayBoundedAtOneMillionRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate notification page and watermark scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	fixtureStarted := time.Now()
	if _, err = pool.Exec(ctx, `
		create temporary table users(id bigint primary key,public_id text not null,username text not null,status text not null default 'active');
		create temporary table notifications(
			id bigint primary key,public_id text not null,recipient_id bigint,kind text not null,title text not null,body text not null,
			source_locale text not null,data jsonb not null,created_at timestamptz not null,updated_at timestamptz not null
		);
		create index idx_notifications_recipient_page on notifications(recipient_id,updated_at desc,id desc);
		create index idx_notifications_recipient_kind_page on notifications(recipient_id,kind,updated_at desc,id desc);
		create index idx_notifications_recipient_id on notifications(recipient_id,id);
		create index idx_notifications_broadcast_page on notifications(updated_at desc,id desc) where recipient_id is null;
		create index idx_notifications_broadcast_kind_page on notifications(kind,updated_at desc,id desc) where recipient_id is null;
		create index idx_notifications_broadcast_id on notifications(id) where recipient_id is null;
		create temporary table notification_broadcast_state(
			singleton boolean primary key,live_count bigint not null,max_notification_id bigint not null,updated_at timestamptz not null
		);
		create temporary table notification_receipts(
			notification_id bigint not null,user_id bigint not null,read_at timestamptz,created_at timestamptz not null default now(),
			primary key(notification_id,user_id)
		);
		create temporary table notification_read_watermarks(
			user_id bigint primary key,max_notification_id bigint not null,read_at timestamptz not null,updated_at timestamptz not null
		);
		create temporary table notification_actors(notification_id bigint not null,actor_id bigint not null,primary key(notification_id,actor_id));
		create temporary table direct_messages(id bigint primary key,recipient_id bigint not null,read_at timestamptz);
		create index idx_direct_messages_recipient_read on direct_messages(recipient_id,read_at);
		insert into users
		select value,'u'||lpad(value::text,8,'0'),'Scale user '||value,'active' from generate_series(1,20) value;
		insert into notifications
		select value,'n'||lpad(value::text,8,'0'),
			case when value%4=0 then null when value%4=1 then 1 else 2 end,
			case when value%101=0 then 'review' when value%4=0 then 'system' when value%4=1 then 'reply_mention' when value%4=2 then 'comment_watch_reply' else 'new_follower' end,
			'title-'||value,'body-'||value,'zh-CN','{}'::jsonb,
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 second',
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 second'
		from generate_series(1,1000000) value;
		insert into notification_broadcast_state values(true,250000,1000000,clock_timestamp());
		analyze notifications;
		analyze notification_receipts;
		analyze notification_read_watermarks;
		analyze notification_actors;
		analyze notification_broadcast_state;
		analyze direct_messages;
		analyze users
	`); err != nil {
		t.Fatal(err)
	}
	t.Logf("1m notification fixture loaded in %s", time.Since(fixtureStarted))

	server := &Server{db: pool}
	cursor := ""
	seen := make(map[string]struct{}, 100)
	counter.queries.Store(0)
	for pageNumber := 0; pageNumber < 2; pageNumber++ {
		request, parseErr := parseNotificationPageRequest(url.Values{
			"kind": {"review"}, "limit": {"50"}, "cursor": {cursor},
		}, 1)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		page, pageErr := server.loadNotificationPage(ctx, 1, request)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if len(page.Items) != 50 || !page.HasMore || page.NextCursor == "" {
			t.Fatalf("page %d items=%d hasMore=%t cursor=%q", pageNumber, len(page.Items), page.HasMore, page.NextCursor)
		}
		encoded, marshalErr := json.Marshal(page.Items)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if len(encoded) >= 128*1024 {
			t.Fatalf("notification page exceeded its response fixture bound: %d bytes", len(encoded))
		}
		for _, item := range page.Items {
			if item.Read {
				t.Fatalf("notification %s was read before the watermark", item.ID)
			}
			if _, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("duplicate notification %s", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		cursor = page.NextCursor
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("two notification pages executed %d SQL statements, want 2", queries)
	}

	counter.queries.Store(0)
	var firstReadBefore time.Time
	if err = pool.QueryRow(ctx, markAllNotificationsReadSQL, int64(1)).Scan(&firstReadBefore); err != nil {
		t.Fatal(err)
	}
	if queries := counter.queries.Load(); queries != 1 {
		t.Fatalf("read-all executed %d client SQL statements, want 1", queries)
	}
	var receipts, watermarks int64
	var maxNotificationID int64
	if err = pool.QueryRow(ctx, `select
		(select count(*) from notification_receipts),
		(select count(*) from notification_read_watermarks),
		(select max_notification_id from notification_read_watermarks where user_id=1)`).Scan(&receipts, &watermarks, &maxNotificationID); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 || watermarks != 1 || maxNotificationID != 1000000 {
		t.Fatalf("read-all receipts=%d watermarks=%d max=%d", receipts, watermarks, maxNotificationID)
	}

	firstRequest, err := parseNotificationPageRequest(url.Values{"kind": {"review"}, "limit": {"50"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	readPage, err := server.loadNotificationPage(ctx, 1, firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range readPage.Items {
		if !item.Read {
			t.Fatalf("pre-watermark notification %s remained unread", item.ID)
		}
	}

	if _, err = pool.Exec(ctx, `
		insert into notifications values(1000001,'n01000001',1,'review','new','new','zh-CN','{}',clock_timestamp(),clock_timestamp());
		update notifications set updated_at=clock_timestamp(),title='updated' where id=999597
	`); err != nil {
		t.Fatal(err)
	}
	var unread int64
	if err = pool.QueryRow(ctx, `select count(*) from notifications n
		left join notification_receipts receipt on receipt.notification_id=n.id and receipt.user_id=$1
		left join notification_read_watermarks watermark on watermark.user_id=$1
		where (n.recipient_id is null or n.recipient_id=$1) and `+notificationUnreadPredicateSQL, int64(1)).Scan(&unread); err != nil {
		t.Fatal(err)
	}
	if unread != 2 {
		t.Fatalf("post-watermark new/update unread=%d want 2", unread)
	}
	var secondReadBefore time.Time
	if err = pool.QueryRow(ctx, markAllNotificationsReadSQL, int64(1)).Scan(&secondReadBefore); err != nil {
		t.Fatal(err)
	}
	if !secondReadBefore.After(firstReadBefore) {
		t.Fatalf("watermark did not advance: first=%s second=%s", firstReadBefore, secondReadBefore)
	}
	if err = pool.QueryRow(ctx, `select count(*) from notifications n
		left join notification_receipts receipt on receipt.notification_id=n.id and receipt.user_id=$1
		left join notification_read_watermarks watermark on watermark.user_id=$1
		where (n.recipient_id is null or n.recipient_id=$1) and `+notificationUnreadPredicateSQL, int64(1)).Scan(&unread); err != nil {
		t.Fatal(err)
	}
	if unread != 0 {
		t.Fatalf("second read-all left %d unread notifications", unread)
	}
	if err = pool.QueryRow(ctx, `select count(*) from notification_receipts`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("read-all materialized receipts=%d err=%v", receipts, err)
	}

	planRequest := notificationPageRequest{Kind: "review", Limit: 50, Cursor: &notificationPageCursor{
		UpdatedAt: time.Date(2026, time.January, 6, 18, 53, 20, 0, time.UTC), ID: 500000,
	}}
	planSQL, planArgs := notificationPageSQL(1, planRequest)
	assertNotificationScalePlan(t, ctx, pool, "1m merged notification page", planSQL, planArgs,
		[]string{"idx_notifications_recipient_kind_page", "idx_notifications_broadcast_kind_page"})
	allPlanSQL, allPlanArgs := notificationPageSQL(1, notificationPageRequest{Limit: 50, Cursor: &notificationPageCursor{
		UpdatedAt: time.Date(2026, time.January, 6, 18, 53, 20, 0, time.UTC), ID: 500000,
	}})
	assertNotificationScalePlan(t, ctx, pool, "1m unfiltered notification page", allPlanSQL, allPlanArgs,
		[]string{"idx_notifications_recipient_page", "idx_notifications_broadcast_page"})
	assertNotificationScalePlan(t, ctx, pool, "1m read watermark", `select coalesce(max(id),0) from notifications`, nil,
		[]string{"notifications_pkey"})

	sampledUserIDs := make([]int64, 0, unreadReconcileMaxSampleSize)
	for userID := int64(3); userID < 3+unreadReconcileMaxSampleSize; userID++ {
		sampledUserIDs = append(sampledUserIDs, userID)
	}
	counter.queries.Store(0)
	truth, err := loadUnreadTruth(ctx, pool, unreadTruthInternalUsersSQL, sampledUserIDs)
	if err != nil {
		t.Fatal(err)
	}
	if queries := counter.queries.Load(); queries != 1 {
		t.Fatalf("%d sampled users executed %d truth SQL statements, want one", len(sampledUserIDs), queries)
	}
	if len(truth) != unreadReconcileMaxSampleSize {
		t.Fatalf("truth items=%d want %d", len(truth), unreadReconcileMaxSampleSize)
	}
	for _, item := range truth {
		if item.Summary.Notifications != 250000 || item.Summary.Messages != 0 {
			t.Fatalf("user %d truth=%+v want shared broadcast count only", item.UserID, item.Summary)
		}
	}
	assertNotificationScalePlan(t, ctx, pool, "16-user cached unread sample", unreadTruthInternalUsersSQL,
		[]any{sampledUserIDs}, []string{"idx_notifications_recipient_id"})

	if _, err = pool.Exec(ctx, `
		insert into notifications values(1000002,'n01000002',3,'review','direct','direct','zh-CN','{}',clock_timestamp(),clock_timestamp());
		insert into direct_messages values(1,3,null),(2,3,null),(3,3,clock_timestamp())
	`); err != nil {
		t.Fatal(err)
	}
	readTag, err := pool.Exec(ctx, markNotificationReadSQL, int64(4), int64(3))
	if err != nil || readTag.RowsAffected() != 1 {
		t.Fatalf("read one broadcast affected=%d err=%v", readTag.RowsAffected(), err)
	}
	truth, err = loadUnreadTruth(ctx, pool, unreadTruthInternalUsersSQL, []int64{3})
	if err != nil || len(truth) != 1 || truth[0].Summary.Notifications != 250000 || truth[0].Summary.Messages != 2 {
		t.Fatalf("mixed unread truth=%+v err=%v", truth, err)
	}
	if err = pool.QueryRow(ctx, markAllNotificationsReadSQL, int64(3)).Scan(&secondReadBefore); err != nil {
		t.Fatal(err)
	}
	readTag, err = pool.Exec(ctx, markNotificationReadSQL, int64(8), int64(3))
	if err != nil || readTag.RowsAffected() != 0 {
		t.Fatalf("watermark-read item changed the derivative affected=%d err=%v", readTag.RowsAffected(), err)
	}
	truth, err = loadUnreadTruth(ctx, pool, unreadTruthInternalUsersSQL, []int64{3})
	if err != nil || len(truth) != 1 || truth[0].Summary.Notifications != 0 || truth[0].Summary.Messages != 2 {
		t.Fatalf("post-watermark unread truth=%+v err=%v", truth, err)
	}
}

func assertNotificationScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name, query string, args []any, indexes []string) {
	t.Helper()
	rows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0)
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	plan := strings.Join(lines, "\n")
	for _, index := range indexes {
		if !strings.Contains(plan, index) {
			t.Fatalf("%s did not use %s:\n%s", name, index, plan)
		}
	}
	if strings.Contains(plan, "Seq Scan on notifications") {
		t.Fatalf("%s scanned notifications:\n%s", name, plan)
	}
	t.Logf("%s plan:\n%s", name, plan)
}
