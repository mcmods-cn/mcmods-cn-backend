package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestAccessLogIngestionDoesNotExtendRequestsDuringDatabaseLockIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 only against an authorized development/test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 4
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	lockConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lockTransaction, err := lockConnection.Begin(ctx)
	if err != nil {
		lockConnection.Release()
		t.Fatal(err)
	}
	if _, err = lockTransaction.Exec(ctx, `lock table app_logs in access exclusive mode`); err != nil {
		_ = lockTransaction.Rollback(ctx)
		lockConnection.Release()
		t.Fatal(err)
	}

	ingestor := newAccessLogIngestor(pool, accessLogIngestorOptions{
		Capacity: 128, BatchSize: 16, FlushInterval: time.Hour, WriteTimeout: 5 * time.Second,
	})
	server := &Server{db: pool, accessLogs: ingestor}
	handler := server.logAccess(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]bool{"failed": true})
	}))
	path := fmt.Sprintf("/api/v1/perf032/%d", time.Now().UnixNano())
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `delete from app_logs where path=$1`, path)
	}()

	started := time.Now()
	for index := 0; index < 64; index++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("response %d status=%d", index, response.Code)
		}
	}
	requestElapsed := time.Since(started)
	if requestElapsed > 250*time.Millisecond {
		t.Fatalf("64 requests took %s while app_logs was locked", requestElapsed)
	}
	if metrics := ingestor.Metrics(); metrics.Enqueued != 64 || metrics.OverflowDropped != 0 {
		t.Fatalf("pre-release metrics=%+v", metrics)
	}

	if err = lockTransaction.Rollback(ctx); err != nil {
		lockConnection.Release()
		t.Fatal(err)
	}
	lockConnection.Release()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err = ingestor.Close(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	metrics := ingestor.Metrics()
	if metrics.Flushed != 64 || metrics.Batches != 4 || metrics.WriteFailures != 0 {
		t.Fatalf("post-flush metrics=%+v", metrics)
	}
	var stored int
	if err = pool.QueryRow(ctx, `select count(*) from app_logs where path=$1`, path).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 64 {
		t.Fatalf("stored access logs=%d want 64", stored)
	}
	t.Logf("64 locked-table requests returned in %s and flushed in %d batched statements", requestElapsed, metrics.Batches)
}
