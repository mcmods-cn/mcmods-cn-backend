package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02ProfileAndEditorLookupOutagesAreRetryable(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://test:test@127.0.0.1:1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	for name, handler := range map[string]http.HandlerFunc{"avatar": server.updateUserProfileSettings, "editor_revoke": server.revokeProjectEditorAssignment, "log_batch": server.createFileLogShares} {
		t.Run(name, func(t *testing.T) {
			body := `{"avatarFileId":"file00001"}`
			want := http.StatusServiceUnavailable
			if name == "log_batch" {
				body, want = `{"fileIds":["file00001"]}`, http.StatusMultiStatus
			}
			r := httptest.NewRequest(http.MethodPut, "/api/v1/test", strings.NewReader(body))
			r.SetPathValue("projectType", "mod")
			r.SetPathValue("projectId", "mod000001")
			r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, security.Claims{Subject: 1, PermissionRules: []security.PermissionRule{{Code: "user.avatar.update", Allow: true}}}))
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != want {
				t.Fatalf("lookup outage status=%d want%d", w.Code, want)
			}
			if strings.Contains(w.Body.String(), "closed") || strings.Contains(w.Body.String(), "postgres") {
				t.Fatal("internal connection details leaked")
			}
		})
	}
}
