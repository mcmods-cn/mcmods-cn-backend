package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestCommentEditRejectsStaleAndMissingVersionIntegration(t *testing.T) {
	ctx, pool := newBUG084Pool(t)
	if err := database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Error(err)
		}
	})
	if _, err := pool.Exec(ctx, `
 insert into users(id,username,email,password_hash,email_verified) values(841,'bug084-author','bug084@example.invalid','disabled',true);
 insert into mods(id,project_code,slug,primary_name,review_status,submitted_by) values(842,'b084mod01','bug084-mod','BUG084 Mod','approved',841);
		insert into comments(id,public_id,author_id,body,target_type,target_id,status,updated_at)
		values(840,'b084c0001',841,'original body','mod',842,'published','2026-08-23T01:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	if _, err := server.resolveVisibleComment(ctx, "b084c0001", security.Claims{Subject: 841}); err != nil {
		t.Fatalf("resolve visible BUG084 comment: %v", err)
	}
	baseUpdatedAt := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `update comments set body='first editor body',updated_at=$2 where id=$1`,
		840, baseUpdatedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	stale := invokeBUG084CommentPatch(t, ctx, server, map[string]any{
		"body": "second editor overwrite", "baseUpdatedAt": baseUpdatedAt,
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale edit status=%d body=%s", stale.Code, stale.Body.String())
	}
	var conflict struct {
		Code    string `json:"code"`
		Details struct {
			Body      string    `json:"body"`
			UpdatedAt time.Time `json:"updatedAt"`
			Deleted   bool      `json:"deleted"`
		} `json:"details"`
	}
	if err := json.Unmarshal(stale.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Code != "COMMENT_EDIT_CONFLICT" || conflict.Details.Body != "first editor body" ||
		!conflict.Details.UpdatedAt.Equal(baseUpdatedAt.Add(time.Second)) || conflict.Details.Deleted {
		t.Fatalf("conflict payload = %+v", conflict)
	}
	assertBUG084Body(t, ctx, pool, "first editor body")

	missing := invokeBUG084CommentPatch(t, ctx, server, map[string]any{"body": "missing version overwrite"})
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing version status=%d body=%s", missing.Code, missing.Body.String())
	}
	assertBUG084Body(t, ctx, pool, "first editor body")

	var intents int
	if err := pool.QueryRow(ctx, `select count(*) from activity_event_outbox`).Scan(&intents); err != nil || intents != 0 {
		t.Fatalf("stale/missing base intents=%d err=%v", intents, err)
	}
	retry := invokeBUG084CommentPatch(t, ctx, server, map[string]any{
		"body": "explicit retry body", "baseUpdatedAt": conflict.Details.UpdatedAt,
	})
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	assertBUG084Body(t, ctx, pool, "explicit retry body")
	if err := pool.QueryRow(ctx, `select count(*) from activity_event_outbox where user_id=841 and object_public_id='b084c0001'`).Scan(&intents); err != nil || intents != 1 {
		t.Fatalf("edit retry intents=%d err=%v", intents, err)
	}
	var retryUpdatedAt time.Time
	if err := pool.QueryRow(ctx, `select updated_at from comments where id=840`).Scan(&retryUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if !retryUpdatedAt.After(conflict.Details.UpdatedAt) {
		t.Fatalf("retry version %s did not advance beyond %s", retryUpdatedAt, conflict.Details.UpdatedAt)
	}
}

func newBUG084Pool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify comment edit concurrency")
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

func invokeBUG084CommentPatch(t *testing.T, ctx context.Context, server *Server, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/comments/b084c0001", bytes.NewReader(body))
	request.SetPathValue("commentId", "b084c0001")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{
		Subject:         841,
		PermissionRules: []security.PermissionRule{{Code: "comment.edit.own", Allow: true}},
	}))
	response := httptest.NewRecorder()
	server.commentItem(response, request)
	return response
}

func assertBUG084Body(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want string) {
	t.Helper()
	var body string
	if err := pool.QueryRow(ctx, `select body from comments where id=840`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != want {
		t.Fatalf("comment body=%q want=%q", body, want)
	}
}

func TestCommentEditReturnsCommittedRevisionWhenDetailsUnavailable(t *testing.T) {
	f := newTEST038Fixture(t)
	comment := f.comment(t, f.otherEditor, f.modCode, "edit-receipt", "Original synthetic comment", "")
	// Disable only the decorative attachment read in this disposable child database.
	if _, err := f.db.Exec(f.ctx, "alter table comment_attachments rename to test_unavailable_comment_attachments"); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"body": "Edited synthetic comment", "baseUpdatedAt": comment.UpdatedAt}
	raw := f.require(t, f.otherEditor, http.MethodPatch, "/api/v1/comments/"+comment.ID, payload, http.StatusOK)
	var expectedVersion time.Time
	var activities int
	if err := f.db.QueryRow(f.ctx, `select updated_at,(select count(*) from activity_event_outbox
		where object_public_id=$1 and action_id=(select id from activity_actions where code='edit'))
		from comments where public_id=$1 and body=$2`, comment.ID, payload["body"]).Scan(&expectedVersion, &activities); err != nil {
		t.Fatal(err)
	}
	got := decodeTEST022Data[struct {
		ID                 string
		Body               string
		UpdatedAt          time.Time
		DetailsUnavailable bool
	}](t, raw)
	if !got.DetailsUnavailable || activities != 1 || got.ID != comment.ID || got.Body != payload["body"] || !got.UpdatedAt.Equal(expectedVersion) {
		t.Fatalf("edit receipt=%+v expected version=%v activity events=%d", got, expectedVersion, activities)
	}
}
