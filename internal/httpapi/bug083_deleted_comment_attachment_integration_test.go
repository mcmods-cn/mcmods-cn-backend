package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestDeletedCommentDetachesAttachmentsAtomicallyAndPreservesLogShareIntegration(t *testing.T) {
	ctx, pool := newBUG083Pool(t)
	if err := database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Error(err)
		}
	})
	if _, err := pool.Exec(ctx, `
		create temporary sequence bug083_reject_detach_once;
		create function pg_temp.reject_first_bug083_detach() returns trigger language plpgsql as $$
		begin
			if nextval('pg_temp.bug083_reject_detach_once')=1 then
				raise exception 'BUG-083 transient attachment detach failure';
			end if;
			return old;
		end $$;
		create trigger reject_first_bug083_detach before delete on comment_attachments
			for each row execute function pg_temp.reject_first_bug083_detach();
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into users(id,username,email,password_hash,email_verified) values(831,'bug083-author','bug083@example.invalid','disabled',true);
 insert into mods(id,project_code,slug,primary_name,review_status,submitted_by) values(832,'b083mod01','bug083-mod','BUG083 Mod','approved',831);
 insert into oss_files(id,object_key,content_type,status,scan_status) values(834,'bug083/latest.log','text/plain','active','clean');
		insert into comments(id,public_id,author_id,body,target_type,target_id,status)
		values(830,'b083c0001',831,'private crash path','mod',832,'published');
		insert into log_shares(id,public_code,owner_user_id,source_type,source_file_id,comment_scope_id,status,expires_at)
		values(833,'bug083_share01',831,'file',834,830,'ready',now()+interval '1 day');
		insert into comment_attachments(comment_id,attachment_file_id,kind,processing_status) values(830,834,'log','ready');
		insert into comment_log_bindings(comment_id,attachment_file_id,log_share_id) values(830,834,833);
		insert into comment_log_attachment_jobs(id,comment_id,attachment_file_id,requested_by) values(835,830,834,831);
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	if _, err := server.resolveVisibleComment(ctx, "b083c0001", security.Claims{Subject: 831}); err != nil {
		t.Fatalf("resolve visible BUG083 comment: %v", err)
	}
	first := invokeBUG083CommentDelete(t, ctx, server)
	if first.Code != http.StatusInternalServerError || !strings.Contains(first.Body.String(), "删除评论失败") {
		t.Fatalf("first delete status=%d body=%s, want rollback error", first.Code, first.Body.String())
	}
	var sequenceValue int64
	var called bool
	if err := pool.QueryRow(ctx, `select last_value,is_called from pg_temp.bug083_reject_detach_once`).Scan(&sequenceValue, &called); err != nil || sequenceValue != 1 || !called {
		t.Fatalf("intended detach fault was not reached: value=%d called=%v err=%v", sequenceValue, called, err)
	}
	var intents int
	if err := pool.QueryRow(ctx, `select count(*) from activity_event_outbox`).Scan(&intents); err != nil || intents != 0 {
		t.Fatalf("failed detach intents=%d err=%v", intents, err)
	}
	assertBUG083State(t, ctx, pool, "published", "private crash path", 1, 1, 1, 1)

	second := invokeBUG083CommentDelete(t, ctx, server)
	if second.Code != http.StatusOK {
		t.Fatalf("retry delete status=%d body=%s", second.Code, second.Body.String())
	}
	assertBUG083State(t, ctx, pool, "deleted", "", 0, 0, 0, 1)
	if err := pool.QueryRow(ctx, `select count(*) from activity_event_outbox where user_id=831 and object_public_id='b083c0001'`).Scan(&intents); err != nil || intents != 1 {
		t.Fatalf("delete recovery intents=%d err=%v", intents, err)
	}
}

func TestDeletedCommentSerializationNeverReturnsAttachmentMetadataIntegration(t *testing.T) {
	ctx, pool := newBUG083Pool(t)
	if _, err := pool.Exec(ctx, `
		create temporary table oss_files(
			id bigint primary key,public_id text not null,source_original_name text not null,original_name text not null,
			content_type text not null,size_bytes bigint not null,status text not null,scan_status text not null
		);
		create temporary table comment_attachments(
			comment_id bigint not null,attachment_file_id bigint not null,kind text not null,
			processing_status text not null,created_at timestamptz not null default now()
		);
		create temporary table comment_log_bindings(comment_id bigint,attachment_file_id bigint,log_share_id bigint);
		create temporary table log_shares(id bigint primary key,public_code text,status text,expires_at timestamptz,
			redaction_applied_version integer not null default 1);
		insert into oss_files values(841,'b083file','latest.log','latest.log','text/plain',4096,'active','clean');
		insert into comment_attachments(comment_id,attachment_file_id,kind,processing_status) values(840,841,'log','ready');
		insert into log_shares(id,public_code,status,expires_at) values(842,'b083leak','ready',now()+interval '1 day');
		insert into comment_log_bindings values(840,841,842);
	`); err != nil {
		t.Fatal(err)
	}
	items := []commentResponse{{ID: "b083deleted", internalID: 840, Deleted: true}}
	if err := (&Server{db: pool}).annotateCommentAttachments(ctx, items); err != nil {
		t.Fatal(err)
	}
	if len(items[0].Attachments) != 0 {
		t.Fatalf("deleted comment leaked attachments: %+v", items[0].Attachments)
	}
}

func newBUG083Pool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify deleted comment attachments")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx, pool
}

func invokeBUG083CommentDelete(t *testing.T, ctx context.Context, server *Server) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/comments/b083c0001", nil)
	request.SetPathValue("commentId", "b083c0001")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{
		Subject:         831,
		PermissionRules: []security.PermissionRule{{Code: "comment.delete.own", Allow: true}},
	}))
	response := httptest.NewRecorder()
	server.commentItem(response, request)
	return response
}

func assertBUG083State(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wantStatus, wantBody string,
	wantAttachments, wantBindings, wantJobs, wantShares int) {
	t.Helper()
	var status, body string
	var attachments, bindings, jobs, shares int
	if err := pool.QueryRow(ctx, `select status,body,
		(select count(*) from comment_attachments),(select count(*) from comment_log_bindings),
		(select count(*) from comment_log_attachment_jobs),(select count(*) from log_shares)
		from comments where id=830`).Scan(&status, &body, &attachments, &bindings, &jobs, &shares); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || body != wantBody || attachments != wantAttachments || bindings != wantBindings || jobs != wantJobs || shares != wantShares {
		t.Fatalf("state=%s/%q attachments/bindings/jobs/shares=%d/%d/%d/%d, want %s/%q %d/%d/%d/%d",
			status, body, attachments, bindings, jobs, shares,
			wantStatus, wantBody, wantAttachments, wantBindings, wantJobs, wantShares)
	}
}
