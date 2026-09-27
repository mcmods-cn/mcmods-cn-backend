package httpapi

import (
	"context"
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
	"mcmods-cn-backend/internal/systemactor"
)

type test020ProviderControl struct {
	mu      sync.RWMutex
	mode    string
	gate    <-chan struct{}
	entered chan<- string
}

func (control *test020ProviderControl) configure(mode string, gate <-chan struct{}, entered chan<- string) {
	control.mu.Lock()
	defer control.mu.Unlock()
	control.mode = mode
	control.gate = gate
	control.entered = entered
}

func (control *test020ProviderControl) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/search" {
		http.NotFound(w, r)
		return
	}
	projectType := "mod"
	if strings.Contains(r.URL.Query().Get("facets"), "project_type:plugin") {
		projectType = "plugin"
	}
	control.mu.RLock()
	mode, gate, entered := control.mode, control.gate, control.entered
	control.mu.RUnlock()
	if mode == "fail" || mode == "partial" && projectType == "plugin" {
		http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
		return
	}
	if mode == "block" {
		select {
		case entered <- projectType:
		default:
		}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"hits":[{"project_id":%q,"slug":%q,"title":%q,"description":"TEST020","downloads":1000}],"total_hits":1}`,
		"test020-"+projectType, "test020-"+projectType, "TEST020 "+projectType)
}

func TestTEST020SeedCrawlerWorkerRetriesDegradesHonorsConcurrencyAndOwnsLeasesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the seed crawler Worker lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newTEST020IsolatedDatabase(t, ctx)

	control := &test020ProviderControl{mode: "fail"}
	provider := httptest.NewServer(http.HandlerFunc(control.serveHTTP))
	defer provider.Close()
	serverConfig := config.Load()
	server := &Server{cfg: serverConfig, db: pool}
	importConfig := defaultModImportConfig()
	importConfig.RequestTimeoutSeconds = 5
	importConfig.Modrinth.BaseURL = provider.URL
	importConfig.Modrinth.Token = ""
	sealedImportConfig, err := server.sealSystemSetting(importConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)`, modImportConfigSettingKey, sealedImportConfig); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `with account as (
		insert into users(username,email,password_hash,email_verified,status)
		values($1,$2,'password-login-disabled',true,$3) returning id
	), permission as (
		insert into permissions(code,module,name,description,access_type)
		select code,'test020',code,'TEST020 automation fixture','write'
		from unnest(array['project.create','project.edit','project.no-review','content.no-review']) code
		returning id
	)
	insert into user_permissions(user_id,permission_id,allow,source,source_key)
	select account.id,permission.id,true,'system_seed','test020' from account cross join permission`,
		systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into seed_crawler_configs(id,enabled,project_types,batch_size,daily_limit,minimum_downloads,
		interval_seconds,max_concurrency,ai_daily_token_budget,auto_submit_review)
		values(true,false,array['mod','plugin'],1,20,0,3600,2,0,false)`); err != nil {
		t.Fatal(err)
	}

	worker := NewSeedCrawlerWorker(serverConfig, pool)
	worker.leaseDuration = 400 * time.Millisecond
	worker.heartbeatInterval = 75 * time.Millisecond

	var failedRunID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(dry_run) values(true) returning id`).Scan(&failedRunID); err != nil {
		t.Fatal(err)
	}
	processed, err := worker.processOne(ctx)
	if err != nil || !processed {
		t.Fatalf("all-provider failure processed=%t err=%v", processed, err)
	}
	var status, lastError string
	var attempts, providerSucceeded, providerFailed, candidates int
	var retryScheduled bool
	if err = pool.QueryRow(ctx, `select status,attempts,last_error,next_attempt_at>now(),
		coalesce((stats->>'providerSucceeded')::int,0),coalesce((stats->>'providerFailed')::int,0)
		from seed_crawler_runs where id=$1`, failedRunID).
		Scan(&status, &attempts, &lastError, &retryScheduled, &providerSucceeded, &providerFailed); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 || !retryScheduled || providerSucceeded != 0 || providerFailed != 2 || !strings.Contains(lastError, "all seed crawler provider categories failed") {
		t.Fatalf("all-provider state=%q attempts=%d retry=%t succeeded=%d failed=%d error=%q",
			status, attempts, retryScheduled, providerSucceeded, providerFailed, lastError)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from seed_crawler_candidates`).Scan(&candidates); err != nil || candidates != 0 {
		t.Fatalf("all-provider candidates=%d err=%v", candidates, err)
	}

	control.configure("success", nil, nil)
	if _, err = pool.Exec(ctx, `update seed_crawler_runs set next_attempt_at=now()-interval '1 second' where id=$1`, failedRunID); err != nil {
		t.Fatal(err)
	}
	processed, err = worker.processOne(ctx)
	if err != nil || !processed {
		t.Fatalf("provider retry processed=%t err=%v", processed, err)
	}
	var discovered, eligible, drafts, submitted int
	if err = pool.QueryRow(ctx, `select status,attempts,coalesce((stats->>'providerSucceeded')::int,0),
		coalesce((stats->>'providerFailed')::int,0),coalesce((stats->>'discovered')::int,0),
		coalesce((stats->>'eligible')::int,0),coalesce((stats->>'drafts')::int,0),coalesce((stats->>'submitted')::int,0)
		from seed_crawler_runs where id=$1`, failedRunID).
		Scan(&status, &attempts, &providerSucceeded, &providerFailed, &discovered, &eligible, &drafts, &submitted); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || attempts != 2 || providerSucceeded != 2 || providerFailed != 0 || discovered != 2 || eligible != 2 || drafts != 0 || submitted != 0 {
		t.Fatalf("retry state=%q attempts=%d provider=%d/%d discovered=%d eligible=%d drafts=%d submitted=%d",
			status, attempts, providerSucceeded, providerFailed, discovered, eligible, drafts, submitted)
	}
	var draftsPersisted, importsPersisted, projectsPersisted int
	if err = pool.QueryRow(ctx, `select
		(select count(*)::int from seed_crawler_candidates where status='candidate'),
		(select count(*)::int from user_drafts),
		(select count(*)::int from mod_metadata_import_jobs),
		(select count(*)::int from mods)+(select count(*)::int from simple_projects)`).
		Scan(&candidates, &draftsPersisted, &importsPersisted, &projectsPersisted); err != nil {
		t.Fatal(err)
	}
	if candidates != 2 || draftsPersisted != 0 || importsPersisted != 0 || projectsPersisted != 0 {
		t.Fatalf("dry-run facts candidates=%d drafts=%d imports=%d projects=%d", candidates, draftsPersisted, importsPersisted, projectsPersisted)
	}

	control.configure("partial", nil, nil)
	var partialRunID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(dry_run) values(true) returning id`).Scan(&partialRunID); err != nil {
		t.Fatal(err)
	}
	processed, err = worker.processOne(ctx)
	if err != nil || !processed {
		t.Fatalf("partial provider run processed=%t err=%v", processed, err)
	}
	if err = pool.QueryRow(ctx, `select status,coalesce((stats->>'providerSucceeded')::int,0),
		coalesce((stats->>'providerFailed')::int,0),coalesce((stats->>'failed')::int,0),last_error
		from seed_crawler_runs where id=$1`, partialRunID).
		Scan(&status, &providerSucceeded, &providerFailed, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || providerSucceeded != 1 || providerFailed != 1 || attempts != 1 || lastError != "" {
		t.Fatalf("partial state=%q provider=%d/%d failed=%d error=%q", status, providerSucceeded, providerFailed, attempts, lastError)
	}

	if _, err = pool.Exec(ctx, `update seed_crawler_configs set project_types=array['mod'],max_concurrency=2`); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	entered := make(chan string, 8)
	control.configure("block", gate, entered)
	concurrentRunIDs := make([]int64, 0, 3)
	for range 3 {
		var runID int64
		if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(dry_run) values(true) returning id`).Scan(&runID); err != nil {
			t.Fatal(err)
		}
		concurrentRunIDs = append(concurrentRunIDs, runID)
	}
	scheduleDone := make(chan error, 1)
	go func() { scheduleDone <- worker.schedule(ctx) }()
	waitTEST020ProviderEntries(t, entered, 2, 3*time.Second)
	select {
	case unexpected := <-entered:
		t.Fatalf("maxConcurrency=2 started a third provider request before capacity was released: %s", unexpected)
	case <-time.After(150 * time.Millisecond):
	}
	time.Sleep(550 * time.Millisecond)
	var running, pending, distinctOwners int
	var leasesFuture bool
	if err = pool.QueryRow(ctx, `select
		count(*) filter(where status='running')::int,
		count(*) filter(where status='pending')::int,
		count(distinct lease_owner) filter(where status='running')::int,
		coalesce(bool_and(lease_expires_at>now()) filter(where status='running'),false)
		from seed_crawler_runs where id=any($1)`, concurrentRunIDs).
		Scan(&running, &pending, &distinctOwners, &leasesFuture); err != nil {
		t.Fatal(err)
	}
	if running != 2 || pending != 1 || distinctOwners != 2 || !leasesFuture {
		t.Fatalf("concurrency state running=%d pending=%d owners=%d leasesFuture=%t", running, pending, distinctOwners, leasesFuture)
	}
	contenderCtx, contenderCancel := context.WithTimeout(ctx, time.Second)
	defer contenderCancel()
	contender := NewSeedCrawlerWorker(serverConfig, pool)
	contender.leaseDuration = worker.leaseDuration
	contender.heartbeatInterval = worker.heartbeatInterval
	processed, err = contender.processOne(contenderCtx)
	if err != nil || processed {
		t.Fatalf("global maxConcurrency contender processed=%t err=%v", processed, err)
	}
	if err = pool.QueryRow(ctx, `select attempts from seed_crawler_runs where id=$1`, concurrentRunIDs[2]).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("capacity-blocked run attempts=%d err=%v", attempts, err)
	}
	close(gate)
	if err = <-scheduleDone; err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from seed_crawler_runs where id=any($1) and status='completed'`, concurrentRunIDs).Scan(&attempts); err != nil || attempts != 3 {
		t.Fatalf("concurrent completed runs=%d err=%v", attempts, err)
	}

	if _, err = pool.Exec(ctx, `update seed_crawler_configs set max_concurrency=1`); err != nil {
		t.Fatal(err)
	}
	gate = make(chan struct{})
	entered = make(chan string, 8)
	control.configure("block", gate, entered)
	serialRunIDs := make([]int64, 0, 2)
	for range 2 {
		var runID int64
		if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(dry_run) values(true) returning id`).Scan(&runID); err != nil {
			t.Fatal(err)
		}
		serialRunIDs = append(serialRunIDs, runID)
	}
	scheduleDone = make(chan error, 1)
	go func() { scheduleDone <- worker.schedule(ctx) }()
	waitTEST020ProviderEntries(t, entered, 1, 3*time.Second)
	select {
	case unexpected := <-entered:
		t.Fatalf("maxConcurrency=1 started a second provider request: %s", unexpected)
	case <-time.After(200 * time.Millisecond):
	}
	if err = pool.QueryRow(ctx, `select count(*) filter(where status='running')::int,count(*) filter(where status='pending')::int
		from seed_crawler_runs where id=any($1)`, serialRunIDs).Scan(&running, &pending); err != nil {
		t.Fatal(err)
	}
	if running != 1 || pending != 1 {
		t.Fatalf("serial capacity running=%d pending=%d", running, pending)
	}
	close(gate)
	if err = <-scheduleDone; err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from seed_crawler_runs where id=any($1) and status='completed'`, serialRunIDs).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("serial completed runs=%d err=%v", attempts, err)
	}

	gate = make(chan struct{})
	entered = make(chan string, 8)
	control.configure("block", gate, entered)
	var stolenRunID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(dry_run) values(true) returning id`).Scan(&stolenRunID); err != nil {
		t.Fatal(err)
	}
	type processResult struct {
		processed bool
		err       error
	}
	processDone := make(chan processResult, 1)
	go func() {
		result, processErr := worker.processOne(ctx)
		processDone <- processResult{processed: result, err: processErr}
	}()
	waitTEST020ProviderEntries(t, entered, 1, 3*time.Second)
	if _, err = pool.Exec(ctx, `update seed_crawler_runs set lease_owner='stolen-owner',lease_expires_at=now()+interval '1 second' where id=$1`, stolenRunID); err != nil {
		t.Fatal(err)
	}
	result := <-processDone
	if !result.processed || result.err == nil || !strings.Contains(result.err.Error(), "lease ownership lost") {
		t.Fatalf("stolen lease processed=%t err=%v", result.processed, result.err)
	}
	if err = pool.QueryRow(ctx, `select status,lease_owner,attempts from seed_crawler_runs where id=$1`, stolenRunID).Scan(&status, &lastError, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "running" || lastError != "stolen-owner" || attempts != 1 {
		t.Fatalf("stolen lease was overwritten status=%q owner=%q attempts=%d", status, lastError, attempts)
	}
	control.configure("success", nil, nil)
	if _, err = pool.Exec(ctx, `update seed_crawler_runs set lease_expires_at=now()-interval '1 second' where id=$1`, stolenRunID); err != nil {
		t.Fatal(err)
	}
	if err = worker.schedule(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select status,attempts,lease_owner from seed_crawler_runs where id=$1`, stolenRunID).Scan(&status, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || attempts != 2 || lastError != "" {
		t.Fatalf("recovered stolen lease status=%q attempts=%d owner=%q", status, attempts, lastError)
	}
}

func newTEST020IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test020_seed_crawler_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test020_seed_crawler_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST020 database name %q", databaseName)
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
			t.Errorf("refuse unsafe TEST020 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST020 database: %v", dropErr)
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

func waitTEST020ProviderEntries(t *testing.T, entered <-chan string, count int, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for range count {
		select {
		case <-entered:
		case <-deadline.C:
			t.Fatalf("provider did not receive %d blocked requests before timeout", count)
		}
	}
}
