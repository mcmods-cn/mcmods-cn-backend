package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestAdminDashboardContractIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the administration dashboard against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := config.Load()
	pool, err := pgxpool.New(ctx, cfg.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cache := querycache.New(cfg.Redis)
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil).WithContext(ctx)
	response := httptest.NewRecorder()

	server.loadAdminDashboard(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Overview adminDashboardOverview `json:"overview"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	stats := envelope.Data.Overview.OSS
	if stats.ActiveFiles < 0 || stats.StoredBytes < 0 || stats.SourceBytes < 0 || stats.PendingScans < 0 || stats.QuarantinedFiles < 0 || stats.UploadsToday < 0 {
		t.Fatalf("dashboard returned invalid OSS statistics: %+v", stats)
	}
	if len(envelope.Data.Overview.Trend) != 30 {
		t.Fatalf("dashboard returned %d trend points, want 30", len(envelope.Data.Overview.Trend))
	}
	for _, point := range envelope.Data.Overview.Trend {
		if _, parseErr := time.Parse(time.DateOnly, point.Date); parseErr != nil {
			t.Fatalf("dashboard returned invalid trend date %q: %v", point.Date, parseErr)
		}
	}
}
