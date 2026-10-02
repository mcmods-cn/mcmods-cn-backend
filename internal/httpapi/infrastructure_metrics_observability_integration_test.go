package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestInfrastructureDurableMetricsFailInsteadOfReportingZeroIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify durable infrastructure metrics")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temp table nats_outbox(published_at timestamptz,status text not null,created_at timestamptz not null);
		create temp table dead_letter_events(replayed_at timestamptz);
		create temp table oss_object_deletion_outbox(status text not null);
		create temp table oss_rehome_jobs(status text not null);
		create temp table oss_multipart_sessions(status text not null);
		insert into nats_outbox values(null,'pending',now()-interval '2 minutes'),(now(),'published',now()-interval '1 day');
		insert into dead_letter_events values(null),(now());
		insert into oss_object_deletion_outbox values('dead'),('completed');
		insert into oss_rehome_jobs values('dead'),('dead'),('completed');
		insert into oss_multipart_sessions values('dead'),('dead'),('dead'),('completed')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	metrics, err := server.loadInfrastructureDurableMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.PendingOutbox != 1 || metrics.OldestSeconds < 60 || metrics.DeadLetters != 1 ||
		metrics.OSSDeletionDead != 1 || metrics.OSSRehomeDead != 2 || metrics.OSSMultipartDead != 3 {
		t.Fatalf("unexpected durable metrics: %#v", metrics)
	}

	if _, err = pool.Exec(ctx, `alter table nats_outbox rename column published_at to arch015_broken_published_at`); err != nil {
		t.Fatal(err)
	}
	if metrics, err = server.loadInfrastructureDurableMetrics(ctx); err == nil || metrics != (infrastructureDurableMetrics{}) {
		t.Fatalf("failed durable metrics=(%#v,%v); want zero value/error", metrics, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/infrastructure/metrics", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	server.infrastructureMetrics(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("durable metric database failure status=%d body=%s; want 500", response.Code, response.Body.String())
	}
}
