package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResolveOSSObjectAccessLegacyESAModeUsesPresignedURL(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		Enabled:               true,
		Region:                "cn-hangzhou",
		Endpoint:              "https://oss-cn-hangzhou.aliyuncs.com",
		PublicEndpoint:        "https://oss.example.test/assets",
		Bucket:                "mcmods-test",
		AccessKeyID:           "test-access-key",
		AccessKeySecret:       "test-access-secret",
		DownloadURLMode:       "esa_private_origin",
		DownloadURLTTLMinutes: 12,
	}
	access, err := server.resolveOSSObjectAccessWithConfig(context.Background(), cfg, "project/icon.png", ossObjectAccessOptions{
		ContentDisposition: `attachment; filename="icon.png"`,
	})
	if err != nil {
		t.Fatalf("resolve legacy ESA object access: %v", err)
	}
	if access.Mode != ossDownloadModePresigned {
		t.Fatalf("unexpected mode %q", access.Mode)
	}
	if !strings.Contains(strings.ToLower(access.URL), "x-oss-signature") ||
		!strings.Contains(access.URL, "response-content-disposition=") ||
		strings.HasPrefix(access.URL, "https://oss.example.test/") {
		t.Fatalf("unexpected signed URL %q", access.URL)
	}
	if remaining := time.Until(access.ExpiresAt); remaining < 11*time.Minute || remaining > 13*time.Minute {
		t.Fatalf("unexpected expiry %s", remaining)
	}
}

func TestResolveStoredOSSObjectAccessLeavesExternalURLUnchanged(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		PublicEndpoint:  "https://oss.example.test",
		DownloadURLMode: "esa_private_origin",
	}
	const external = "https://cdn.example.test/avatar.png"
	resolved, err := server.resolveStoredOSSObjectAccessURLWithConfig(context.Background(), cfg, external)
	if err != nil {
		t.Fatalf("resolve external URL: %v", err)
	}
	if resolved != external {
		t.Fatalf("external URL changed from %q to %q", external, resolved)
	}
}

func TestResolveOSSObjectAccessPresigned(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		Enabled:               true,
		Region:                "cn-hangzhou",
		Endpoint:              "https://oss-cn-hangzhou.aliyuncs.com",
		Bucket:                "mcmods-test",
		AccessKeyID:           "test-access-key",
		AccessKeySecret:       "test-access-secret",
		DownloadURLMode:       ossDownloadModePresigned,
		DownloadURLTTLMinutes: 5,
	}
	access, err := server.resolveOSSObjectAccessWithConfig(context.Background(), cfg, "project/icon.png", ossObjectAccessOptions{})
	if err != nil {
		t.Fatalf("resolve presigned object access: %v", err)
	}
	if access.Mode != ossDownloadModePresigned {
		t.Fatalf("unexpected mode %q", access.Mode)
	}
	if !strings.Contains(strings.ToLower(access.URL), "x-oss-signature") {
		t.Fatalf("URL is not OSS-signed: %q", access.URL)
	}
}

func TestPublicMediaReadFailureIsReported(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), "postgres://unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	routes := http.NewServeMux()
	routes.HandleFunc("GET /api/v1/mods/{siteId}/icon", server.publicModIcon)
	routes.HandleFunc("GET /api/v1/modpacks/{siteId}/icon", server.modpackIcon)
	routes.HandleFunc("GET /api/v1/content-projects/{projectType}/{siteId}/icon", server.simpleProjectIcon)
	routes.HandleFunc("GET /api/v1/oss/files/{publicId}/content", server.publicInlineOSSFile)
	for _, path := range []string{
		"/api/v1/mods/sample/icon",
		"/api/v1/modpacks/sample/icon",
		"/api/v1/content-projects/map/sample/icon",
		"/api/v1/oss/files/sample/content",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusInternalServerError || response.Header().Get("Location") != "" {
				t.Fatalf("media lookup with unavailable storage returned %d, redirect %q", response.Code, response.Header().Get("Location"))
			}
		})
	}
}

func TestProjectFileReadsReportUnavailableStorage(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), "postgres://unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	s := &Server{db: pool}
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		invalid int
	}{
		{"list", s.projectFiles, http.StatusBadRequest},
		{"remove", s.projectFile, http.StatusNotFound},
		{"upload", s.createProjectFileUpload, http.StatusNotFound},
		{"complete", s.completeProjectFileUpload, http.StatusNotFound},
		{"download", s.downloadProjectFile, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.SetPathValue("projectType", "mod")
			for id, want := range map[string]int{"short": tc.invalid, "test00001": http.StatusServiceUnavailable} {
				request.SetPathValue("projectId", id)
				response := httptest.NewRecorder()
				tc.handler(response, request)
				if response.Code != want {
					t.Errorf("project %q got %d, want %d", id, response.Code, want)
				}
			}
		})
	}
}
