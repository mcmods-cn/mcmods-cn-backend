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

	"mcmods-cn-backend/internal/security"
)

func TestCommentEditRejectsStaleAndMissingVersionIntegration(t *testing.T) {
	ctx, pool := newBUG084Pool(t)
	if _, err := pool.Exec(ctx, `
		create temporary table comments(
			id bigint primary key,public_id text not null unique,author_id bigint not null,body text not null,
			target_type text not null,target_id bigint not null,target_version_id bigint,root_id bigint,status text not null,
			deleted_at timestamptz,pinned_at timestamptz,pinned_by bigint,updated_at timestamptz not null
		);
		create temporary table mods(
			id bigint primary key,project_code text not null,primary_name text not null,
			review_status text not null,submitted_by bigint not null
		);
		create temporary table public_routes(
			public_id text not null,entity_type text not null,internal_id bigint not null,canonical_path text not null
		);
		create temporary table modpacks(id bigint primary key,public_id text not null);
		create temporary table simple_projects(id bigint primary key,project_type text not null,public_id text not null);
		create temporary table mod_content_versions(id bigint primary key,mod_id bigint not null);
		create temporary table community_posts(id bigint primary key,author_id bigint not null,kind text not null,accepted_comment_id bigint);
		insert into mods values(842,'b084mod01','BUG084 Mod','approved',841);
		insert into public_routes values('b084mod01','mod',842,'/mods/b084mod01');
		insert into comments(id,public_id,author_id,body,target_type,target_id,status,updated_at)
		values(840,'b084comment',841,'original body','mod',842,'published','2026-08-23T01:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	if _, err := server.resolveVisibleComment(ctx, "b084comment", security.Claims{Subject: 841}); err != nil {
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

	retry := invokeBUG084CommentPatch(t, ctx, server, map[string]any{
		"body": "explicit retry body", "baseUpdatedAt": conflict.Details.UpdatedAt,
	})
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	assertBUG084Body(t, ctx, pool, "explicit retry body")
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
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/comments/b084comment", bytes.NewReader(body))
	request.SetPathValue("commentId", "b084comment")
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
