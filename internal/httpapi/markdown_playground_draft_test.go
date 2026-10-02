package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMarkdownDraftReadDistinguishesMissingRowFromDatabaseFailure(t *testing.T) {
	missingRecorder := httptest.NewRecorder()
	writeMarkdownPlaygroundDraftReadResult(missingRecorder, markdownPlaygroundDraftRecord{}, pgx.ErrNoRows)
	if missingRecorder.Code != http.StatusOK {
		t.Fatalf("missing status=%d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
	var missing apiResponse
	if err := json.Unmarshal(missingRecorder.Body.Bytes(), &missing); err != nil {
		t.Fatal(err)
	}
	missingData, ok := missing.Data.(map[string]any)
	if !ok || missingData["revision"] != float64(0) || missingData["content"] != "" {
		t.Fatalf("unexpected missing response: %#v", missing.Data)
	}

	failureRecorder := httptest.NewRecorder()
	writeMarkdownPlaygroundDraftReadResult(failureRecorder, markdownPlaygroundDraftRecord{}, errors.New("database unavailable"))
	if failureRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("failure status=%d body=%s", failureRecorder.Code, failureRecorder.Body.String())
	}
	var failure apiResponse
	if err := json.Unmarshal(failureRecorder.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Code != "MARKDOWN_DRAFT_READ_FAILED" || failure.Data != nil {
		t.Fatalf("unexpected failure response: %#v", failure)
	}
}

func TestMarkdownDraftRequestRequiresRevisionAndMonotonicSequence(t *testing.T) {
	valid := markdownPlaygroundDraftRequest{Content: "body", BaseRevision: 0, SaveSessionID: "editor-session", ClientSequence: 1}
	if err := validateMarkdownPlaygroundDraftRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	for _, request := range []markdownPlaygroundDraftRequest{
		{Content: "body", BaseRevision: -1, SaveSessionID: "editor-session", ClientSequence: 1},
		{Content: "body", BaseRevision: 0, SaveSessionID: "editor-session", ClientSequence: 0},
		{Content: "body", BaseRevision: 0, SaveSessionID: "bad session", ClientSequence: 1},
	} {
		if err := validateMarkdownPlaygroundDraftRequest(request); err == nil {
			t.Fatalf("invalid request accepted: %#v", request)
		}
	}
}

func TestMarkdownDraftSaveArrivalOrderAndCrossTabConflictIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify Markdown draft ordering")
	}
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if databaseURL == "" {
		t.Fatal("MCMODS_TEST_DATABASE_URL or DATABASE_URL is required")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	connection, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	ctx := context.Background()
	if _, err = connection.Exec(ctx, `create temp table markdown_playground_drafts(
		user_id bigint primary key, content text not null default '', revision bigint not null default 1,
		save_session_id text not null default '', client_sequence bigint not null default 0,
		updated_at timestamptz not null default now()) on commit preserve rows`); err != nil {
		t.Fatal(err)
	}

	seed := markdownPlaygroundDraftRequest{Content: "base", BaseRevision: 0, SaveSessionID: "seed-session", ClientSequence: 1}
	base, err := saveMarkdownPlaygroundDraftRecord(ctx, connection, 42, seed)
	if err != nil || base.Revision != 1 {
		t.Fatalf("seed: result=%#v err=%v", base, err)
	}

	// B reaches PostgreSQL first. A is the slower obsolete request and must fail.
	fastB := markdownPlaygroundDraftRequest{Content: "body B", BaseRevision: 1, SaveSessionID: "editor-session", ClientSequence: 2}
	if _, err = saveMarkdownPlaygroundDraftRecord(ctx, connection, 42, fastB); err != nil {
		t.Fatalf("fast B: %v", err)
	}
	slowA := markdownPlaygroundDraftRequest{Content: "body A", BaseRevision: 1, SaveSessionID: "editor-session", ClientSequence: 1}
	if _, err = saveMarkdownPlaygroundDraftRecord(ctx, connection, 42, slowA); !errors.Is(err, errMarkdownDraftConflict) {
		t.Fatalf("slow A should conflict, got %v", err)
	}
	assertMarkdownDraftContent(t, ctx, connection, 42, "body B", 2)

	// If A arrives first, the higher sequence B from the same editor may advance despite its old base.
	if _, err = connection.Exec(ctx, `truncate markdown_playground_drafts`); err != nil {
		t.Fatal(err)
	}
	if _, err = saveMarkdownPlaygroundDraftRecord(ctx, connection, 42, seed); err != nil {
		t.Fatal(err)
	}
	if _, err = saveMarkdownPlaygroundDraftRecord(ctx, connection, 42, markdownPlaygroundDraftRequest{Content: "body A", BaseRevision: 1, SaveSessionID: "editor-session", ClientSequence: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = saveMarkdownPlaygroundDraftRecord(ctx, connection, 42, fastB); err != nil {
		t.Fatalf("newer same-editor B should advance: %v", err)
	}
	assertMarkdownDraftContent(t, ctx, connection, 42, "body B", 3)

	otherTab := markdownPlaygroundDraftRequest{Content: "other tab", BaseRevision: 1, SaveSessionID: "other-session", ClientSequence: 1}
	if _, err = saveMarkdownPlaygroundDraftRecord(ctx, connection, 42, otherTab); !errors.Is(err, errMarkdownDraftConflict) {
		t.Fatalf("stale other tab should conflict, got %v", err)
	}
}

func assertMarkdownDraftContent(t *testing.T, ctx context.Context, connection *pgxpool.Conn, userID int64, wantContent string, wantRevision int64) {
	t.Helper()
	var content string
	var revision int64
	if err := connection.QueryRow(ctx, `select content,revision from markdown_playground_drafts where user_id=$1`, userID).Scan(&content, &revision); err != nil {
		t.Fatal(err)
	}
	if content != wantContent || revision != wantRevision {
		t.Fatalf("content=%q revision=%d, want %q/%d", content, revision, wantContent, wantRevision)
	}
}
