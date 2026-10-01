package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestInfrastructureMetricsDatabaseFailureDoesNotReportZeroBacklogIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	pool, err := pgxpool.New(context.Background(), config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	response := httptest.NewRecorder()
	server.infrastructureMetrics(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/infrastructure/metrics", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable metrics status=%d body=%s", response.Code, response.Body.String())
	}
}
