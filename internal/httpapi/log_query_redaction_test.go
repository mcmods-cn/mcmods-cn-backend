package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccessLogsDoNotPersistQueryCredentialsIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{cfg: cfg, db: pool}
	cases := []struct {
		name, path, query, logged string
	}{
		{"oauth callback", "/api/v1/auth/oauth/audit/callback", "code=synthetic-code&state=synthetic-state", "[redacted]"},
		{"arbitrary parameters", "/api/v1/log-audit-fixture", "api_key=synthetic-key&filter=synthetic-private-text&filter=second-value", "[redacted]"},
		{"malformed parameters", "/api/v1/log-audit-malformed", "synthetic-secret-in-name=value&code=%zz", "[redacted]"},
		{"empty query", "/api/v1/log-audit-empty", "", ""},
		{"launcher query", "/api/yggdrasil/log-audit-fixture", "accessToken=synthetic-token", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.URL.RawQuery = test.query
			observed := ""
			handler := server.logAccess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed = r.URL.RawQuery
				w.WriteHeader(http.StatusBadRequest)
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if observed != test.query || response.Code != http.StatusBadRequest {
				t.Fatal("logging changed the application's query or response")
			}
			var persisted string
			if err := pool.QueryRow(ctx, `select payload->>'query' from app_logs where category='api_access' and path=$1 order by id desc limit 1`, test.path).Scan(&persisted); err != nil {
				t.Fatal(err)
			}
			if persisted != test.logged {
				t.Fatal("access log did not omit query credentials")
			}
		})
	}
}
