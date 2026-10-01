package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestLogRedactionIncludesQuotedSecretFields(t *testing.T) {
	for _, test := range []struct{ name, input, secret string }{
		{"JSON password", `{"password":"synthetic quoted password"}`, "synthetic quoted password"},
		{"JSON token", `{"access_token":"synthetic-token"}`, "synthetic-token"},
		{"JSON escaped token", `{"api_key":"synthetic\"quoted token"}`, `synthetic\"quoted token`},
		{"single quoted key and value", `'client_secret' = 'synthetic secret with spaces'`, "synthetic secret with spaces"},
		{"bare key quoted value", `password="synthetic quoted password"`, "quoted password"},
		{"existing bare token", `access_token=synthetic-token`, "synthetic-token"},
		{"OAuth query", `https://example.invalid/callback?code=synthetic-code&state=synthetic-state`, "synthetic-code"},
		{"signed query", `https://example.invalid/file?X-Amz-Signature=synthetic-signature`, "synthetic-signature"},
	} {
		t.Run(test.name, func(t *testing.T) {
			redacted, counts := redactLogText(test.input)
			if strings.Contains(redacted, test.secret) || !strings.Contains(redacted, "❄") || len(counts) == 0 {
				t.Fatal("synthetic secret was not fully redacted")
			}
		})
	}
	redacted, _ := redactLogText(`{"level":"info","message":"synthetic useful log line"}`)
	if !strings.Contains(redacted, "synthetic useful log line") {
		t.Fatal("ordinary log text was removed")
	}
}

func TestPasteLogSharePublishesRedactedQuotedFieldsIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{cfg: cfg, db: pool}
	var userID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('log-quote-audit','log-quote-audit@example.invalid','synthetic',true) returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	content := `{"password":"synthetic private phrase","api_key":"synthetic-private-key","message":"preserve this log context"}`
	encoded, err := json.Marshal(createPasteLogShareRequest{Title: "Synthetic redaction audit", Content: content, RetentionDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/log-shares/paste", bytes.NewReader(encoded))
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: userID}))
	response := httptest.NewRecorder()
	server.createPasteLogShare(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status: %d", response.Code)
	}
	var created struct {
		Data struct {
			PublicCode string `json:"publicCode"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.Data.PublicCode == "" {
		t.Fatal("missing created share code")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/log-shares/"+created.Data.PublicCode, nil)
	request.SetPathValue("code", created.Data.PublicCode)
	response = httptest.NewRecorder()
	server.publicLogShare(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "synthetic private phrase") || strings.Contains(response.Body.String(), "synthetic-private-key") || !strings.Contains(response.Body.String(), "preserve this log context") {
		t.Fatal("public share did not preserve context while omitting secrets")
	}
	// Recreate a version-1 stored entry in this dedicated test DB. Old shares
	// receive the new protection when displayed without rewriting stored text.
	if _, err = pool.Exec(ctx, `update log_shares set redaction_version=1 where public_code=$1`, created.Data.PublicCode); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update log_share_entries set sanitized_text=$2 where log_share_id=(select id from log_shares where public_code=$1)`, created.Data.PublicCode, content); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	server.publicLogShare(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "synthetic private phrase") || strings.Contains(response.Body.String(), "synthetic-private-key") {
		t.Fatal("historical share bypassed current display protection")
	}
	var stored string
	if err = pool.QueryRow(ctx, `select sanitized_text from log_share_entries where log_share_id=(select id from log_shares where public_code=$1)`, created.Data.PublicCode).Scan(&stored); err != nil || stored != content {
		t.Fatal("display protection rewrote historical stored content")
	}
}
