package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

type favoriteExportFixtureTransport func(*http.Request) (*http.Response, error)

func (transport favoriteExportFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return transport(r)
}

func TestFavoriteExportQuotaConcurrencyAndLateLeaseIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var userID, modID, routeID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('favorite_quota_fixture','favorite-fixture@example.test','fixture') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values('exportfix','favorite-fixture','synthetic export','approved') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at) values($1,'modrinth','SyntheticProject','https://modrinth.com/mod/SyntheticProject',now())`, routeID); err != nil {
		t.Fatal(err)
	}
	collections := make([]string, 9)
	var firstCollectionID int64
	for index := range collections {
		var collectionID int64
		if err := pool.QueryRow(ctx, `insert into favorite_collections(user_id,name) values($1,$2) returning id,public_id`, userID, fmt.Sprintf("synthetic export %d", index)).Scan(&collectionID, &collections[index]); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			firstCollectionID = collectionID
		}
		if _, err := pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id) values($1,'mod',$2)`, collectionID, modID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `insert into favorite_modpack_export_tasks(owner_user_id,collection_id,pack_name,pack_version_id,minecraft_version,loader_type)
		values($1,$2,'synthetic existing','fixture-1','1.20.1','fabric')`, userID, firstCollectionID); err != nil {
		t.Fatal(err)
	}
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	files := []providerProjectFile{{FileName: "synthetic.jar", GameVersions: []string{"1.20.1"}, Loaders: []string{"fabric"}, SizeBytes: 100, SHA1: strings.Repeat("a", 40), SHA512: strings.Repeat("b", 128), DirectURL: "https://cdn.modrinth.com/data/SyntheticProject/versions/SyntheticVersion/synthetic.jar", ProviderProjectID: "SyntheticProject", ProviderVersionID: "SyntheticVersion"}}
	raw, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	cache.Set(ctx, "project-files:provider:modrinth:syntheticproject", raw, time.Minute)
	previous := minecraftVersionHTTPClient
	t.Cleanup(func() { minecraftVersionHTTPClient = previous })
	minecraftVersionHTTPClient = &http.Client{Transport: favoriteExportFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != fabricLoaderVersionsURL+"1.20.1" {
			return nil, errors.New("unexpected external request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"loader":{"version":"0.16.0","stable":true}}]`))}, nil
	})}
	cfg.NATS.OutboxEnabled = true
	cfg.FavoriteExport.MaxActivePerUser = 2
	server := &Server{db: pool, cfg: cfg, cache: cache}
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 8)
	var callers sync.WaitGroup
	for _, collection := range collections[1:] {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			r := httptest.NewRequest(http.MethodPost, "/exports", strings.NewReader(`{"minecraftVersion":"1.20.1","loader":"fabric"}`))
			r.SetPathValue("id", collection)
			r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
			response := httptest.NewRecorder()
			server.createFavoriteModpackExport(response, r)
			responses <- response
		}()
	}
	close(start)
	callers.Wait()
	close(responses)
	accepted, limited := 0, 0
	for response := range responses {
		switch response.Code {
		case http.StatusAccepted:
			accepted++
		case http.StatusTooManyRequests:
			if !strings.Contains(response.Body.String(), "MODPACK_EXPORT_CONCURRENCY_LIMIT") {
				t.Fatalf("wrong rejection: %s", response.Body.String())
			}
			limited++
		default:
			t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
		}
	}
	var active, outbox int
	if err = pool.QueryRow(ctx, `select count(*) from favorite_modpack_export_tasks where owner_user_id=$1 and status in('pending','processing')`, userID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_type='favorite_modpack_export'`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || limited != 7 || active != 2 || outbox != 1 {
		t.Fatalf("non-atomic task quota: accepted=%d limited=%d active=%d outbox=%d", accepted, limited, active, outbox)
	}
	var taskID string
	if err = pool.QueryRow(ctx, `update favorite_modpack_export_tasks set status='processing',attempt_count=3,lease_token='new-attempt',lease_expires_at=now()+interval '5 minutes' where collection_id=$1 returning public_id`, firstCollectionID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	worker := &FavoriteModpackExportWorker{server: server}
	_ = worker.fail(ctx, taskID, "old-attempt", "OLD_FAILURE", errors.New("synthetic old failure"))
	var status, token string
	if err = pool.QueryRow(ctx, `select status,lease_token from favorite_modpack_export_tasks where public_id=$1`, taskID).Scan(&status, &token); err != nil || status != "processing" || token != "new-attempt" {
		t.Fatalf("late failure changed new task: status=%s err=%v", status, err)
	}
	var notifications int
	if err = pool.QueryRow(ctx, `select count(*) from notifications where recipient_id=$1`, userID).Scan(&notifications); err != nil || notifications != 0 {
		t.Fatalf("late failure sent misleading notification: count=%d err=%v", notifications, err)
	}
	server.cfg.NATS.Tasks = []config.NATSTaskConfig{{Code: favoriteModpackExportTaskCode, Enabled: false}}
	if err = worker.process(ctx, taskID); !errors.Is(err, queue.ErrTaskDisabled) {
		t.Fatalf("disabled scanner/task executed: %v", err)
	}
	worker.processPending(ctx)
	if err = pool.QueryRow(ctx, `select count(*) from favorite_modpack_export_tasks where status='pending' and attempt_count=0`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("disabled scan claimed queued backlog: count=%d err=%v", active, err)
	}
	// The actual preview also completes with one pool connection: rows must
	// close before the cached provider/config/dependency reads begin.
	server.cfg.NATS.Tasks = nil
	poolConfig := pool.Config().Copy()
	poolConfig.MaxConns, poolConfig.MinConns = 1, 0
	small, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	server.db = small
	previewCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if preview, err := server.buildFavoriteModpackExportPreview(previewCtx, userID, collections[1], favoriteModpackExportRequest{MinecraftVersion: "1.20.1", Loader: "fabric"}); err != nil || preview.ExportedModCount != 1 {
		t.Fatalf("one-connection preview failed: count=%d err=%v", preview.ExportedModCount, err)
	}
}
