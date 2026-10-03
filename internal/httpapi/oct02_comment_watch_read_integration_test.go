package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02CommentWatchReadKeepsConcurrentReplyCountConsistentIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `alter table public_routes add column canonical_path text;
 alter table community_posts add column public_id text,add column author_id bigint,add column title text;
 insert into community_posts(id,status,review_status,public_id,author_id,title) values(7,'active','approved','c00000007',42,'fixture');
 insert into public_routes values(7,'c00000007','community_post','/community/c00000007');
 create table comments(id bigint primary key,public_id text,root_id bigint,status text,target_type text,target_id bigint,target_version_id bigint);
 insert into comments values(8,'c00000008',null,'published','community_post',7,null);
 create table comment_watches(id bigint primary key,public_id text,user_id bigint,comment_id bigint,status text,unread_count bigint,last_read_comment_id bigint,updated_at timestamptz);
 insert into comment_watches values(9,'w00000009',42,8,'active',1,null,now());
 create table comment_watch_replies(watch_id bigint,comment_id bigint,read_at timestamptz,created_at timestamptz);
 insert into comment_watch_replies values(9,10,null,now()-interval '1 second')`); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, `update comment_watches set unread_count=unread_count+1 where id=9`); err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.Exec(ctx, `insert into comment_watch_replies values(9,11,null,now())`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/comment-watches/w00000009/read", nil)
	request.SetPathValue("watchId", "w00000009")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 42, PermissionRules: []security.PermissionRule{{Code: "comment.watch", Allow: true}}}))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); (&Server{db: pool}).commentWatchItem(response, request) }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err = pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where datname=current_database() and wait_event_type='Lock' and (query like '%comment_watches%' and query not like '%pg_stat_activity%'))`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("read handler did not reach watch lock")
		case <-ticker.C:
		}
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("read handler did not complete")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", response.Code, response.Body.String())
	}
	var counter, unread int64
	if err = pool.QueryRow(ctx, `select unread_count,(select count(*) from comment_watch_replies where watch_id=9 and read_at is null) from comment_watches where id=9`).Scan(&counter, &unread); err != nil {
		t.Fatal(err)
	}
	if counter != unread {
		t.Fatalf("watch unread counter=%d, actual unread replies=%d", counter, unread)
	}
}
