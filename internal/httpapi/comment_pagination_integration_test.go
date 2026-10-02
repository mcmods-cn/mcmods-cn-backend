package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommentKeysetPagesAndCounterFactsAtMillionRowScaleIntegration(t *testing.T) {
	pool, ctx := openDeadLetterPageTestDB(t, 3*time.Minute)
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		create temporary table comments(
			id bigint primary key,public_id text not null,target_type text not null,target_id bigint not null,
			target_version_id bigint,parent_id bigint,status text not null,author_id bigint not null,
			pinned_at timestamptz,created_at timestamptz not null,hot_score numeric(16,6) not null,
			descendant_count integer not null);
		create temporary table user_blocks(blocker_id bigint not null,blocked_id bigint not null,primary key(blocker_id,blocked_id));
		create temporary table comment_watches(
			id bigint primary key,public_id text not null,user_id bigint not null,comment_id bigint not null,status text not null,
			muted_until timestamptz,muted_forever boolean not null,unread_count integer not null,
			watched_reply_count integer not null,created_at timestamptz not null,last_activity_at timestamptz not null);
		create temporary table comment_target_counts(
			target_type text not null,target_id bigint not null,target_version_key bigint not null,visible_count bigint not null,
			primary key(target_type,target_id,target_version_key));
		create temporary table comment_target_author_counts(
			target_type text not null,target_id bigint not null,target_version_key bigint not null,author_id bigint not null,
			visible_count bigint not null,primary key(target_type,target_id,target_version_key,author_id));
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into comments
		select value,value::text,'mod',42,null,null,'published',case when value%10=0 then 2 else 1 end,
			null,$1::timestamptz-value*interval '1 second',(value%1000)::numeric,(value%100)::integer
		from generate_series(1,1000000) value`, base); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into comments
		select 1000000+value,(1000000+value)::text,'mod',42,null,1,'published',case when value%10=0 then 2 else 1 end,
			null,$1::timestamptz-value*interval '1 second',0,0
		from generate_series(1,100000) value`, base); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into comment_watches
		select value,value::text,7,value,'active',null,false,(value%10)::integer,value::integer,
			$1::timestamptz-value*interval '2 seconds',$1::timestamptz-value*interval '1 second'
		from generate_series(1,2000) value`, base); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into comment_target_counts values('mod',42,0,1100000);
		insert into comment_target_author_counts values('mod',42,0,2,110000);
		insert into user_blocks values(7,2);
		create index test_comments_latest on comments(target_type,target_id,coalesce(target_version_id,0),
			(pinned_at is not null) desc,pinned_at desc,created_at desc,id desc)
			where parent_id is null and status in ('published','deleted');
		create index test_comments_oldest on comments(target_type,target_id,coalesce(target_version_id,0),
			(pinned_at is not null) desc,pinned_at desc,created_at,id)
			where parent_id is null and status in ('published','deleted');
		create index test_comments_hot on comments(target_type,target_id,coalesce(target_version_id,0),
			(pinned_at is not null) desc,pinned_at desc,hot_score desc,created_at desc,id desc)
			where parent_id is null and status in ('published','deleted');
		create index test_comments_replies on comments(target_type,target_id,coalesce(target_version_id,0),
			(pinned_at is not null) desc,pinned_at desc,descendant_count desc,created_at desc,id desc)
			where parent_id is null and status in ('published','deleted');
		create index test_comment_children on comments(parent_id,created_at,id)
			where status in ('published','deleted');
		create index test_watches_activity on comment_watches(user_id,last_activity_at desc,id desc) where status='active';
		create index test_watches_created on comment_watches(user_id,created_at desc,id desc) where status='active';
		create index test_watches_unread on comment_watches(user_id,unread_count desc,last_activity_at desc,id desc) where status='active';
		analyze comments;analyze comment_watches;analyze user_blocks`); err != nil {
		t.Fatal(err)
	}

	target := commentTargetInfo{Type: "mod", InternalID: 42}
	rootCases := []struct {
		name   string
		sort   string
		cursor commentRootPageCursor
	}{
		{name: "latest", sort: "latest", cursor: commentRootPageCursor{CreatedAt: base.Add(-900000 * time.Second), ID: 900000}},
		{name: "oldest", sort: "oldest", cursor: commentRootPageCursor{CreatedAt: base.Add(-100000 * time.Second), ID: 100000}},
		{name: "hot", sort: "hot", cursor: commentRootPageCursor{CreatedAt: base.Add(-500000 * time.Second), HotScore: "0.000000", ID: 500000}},
		{name: "replies", sort: "replies", cursor: commentRootPageCursor{CreatedAt: base.Add(-500000 * time.Second), DescendantCount: 0, ID: 500000}},
	}
	for _, test := range rootCases {
		t.Run(test.name, func(t *testing.T) {
			cursor := test.cursor
			cursor.Version = commentPageCursorVersion
			cursor.Scope = commentRootCursorScope(target, 0)
			cursor.Sort = test.sort
			query, args := buildCommentRootPageQuery(target, 0, 20, test.sort, &cursor)
			assertCommentPageUsesBoundedIndexPlan(t, ctx, pool, query, args...)
			if count := countCommentPageRows(t, ctx, pool, query, args...); count == 0 || count > 21 {
				t.Fatalf("page rows=%d want 1..21", count)
			}
		})
	}
	firstQuery, firstArgs := buildCommentRootPageQuery(target, 0, 20, "latest", nil)
	firstPage := readCommentRootPageRows(t, ctx, pool, firstQuery, firstArgs...)
	if len(firstPage) != 21 {
		t.Fatalf("first latest page rows=%d want 21", len(firstPage))
	}
	anchor := firstPage[19]
	if _, err := pool.Exec(ctx, `insert into comments values(
		2000001,'2000001','mod',42,null,null,'published',1,null,$1::timestamptz+interval '1 hour',0,0)`, base); err != nil {
		t.Fatal(err)
	}
	stableCursor := &commentRootPageCursor{CreatedAt: anchor.createdAt, HotScore: anchor.hotScore, DescendantCount: anchor.descendantCount, ID: anchor.id}
	secondQuery, secondArgs := buildCommentRootPageQuery(target, 0, 20, "latest", stableCursor)
	secondPage := readCommentRootPageRows(t, ctx, pool, secondQuery, secondArgs...)
	seen := make(map[int64]struct{}, 20)
	for _, row := range firstPage[:20] {
		seen[row.id] = struct{}{}
	}
	for _, row := range secondPage {
		if _, duplicated := seen[row.id]; duplicated || row.id == 2000001 {
			t.Fatalf("concurrent insert drifted across keyset pages at id=%d", row.id)
		}
	}

	replyCursor := &commentReplyPageCursor{CreatedAt: base.Add(-10000 * time.Second), ID: 1010000}
	replyQuery, replyArgs := buildCommentReplyPageQuery(1, 0, 20, replyCursor)
	assertCommentPageUsesBoundedIndexPlan(t, ctx, pool, replyQuery, replyArgs...)
	if count := countCommentPageRows(t, ctx, pool, replyQuery, replyArgs...); count != 21 {
		t.Fatalf("reply page rows=%d want 21", count)
	}

	watchCursor := &commentWatchPageCursor{
		UnreadCount: 0, LastActivityAt: base.Add(-1900 * time.Second), CreatedAt: base.Add(-3800 * time.Second), ID: 1900,
	}
	watchQuery, watchArgs := buildCommentWatchPageQuery(7, 20, "all", "activity", watchCursor)
	assertCommentPageUsesBoundedIndexPlan(t, ctx, pool, watchQuery, watchArgs...)
	if count := countCommentPageRows(t, ctx, pool, watchQuery, watchArgs...); count != 21 {
		t.Fatalf("watch page rows=%d want 21", count)
	}

	server := &Server{db: pool}
	started := time.Now()
	total, err := server.queryCommentTargetVisibleTotal(ctx, target, 7)
	if err != nil {
		t.Fatal(err)
	}
	if total != 990000 {
		t.Fatalf("visible total=%d want 990000", total)
	}
	t.Logf("million-row target counter fact wall=%s", time.Since(started))
}

type commentRootIntegrationRow struct {
	id              int64
	pinnedAt        *time.Time
	createdAt       time.Time
	hotScore        string
	descendantCount int
}

func readCommentRootPageRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) []commentRootIntegrationRow {
	t.Helper()
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	values := make([]commentRootIntegrationRow, 0, 21)
	for rows.Next() {
		var value commentRootIntegrationRow
		if err = rows.Scan(&value.id, &value.pinnedAt, &value.createdAt, &value.hotScore, &value.descendantCount); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func assertCommentPageUsesBoundedIndexPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	started := time.Now()
	var plan string
	if err := pool.QueryRow(ctx, "explain (analyze,buffers,format json) "+query, args...).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan, `"Node Type": "Seq Scan"`) {
		t.Fatalf("deep keyset page used a sequential scan: %s", plan)
	}
	t.Logf("deep keyset plan wall=%s", time.Since(started))
}

func countCommentPageRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return count
}
