package searchindex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestSearchRebuildLeaseBlocksSecondWorkerQueueClaimIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify cross-instance search rebuild coordination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	schemaName := fmt.Sprintf("bug058_search_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, dropErr := adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated search schema: %v", dropErr)
		}
	}()

	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.MaxConns = 4
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create table search_index_queue (
		document_type text not null,document_id bigint not null,operation text not null,
		attempts integer not null default 0,available_at timestamptz not null default now(),
		last_error text not null default '',created_at timestamptz not null default now(),
		updated_at timestamptz not null default now(),primary key(document_type,document_id));
		create table search_index_state (
			collection_kind text primary key,schema_version integer not null,
			collection_name text not null,rebuilt_at timestamptz not null default now());
		create table creators (
			id bigint primary key,kind text not null,name text not null,description_markdown text not null,
			review_status text not null,created_by bigint,updated_at timestamptz not null);
		create table content_localizations (
			subject_type text not null,subject_id bigint not null,content_markdown text not null);
		create table creator_claims (creator_id bigint not null,status text not null,user_id bigint not null);
		insert into search_index_queue(document_type,document_id,operation) values('creator',58001,'delete')`); err != nil {
		t.Fatal(err)
	}

	var deleted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete || request.URL.Path != "/collections/bug058_creators/documents/creator_58001" {
			t.Errorf("unexpected Typesense request %s %s", request.Method, request.URL.Path)
			http.Error(response, "unexpected request", http.StatusBadRequest)
			return
		}
		deleted.Add(1)
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := New(config.TypesenseConfig{
		Enabled: true, URL: server.URL, APIKey: "test", CollectionPrefix: "bug058", Timeout: 2 * time.Second,
	})
	workerA := NewWorker(pool, client)
	workerB := NewWorker(pool, client)

	rebuildEntered := make(chan struct{})
	releaseRebuild := make(chan struct{})
	rebuildDone := make(chan error, 1)
	go func() {
		rebuildDone <- workerA.withSearchProjectionLease(ctx, false, func(*Worker) error {
			close(rebuildEntered)
			select {
			case <-releaseRebuild:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-rebuildEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("first worker did not acquire the rebuild lease")
	}

	drained := make(chan error, 1)
	go func() { drained <- workerB.drain(ctx) }()
	select {
	case drainErr := <-drained:
		t.Fatalf("second worker drained during the rebuild lease: %v", drainErr)
	case <-time.After(250 * time.Millisecond):
	}
	var attempts int
	if err = pool.QueryRow(ctx, `select attempts from search_index_queue where document_type='creator' and document_id=58001`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || deleted.Load() != 0 {
		t.Fatalf("queue changed under rebuild lease: attempts=%d Typesense deletes=%d", attempts, deleted.Load())
	}
	close(releaseRebuild)
	select {
	case err = <-rebuildDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first worker did not release the rebuild lease")
	}
	select {
	case err = <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second worker did not resume after the rebuild lease was released")
	}
	var remaining int
	if err = pool.QueryRow(ctx, `select count(*) from search_index_queue`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 || deleted.Load() != 1 {
		t.Fatalf("queue was not drained after rebuild: remaining=%d Typesense deletes=%d", remaining, deleted.Load())
	}
}
