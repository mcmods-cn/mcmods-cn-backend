package searchindex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestTEST030SearchRebuildRejectsDowngradesRecoversPoisonAndReportsPersistenceFailuresIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute the TEST030 search state-machine proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newTEST030IsolatedDatabase(t, ctx)
	if _, err := pool.Exec(ctx, `truncate search_index_queue`); err != nil {
		t.Fatal(err)
	}

	typesense := newTEST030Typesense(t)
	server := httptest.NewServer(typesense)
	t.Cleanup(server.Close)
	client := New(config.TypesenseConfig{
		Enabled: true, URL: server.URL, APIKey: "test030", CollectionPrefix: "test030", Timeout: 3 * time.Second,
	})
	workerA, workerB := NewWorker(pool, client), NewWorker(pool, client)
	rebuildResults := make(chan error, 2)
	go func() { rebuildResults <- workerA.rebuild(ctx) }()
	go func() { rebuildResults <- workerB.rebuild(ctx) }()
	for range 2 {
		if err := <-rebuildResults; err != nil {
			t.Fatalf("concurrent initial rebuild: %v", err)
		}
	}
	if got := typesense.totalCreatesForVersion(projectionSchemaVersion); got != len(searchRegistry) {
		t.Fatalf("same-version workers created %d collections, want %d", got, len(searchRegistry))
	}
	assertTEST030ProjectionState(t, ctx, pool, projectionSchemaVersion)

	var creatorID int64
	if err := pool.QueryRow(ctx, `insert into creators(kind,name,normalized_name,description_markdown,review_status)
		values('author','TEST030 Poison','test030 poison','search poison document','approved') returning id`).Scan(&creatorID); err != nil {
		t.Fatal(err)
	}
	poisonID := documentKey("creator", creatorID)
	oldCreatorAlias := typesense.aliasTarget("test030_creators")
	typesense.failNextImport(poisonID)
	versionFourWorker := NewWorker(pool, client)
	versionFourWorker.projectionVersion = projectionSchemaVersion + 1
	if err := versionFourWorker.rebuild(ctx); err == nil || !strings.Contains(err.Error(), "rejected a document") {
		t.Fatalf("poison rebuild error=%v", err)
	}
	if got := typesense.aliasTarget("test030_creators"); got != oldCreatorAlias {
		t.Fatalf("poison collection changed creator alias from %q to %q", oldCreatorAlias, got)
	}
	var progressStatus, progressError string
	if err := pool.QueryRow(ctx, `select status,last_error from search_index_rebuild_progress where collection_kind='creators'`).
		Scan(&progressStatus, &progressError); err != nil {
		t.Fatal(err)
	}
	if progressStatus != "failed" || !strings.Contains(progressError, "rejected a document") {
		t.Fatalf("poison progress status=%q error=%q", progressStatus, progressError)
	}
	if err := versionFourWorker.rebuild(ctx); err != nil {
		t.Fatalf("retry version-four rebuild: %v", err)
	}
	assertTEST030ProjectionState(t, ctx, pool, projectionSchemaVersion+1)
	for _, kind := range []string{"projects", "community", "resources", "servers"} {
		if got := typesense.createsForKindAndVersion(kind, projectionSchemaVersion+1); got != 1 {
			t.Errorf("version-four %s creates=%d want 1", kind, got)
		}
	}
	if got := typesense.createsForKindAndVersion("creators", projectionSchemaVersion+1); got != 2 {
		t.Errorf("version-four poison collection creates=%d want 2", got)
	}
	if !typesense.documentExists("test030_creators", poisonID) {
		t.Fatalf("recovered creator document %q is absent from the active alias", poisonID)
	}

	if _, err := pool.Exec(ctx, `truncate search_index_queue;
		insert into search_index_queue(document_type,document_id,operation) values('creator',930030,'delete')`); err != nil {
		t.Fatal(err)
	}
	oldWorker := NewWorker(pool, client)
	beforeCreates, beforeAliases, beforeDeletes := typesense.mutationCounts()
	if err := oldWorker.rebuild(ctx); !errors.Is(err, errSearchProjectionVersionSuperseded) {
		t.Fatalf("old worker rebuild error=%v", err)
	}
	if err := oldWorker.drain(ctx); !errors.Is(err, errSearchProjectionVersionSuperseded) {
		t.Fatalf("old worker drain error=%v", err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `select attempts from search_index_queue where document_type='creator' and document_id=930030`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	afterCreates, afterAliases, afterDeletes := typesense.mutationCounts()
	if attempts != 0 || beforeCreates != afterCreates || beforeAliases != afterAliases || beforeDeletes != afterDeletes {
		t.Fatalf("superseded worker mutated state: attempts=%d mutations %d/%d/%d -> %d/%d/%d",
			attempts, beforeCreates, beforeAliases, beforeDeletes, afterCreates, afterAliases, afterDeletes)
	}
	if err := versionFourWorker.drain(ctx); err != nil {
		t.Fatalf("current worker did not consume the retained queue item: %v", err)
	}

	if _, err := pool.Exec(ctx, `insert into search_index_queue(document_type,document_id,operation)
		values('creator',930031,'delete');
		create or replace function test030_reject_retry_state() returns trigger as $$
		begin
			if new.last_error<>'' then raise exception 'TEST030 retry state rejected'; end if;
			return new;
		end;
		$$ language plpgsql;
		create trigger test030_reject_retry_state before update on search_index_queue
		for each row execute function test030_reject_retry_state()`); err != nil {
		t.Fatal(err)
	}
	typesense.setFailDeletes(true)
	drainErr := versionFourWorker.drain(ctx)
	if drainErr == nil || !strings.Contains(drainErr.Error(), "Typesense") ||
		!strings.Contains(drainErr.Error(), "TEST030 retry state rejected") {
		t.Fatalf("delete/retry persistence failure was not fully observable: %v", drainErr)
	}
	var lastError string
	if err := pool.QueryRow(ctx, `select attempts,last_error from search_index_queue
		where document_type='creator' and document_id=930031`).Scan(&attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || lastError != "" {
		t.Fatalf("failed retry persistence attempts=%d lastError=%q", attempts, lastError)
	}
	if _, err := pool.Exec(ctx, `drop trigger test030_reject_retry_state on search_index_queue;
		drop function test030_reject_retry_state();
		update search_index_queue set available_at=now() where document_type='creator' and document_id=930031`); err != nil {
		t.Fatal(err)
	}
	typesense.setFailDeletes(false)
	if err := versionFourWorker.drain(ctx); err != nil {
		t.Fatalf("queue recovery after retry SQL repair: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `select count(*) from search_index_queue`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("search queue remaining=%d after recovery", remaining)
	}
}

func assertTEST030ProjectionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, version int) {
	t.Helper()
	rows, err := pool.Query(ctx, `select state.collection_kind,state.schema_version,state.collection_name,progress.status
		from search_index_state state join search_index_rebuild_progress progress using(collection_kind)
		order by state.collection_kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var kind, collection, status string
		var storedVersion int
		if err = rows.Scan(&kind, &storedVersion, &collection, &status); err != nil {
			t.Fatal(err)
		}
		if storedVersion != version || status != "complete" || !strings.Contains(collection, fmt.Sprintf("_%s_v%d_", kind, version)) {
			t.Errorf("projection %s version=%d collection=%q status=%q", kind, storedVersion, collection, status)
		}
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != len(searchRegistry) {
		t.Fatalf("projection state rows=%d want %d", count, len(searchRegistry))
	}
}

func newTEST030IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test030_search_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test030_search_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST030 database name %q", databaseName)
	}
	quotedName := pgx.Identifier{databaseName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create database "+quotedName); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if !namePattern.MatchString(databaseName) {
			t.Errorf("refuse unsafe TEST030 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST030 database: %v", dropErr)
		}
		adminPool.Close()
	})
	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.Database = databaseName
	scopedConfig.MaxConns, scopedConfig.MinConns = 8, 1
	pool, err = pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

type test030Typesense struct {
	t           *testing.T
	mutex       sync.Mutex
	collections map[string]map[string]map[string]any
	aliases     map[string]string
	creates     map[string]int
	createCount int
	aliasCount  int
	deleteCount int
	poisonID    string
	poisonOnce  bool
	failDeletes bool
}

func newTEST030Typesense(t *testing.T) *test030Typesense {
	return &test030Typesense{
		t: t, collections: make(map[string]map[string]map[string]any), aliases: make(map[string]string),
		creates: make(map[string]int),
	}
}

func (typesense *test030Typesense) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	typesense.mutex.Lock()
	defer typesense.mutex.Unlock()
	path := strings.Trim(request.URL.Path, "/")
	parts := strings.Split(path, "/")
	if request.Method == http.MethodGet && path == "health" {
		_, _ = response.Write([]byte(`{"ok":true}`))
		return
	}
	if len(parts) == 2 && parts[0] == "aliases" {
		alias := parts[1]
		switch request.Method {
		case http.MethodGet:
			target := typesense.aliases[alias]
			if target == "" {
				http.Error(response, "missing alias", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]string{"collection_name": target})
			return
		case http.MethodPut:
			var payload map[string]string
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				http.Error(response, err.Error(), http.StatusBadRequest)
				return
			}
			if _, exists := typesense.collections[payload["collection_name"]]; !exists {
				http.Error(response, "missing collection", http.StatusNotFound)
				return
			}
			typesense.aliases[alias] = payload["collection_name"]
			typesense.aliasCount++
			response.WriteHeader(http.StatusOK)
			return
		}
	}
	if request.Method == http.MethodPost && path == "collections" {
		var schema CollectionSchema
		if err := json.NewDecoder(request.Body).Decode(&schema); err != nil || schema.Name == "" {
			http.Error(response, "invalid collection", http.StatusBadRequest)
			return
		}
		typesense.collections[schema.Name] = make(map[string]map[string]any)
		typesense.creates[schema.Name]++
		typesense.createCount++
		response.WriteHeader(http.StatusCreated)
		return
	}
	if len(parts) >= 2 && parts[0] == "collections" {
		collection := parts[1]
		if target := typesense.aliases[collection]; target != "" {
			collection = target
		}
		if len(parts) == 2 {
			switch request.Method {
			case http.MethodGet:
				if _, exists := typesense.collections[collection]; !exists {
					http.Error(response, "missing collection", http.StatusNotFound)
					return
				}
				response.WriteHeader(http.StatusOK)
				return
			case http.MethodDelete:
				delete(typesense.collections, collection)
				response.WriteHeader(http.StatusOK)
				return
			}
		}
		if len(parts) == 4 && parts[2] == "documents" && parts[3] == "import" && request.Method == http.MethodPost {
			if _, exists := typesense.collections[collection]; !exists {
				http.Error(response, "missing collection", http.StatusNotFound)
				return
			}
			scanner := bufio.NewScanner(request.Body)
			for scanner.Scan() {
				var document map[string]any
				if err := json.Unmarshal(scanner.Bytes(), &document); err != nil {
					http.Error(response, err.Error(), http.StatusBadRequest)
					return
				}
				id, _ := document["id"].(string)
				if typesense.poisonOnce && id == typesense.poisonID {
					typesense.poisonOnce = false
					_, _ = response.Write([]byte(`{"success":false,"error":"TEST030 poison document"}` + "\n"))
					continue
				}
				typesense.collections[collection][id] = document
				_, _ = response.Write([]byte(`{"success":true}` + "\n"))
			}
			if err := scanner.Err(); err != nil {
				typesense.t.Errorf("scan TEST030 import: %v", err)
			}
			return
		}
		if len(parts) == 4 && parts[2] == "documents" && request.Method == http.MethodDelete {
			if typesense.failDeletes {
				http.Error(response, "TEST030 Typesense delete failure", http.StatusServiceUnavailable)
				return
			}
			delete(typesense.collections[collection], parts[3])
			typesense.deleteCount++
			response.WriteHeader(http.StatusOK)
			return
		}
	}
	typesense.t.Errorf("unexpected TEST030 Typesense request %s %s", request.Method, request.URL.Path)
	http.Error(response, "unexpected request", http.StatusBadRequest)
}

func (typesense *test030Typesense) failNextImport(documentID string) {
	typesense.mutex.Lock()
	defer typesense.mutex.Unlock()
	typesense.poisonID, typesense.poisonOnce = documentID, true
}

func (typesense *test030Typesense) setFailDeletes(fail bool) {
	typesense.mutex.Lock()
	defer typesense.mutex.Unlock()
	typesense.failDeletes = fail
}

func (typesense *test030Typesense) aliasTarget(alias string) string {
	typesense.mutex.Lock()
	defer typesense.mutex.Unlock()
	return typesense.aliases[alias]
}

func (typesense *test030Typesense) documentExists(alias, documentID string) bool {
	typesense.mutex.Lock()
	defer typesense.mutex.Unlock()
	_, exists := typesense.collections[typesense.aliases[alias]][documentID]
	return exists
}

func (typesense *test030Typesense) mutationCounts() (int, int, int) {
	typesense.mutex.Lock()
	defer typesense.mutex.Unlock()
	return typesense.createCount, typesense.aliasCount, typesense.deleteCount
}

func (typesense *test030Typesense) totalCreatesForVersion(version int) int {
	typesense.mutex.Lock()
	defer typesense.mutex.Unlock()
	total := 0
	needle := fmt.Sprintf("_v%d_", version)
	for collection, count := range typesense.creates {
		if strings.Contains(collection, needle) {
			total += count
		}
	}
	return total
}

func (typesense *test030Typesense) createsForKindAndVersion(kind string, version int) int {
	typesense.mutex.Lock()
	defer typesense.mutex.Unlock()
	total := 0
	needle := fmt.Sprintf("_%s_v%d_", kind, version)
	for collection, count := range typesense.creates {
		if strings.Contains(collection, needle) {
			total += count
		}
	}
	return total
}
