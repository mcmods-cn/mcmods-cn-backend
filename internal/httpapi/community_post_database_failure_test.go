package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func TestCommunityDatabaseFailureIsNotReportedAsInvalidOrMissing(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://fixture:fixture@127.0.0.1:1/fixture?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	for _, entry := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"edit", func(w http.ResponseWriter, r *http.Request) { server.updateCommunityPost(w, r, "synthetic") }},
		{"request_translation", server.requestCommunityPostTranslation},
		{"translation_result", server.communityPostTranslationResult},
	} {
		t.Run(entry.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/community/posts", strings.NewReader(`{"targetLocale":"fr-FR"}`)).WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 1}))
			request.SetPathValue("id", "synthetic")
			request.SetPathValue("taskId", "synthetic")
			response := httptest.NewRecorder()
			entry.handler(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("database unavailable returned %d instead of 500: %s", response.Code, response.Body.String())
			}
		})
	}
}
