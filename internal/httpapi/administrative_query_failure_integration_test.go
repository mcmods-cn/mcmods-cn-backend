package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdministrativeQueryFailureIsNotEmptySuccessIntegration(t *testing.T) {
	_, pool, _ := isolatedAITestDatabase(t)
	pool.Close() // A real pgx pool failure, never a mocked query result.
	server := &Server{db: pool}
	cases := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"system logs", "/?category=system", server.adminLogs},
		{"permission logs", "/?category=permission_change", server.adminLogs},
		{"login logs", "/?category=login_security", server.adminLogs},
		{"file logs", "/?category=file_upload", server.adminLogs},
		{"AI task list", "/", server.adminAITasks},
		{"AI task detail", "/", server.adminAITask},
		{"AI accounting", "/", server.adminAIStats},
		{"OSS upload logs", "/", server.ossUploadLogs},
		{"OSS scan logs", "/", server.ossScanLogs},
		{"OSS download statistics", "/", server.ossDownloadStats},
		{"bot rules", "/", server.adminAntiAbuseBotRules},
		{"restrictions", "/", server.adminAntiAbuseRestrictions},
		{"project automation overview", "/", server.adminProjectAutomationOverview},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			request.SetPathValue("id", "synthetic")
			response := httptest.NewRecorder()
			tc.handler(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("failed database query returned %d, wanted 500", response.Code)
			}
		})
	}
}

func TestSimpleQueryCannotReturnPartialRowsAfterPostgreSQLErrorIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	server := &Server{db: pool}
	items, err := server.querySimpleRowsWithContext(ctx, `select 7::bigint as id, '{"label":"synthetic"}'::jsonb as metadata`)
	if err != nil || len(items) != 1 || items[0]["id"] != int64(7) {
		t.Fatalf("normal query did not preserve data: rows=%d err=%v", len(items), err)
	}
	items, err = server.querySimpleRowsWithContext(ctx, `select 7 as id where false`)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("valid empty query must remain a JSON array: rows=%d err=%v", len(items), err)
	}
	// PostgreSQL emits the first row, then an error at the second. Returning the
	// prefix would present incomplete governance/accounting data as success.
	items, err = server.querySimpleRowsWithContext(ctx, `select 1/(2-value) as result from generate_series(1,2) value`)
	if err == nil || items != nil {
		t.Fatalf("failed stream returned partial success: rows=%d err=%v", len(items), err)
	}
}
