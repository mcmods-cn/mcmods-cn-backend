package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestTEST033SchedulerRunsOnceAtStartupAndNeoForgeRetainsAllSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	runMinecraftVersionSyncScheduler(ctx, func(context.Context) (minecraftVersionConfig, error) {
		calls++
		cancel()
		return minecraftVersionConfig{}, nil
	})
	if calls != 1 {
		t.Fatalf("startup synchronizations=%d want 1", calls)
	}

	sources := []minecraftLoaderVersionSource{{code: "NeoForge", primaryURL: neoForgeMavenMetadataURL}}
	config := minecraftVersionConfig{
		Versions:     []minecraftVersionOption{{Code: "1.21"}, {Code: "1.20.1"}},
		Loaders:      []minecraftLoaderOption{{Code: "NeoForge", Versions: []string{"1.20.1"}}},
		LastSyncedAt: "2026-08-24T04:00:00Z",
	}
	allSources := []string{neoForgeMavenMetadataURL, neoForgeLegacyMetadataURL}
	applyMinecraftLoaderVersionResults(&config, sources, map[string]minecraftLoaderVersionResult{
		"neoforge": {source: sources[0], versions: []string{"1.21", "1.20.1"}, sourceURLs: allSources},
	})
	if len(config.LoaderSyncs) != 1 || config.LoaderSyncs[0].SourceURL != neoForgeMavenMetadataURL ||
		!reflect.DeepEqual(config.LoaderSyncs[0].SourceURLs, allSources) {
		t.Fatalf("NeoForge provenance=%#v want both modern and legacy sources", config.LoaderSyncs)
	}
	normalized, err := normalizeMinecraftVersionConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalized.LoaderSyncs[0].SourceURLs, allSources) {
		t.Fatalf("normalized NeoForge provenance=%#v", normalized.LoaderSyncs[0])
	}
}

func TestTEST033MinecraftVersionSyncUsesCrossInstanceLeasePersistsProvenanceAndFailsClosedIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute the TEST033 version-sync state-machine proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolA, databaseName := newTEST033IsolatedDatabase(t, ctx)
	poolB := newTEST033DatabasePool(t, ctx, databaseName)

	resetMinecraftSourceCacheForTest()
	t.Cleanup(resetMinecraftSourceCacheForTest)
	originalClient := minecraftVersionHTTPClient
	t.Cleanup(func() { minecraftVersionHTTPClient = originalClient })
	var remoteRequests atomic.Int64
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		remoteRequests.Add(1)
		return test033MinecraftSourceResponse(request), nil
	})}

	lease, err := acquireMinecraftVersionSyncLease(ctx, poolA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = syncMinecraftVersionCatalog(ctx, poolB); !errors.Is(err, errMinecraftVersionSyncInProgress) {
		t.Fatalf("second instance synchronization error=%v want in-progress", err)
	}
	response := httptest.NewRecorder()
	(&Server{db: poolB}).syncMinecraftVersions(response, httptest.NewRequest(http.MethodPost, "/api/v1/admin/minecraft-versions/sync", nil).WithContext(ctx))
	if response.Code != http.StatusConflict {
		t.Fatalf("concurrent sync HTTP status=%d body=%s want 409", response.Code, response.Body.String())
	}
	if remoteRequests.Load() != 0 {
		t.Fatalf("lease loser made %d upstream request(s), want zero", remoteRequests.Load())
	}
	if err = lease.Release(ctx); err != nil {
		t.Fatal(err)
	}

	config, err := syncMinecraftVersionCatalog(ctx, poolB)
	if err != nil {
		t.Fatal(err)
	}
	if remoteRequests.Load() != 7 {
		t.Fatalf("successful synchronization made %d upstream request(s), want 7 bounded catalogs", remoteRequests.Load())
	}
	neoForgeStatus := findTEST033LoaderSync(t, config.LoaderSyncs, "NeoForge")
	wantSources := []string{neoForgeMavenMetadataURL, neoForgeLegacyMetadataURL}
	if !reflect.DeepEqual(neoForgeStatus.SourceURLs, wantSources) || neoForgeStatus.SourceURL != wantSources[0] ||
		neoForgeStatus.Status != "synced" || neoForgeStatus.VersionCount != 3 {
		t.Fatalf("synchronized NeoForge status=%#v", neoForgeStatus)
	}
	var stored minecraftVersionConfig
	var raw []byte
	if err = poolA.QueryRow(ctx, `select value from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if got := findTEST033LoaderSync(t, stored.LoaderSyncs, "NeoForge").SourceURLs; !reflect.DeepEqual(got, wantSources) {
		t.Fatalf("persisted NeoForge sources=%#v want %#v", got, wantSources)
	}
	var neoForgeArtifacts int
	var neoForgeArtifactSources []string
	if err = poolA.QueryRow(ctx, `select count(*),array_agg(distinct source_url order by source_url)
		from minecraft_loader_artifact_versions where loader_type='neoforge'`).Scan(&neoForgeArtifacts, &neoForgeArtifactSources); err != nil {
		t.Fatal(err)
	}
	if neoForgeArtifacts != 3 || !reflect.DeepEqual(neoForgeArtifactSources, []string{neoForgeLegacyMetadataURL, neoForgeMavenMetadataURL}) {
		t.Fatalf("NeoForge artifact provenance=%d/%#v", neoForgeArtifacts, neoForgeArtifactSources)
	}
	var settingBefore, artifactsBefore string
	if err = poolA.QueryRow(ctx, `select value::text from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&settingBefore); err != nil {
		t.Fatal(err)
	}
	if err = poolA.QueryRow(ctx, `select coalesce(string_agg(catalog_hash||'|'||minecraft_version||'|'||loader_type||'|'||loader_version||'|'||source_url,';' order by loader_type,minecraft_version),'')
		from minecraft_loader_artifact_versions`).Scan(&artifactsBefore); err != nil {
		t.Fatal(err)
	}

	resetMinecraftSourceCacheForTest()
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("TEST033 upstream outage")
	})}
	if _, err = syncMinecraftVersionCatalog(ctx, poolA); err == nil || !strings.Contains(err.Error(), "TEST033 upstream outage") {
		t.Fatalf("network failure error=%v", err)
	}
	var settingAfter, artifactsAfter string
	if err = poolA.QueryRow(ctx, `select value::text from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&settingAfter); err != nil {
		t.Fatal(err)
	}
	if err = poolA.QueryRow(ctx, `select coalesce(string_agg(catalog_hash||'|'||minecraft_version||'|'||loader_type||'|'||loader_version||'|'||source_url,';' order by loader_type,minecraft_version),'')
		from minecraft_loader_artifact_versions`).Scan(&artifactsAfter); err != nil {
		t.Fatal(err)
	}
	if settingAfter != settingBefore || artifactsAfter != artifactsBefore {
		t.Fatal("failed synchronization changed the published catalog or artifact snapshot")
	}
}

func findTEST033LoaderSync(t *testing.T, statuses []minecraftLoaderSyncStatus, code string) minecraftLoaderSyncStatus {
	t.Helper()
	for _, status := range statuses {
		if strings.EqualFold(status.Code, code) {
			return status
		}
	}
	t.Fatalf("loader sync %q is absent from %#v", code, statuses)
	return minecraftLoaderSyncStatus{}
}

func test033MinecraftSourceResponse(request *http.Request) *http.Response {
	body, status := "", http.StatusOK
	switch request.URL.String() {
	case mojangVersionManifestURL:
		body = `{"latest":{"release":"26.2","snapshot":"26.3-snapshot-1"},"versions":[{"id":"26.2","type":"release"},{"id":"1.21","type":"release"},{"id":"1.20.1","type":"release"}]}`
	case forgeMavenMetadataURL:
		body = `<metadata><versioning><versions><version>1.20.1-47.4.10</version></versions></versioning></metadata>`
	case neoForgeMavenMetadataURL:
		body = `<metadata><versioning><versions><version>21.0.168</version><version>26.2.0.61</version></versions></versioning></metadata>`
	case neoForgeLegacyMetadataURL:
		body = `<metadata><versioning><versions><version>1.20.1-47.1.106</version></versions></versioning></metadata>`
	case fabricGameVersionsURL:
		body = `[{"version":"26.2","stable":true},{"version":"1.21","stable":true},{"version":"1.20.1","stable":true}]`
	case liteLoaderVersionsURL:
		body = `{"versions":{"1.20.1":{}}}`
	case fabricLoaderCatalogURL:
		body = `[{"version":"0.16.14","stable":true}]`
	default:
		body, status = `{"error":"unexpected TEST033 URL"}`, http.StatusNotFound
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func newTEST033IsolatedDatabase(t *testing.T, ctx context.Context) (*pgxpool.Pool, string) {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test033_versions_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test033_versions_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST033 database name %q", databaseName)
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
			t.Errorf("refuse unsafe TEST033 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST033 database: %v", dropErr)
		}
		adminPool.Close()
	})
	pool = newTEST033DatabasePool(t, ctx, databaseName)
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool, databaseName
}

func newTEST033DatabasePool(t *testing.T, ctx context.Context, databaseName string) *pgxpool.Pool {
	t.Helper()
	scopedConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.Database = databaseName
	scopedConfig.MaxConns, scopedConfig.MinConns = 8, 1
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
