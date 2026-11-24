package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

type providerErrorBody struct {
	*strings.Reader
	closed bool
}

func (body *providerErrorBody) Close() error {
	body.closed = true
	return nil
}

func TestProviderErrorStatusClosesUnusedBody(t *testing.T) {
	text := "Temporarily unavailable"
	body := &providerErrorBody{Reader: strings.NewReader(text)}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body}, nil
	})}
	_, err := getProviderBytes(context.Background(), client, "https://provider.example.invalid/file", nil)
	var status *providerHTTPStatusError
	if !errors.As(err, &status) || status.statusCode != http.StatusServiceUnavailable || !body.closed || body.Len() != len(text) {
		t.Fatalf("status=%v closed=%t consumed=%d", err, body.closed, len(text)-body.Len())
	}
}

func BenchmarkProviderErrorStatus(b *testing.B) {
	maintenancePage := strings.Repeat("Upstream temporarily unavailable.\n", 2048)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(maintenancePage))}, nil
	})}
	b.ReportAllocs()
	for b.Loop() {
		_, err := getProviderBytes(context.Background(), client, "https://provider.example.invalid/file", nil)
		if err == nil {
			b.Fatal("provider 503 was treated as success")
		}
	}
}

func TestMetadataImportWorkerStopIsRecoverableIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	owner := newCurrentWriteTestActor(t, ctx, pool, "import-recovery", "project.create")
	var jobID string
	err := pool.QueryRow(ctx, `insert into mod_metadata_import_jobs(user_id,project_type,provider,source_url,status,progress)
		values($1,'mod','modrinth','https://modrinth.com/mod/stopping','queued',0) returning public_id`, owner.Subject).Scan(&jobID)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: pool, cfg: config.Load()}
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	err = s.runModMetadataImportWithProviderClientFactory(workerCtx, jobID, func(time.Duration, string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			stopWorker()
			return nil, r.Context().Err()
		})}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("worker shutdown returned %v, want context cancellation", err)
	}
	if _, err := pool.Exec(ctx, `update mod_metadata_import_jobs set updated_at=now()-interval '6 minutes' where public_id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	requeued, err := recoverModMetadataImportOutboxTx(ctx, tx, 5*time.Minute, 20)
	if err != nil || requeued != 1 {
		t.Fatalf("recover stopped worker: jobs=%d error=%v", requeued, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var events int
	err = pool.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_id=$1 and event_type='mod.metadata.import.recovered' and payload->>'jobId'=$1`, jobID).Scan(&events)
	if err != nil || events != 1 {
		t.Fatalf("recovery events=%d err=%v", events, err)
	}
	calls := 0
	err = s.runModMetadataImportWithProviderClientFactory(ctx, jobID, func(time.Duration, string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody}, nil
		})}, nil
	})
	var status, message string
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select status,error from mod_metadata_import_jobs where public_id=$1`, jobID).Scan(&status, &message); err != nil ||
		calls != 1 || status != "failed" || !strings.Contains(message, "HTTP 503") {
		t.Fatalf("provider failure status=%s calls=%d message=%q err=%v", status, calls, message, err)
	}
}
