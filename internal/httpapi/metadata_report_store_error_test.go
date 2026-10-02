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

func TestOCT02MetadataAndEvidenceStoreErrorsAreNotMissingResources(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://test:test@127.0.0.1:1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	for name, handler := range map[string]http.HandlerFunc{"mod": server.getModMetadataImport, "plugin": server.getSimpleProjectMetadataImport, "evidence": server.reportEvidenceAccess, "defaults": server.getPermissionDefaults, "comparison": server.comparePermissions} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/test", strings.NewReader(`{"left":{"kind":"me"},"right":{"kind":"me"}}`))
			req.SetPathValue("jobId", "job000001")
			req.SetPathValue("projectType", "plugin")
			req.SetPathValue("id", "file00001")
			req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, security.Claims{Subject: 1}))
			response := httptest.NewRecorder()
			handler(response, req)
			want := http.StatusServiceUnavailable
			if name == "evidence" {
				want = http.StatusInternalServerError
			}
			if response.Code != want {
				t.Fatalf("store error reported as %d want %d", response.Code, want)
			}
			if strings.Contains(response.Body.String(), "closed") || strings.Contains(response.Body.String(), "postgres") {
				t.Fatal("driver error exposed")
			}
		})
	}
}
