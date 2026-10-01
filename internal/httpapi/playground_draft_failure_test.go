package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestPlaygroundDraftReadFailureIsNotAnEmptyDraftIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{cfg: cfg, db: pool}
	var userID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('playground-read-audit','playground-read-audit@example.invalid','synthetic',true) returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/markdown-playground", nil)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: userID}))
	read := func() *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		server.getMarkdownPlaygroundDraft(response, request)
		return response
	}
	if response := read(); response.Code != http.StatusOK {
		t.Fatalf("missing draft must be empty, status %d", response.Code)
	}
	if _, err := pool.Exec(ctx, `insert into markdown_playground_drafts(user_id,content) values($1,'preserve synthetic draft')`, userID); err != nil {
		t.Fatal(err)
	}
	response := read()
	var body struct {
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK || body.Data.Content != "preserve synthetic draft" {
		t.Fatal("existing draft was not returned")
	}
	// Only this ownership-verified disposable database is altered. Restoring
	// the table checks that failed reads did not remove the existing draft.
	if _, err := pool.Exec(ctx, `alter table markdown_playground_drafts rename to audit_preserved_drafts`); err != nil {
		t.Fatal(err)
	}
	if response := read(); response.Code != http.StatusInternalServerError {
		t.Fatalf("database outage was falsely treated as empty draft: %d", response.Code)
	}
	if _, err := pool.Exec(ctx, `alter table audit_preserved_drafts rename to markdown_playground_drafts`); err != nil {
		t.Fatal(err)
	}
	var preserved string
	if err := pool.QueryRow(ctx, `select content from markdown_playground_drafts where user_id=$1`, userID).Scan(&preserved); err != nil || preserved != "preserve synthetic draft" {
		t.Fatal("failed read changed the existing draft")
	}
}
