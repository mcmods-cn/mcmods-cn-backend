package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMinecraftVersionManualUpdateConflictsWithInFlightSyncIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	syncPool, _ := newTEST033IsolatedDatabase(t, ctx)
	adminConfig := syncPool.Config()
	adminConfig.MaxConns, adminConfig.MinConns = 1, 1
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	server := &Server{db: adminPool}

	update := func(name, version string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/minecraft-versions", strings.NewReader(
			`{"versions":[{"code":"`+version+`","type":"release"}],"commonVersions":["`+version+`"],"loaders":[{"code":"Fabric","name":"`+name+`","versions":["`+version+`"]}]}`,
		)).WithContext(ctx)
		response := httptest.NewRecorder()
		server.updateMinecraftVersions(response, request)
		return response
	}
	if response := update("Fabric", "1.20.1"); response.Code != http.StatusOK {
		t.Fatalf("seed version configuration status=%d body=%s", response.Code, response.Body.String())
	}

	previousClient := minecraftVersionHTTPClient
	resetMinecraftSourceCacheForTest()
	defer func() { minecraftVersionHTTPClient = previousClient; resetMinecraftSourceCacheForTest() }()
	fetchStarted, releaseFetch := make(chan struct{}), make(chan struct{})
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == fabricGameVersionsURL {
			close(fetchStarted)
			select {
			case <-releaseFetch:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return test033MinecraftSourceResponse(request), nil
	})}

	syncComplete := make(chan struct{})
	var syncErr error
	go func() {
		defer close(syncComplete)
		_, syncErr = syncMinecraftVersionCatalog(ctx, syncPool)
	}()
	released := false
	defer func() {
		if !released {
			close(releaseFetch)
		}
		<-syncComplete
	}()
	select {
	case <-fetchStarted:
	case <-syncComplete:
		t.Fatalf("synchronization completed before its loader fetch: %v", syncErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	conflict := update("Custom Fabric", "1.20.4")
	duringSync, err := loadMinecraftVersionConfig(ctx, adminPool)
	if err != nil {
		t.Fatal(err)
	}
	close(releaseFetch)
	released = true
	<-syncComplete
	if syncErr != nil {
		t.Fatal(syncErr)
	}
	afterSync, err := loadMinecraftVersionConfig(ctx, adminPool)
	if err != nil {
		t.Fatal(err)
	}
	if conflict.Code != http.StatusConflict || duringSync.Loaders[0].Name != "Fabric" {
		t.Errorf("concurrent update status=%d loader during sync=%q after sync=%q; want 409 with no saved edit",
			conflict.Code, duringSync.Loaders[0].Name, afterSync.Loaders[0].Name)
	}
	if afterSync.LastSyncedAt == "" || afterSync.Loaders[0].Name != "Fabric" {
		t.Fatalf("synchronized catalog was not published: %#v", afterSync)
	}
	if version, err := resolveSynchronizedMRPackLoaderVersion(ctx, adminPool, "1.20.1", "fabric"); err != nil || version != "0.16.14" {
		t.Fatalf("synced artifact=%q err=%v", version, err)
	}

	if response := update("Custom Fabric", "1.20.4"); response.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
	}
	afterRetry, err := loadMinecraftVersionConfig(ctx, adminPool)
	if err != nil {
		t.Fatal(err)
	}
	if afterRetry.Loaders[0].Name != "Custom Fabric" || afterRetry.Versions[0].Code != "1.20.4" ||
		afterRetry.LastSyncedAt != afterSync.LastSyncedAt || afterRetry.SourceURL != afterSync.SourceURL {
		t.Fatalf("retry failed to persist the edit and keep sync metadata: %#v", afterRetry)
	}
	if _, err = resolveSynchronizedMRPackLoaderVersion(ctx, adminPool, "1.20.4", "fabric"); !errors.Is(err, errMinecraftLoaderArtifactUnavailable) {
		t.Fatalf("outdated snapshot was treated as authoritative: %v", err)
	}
	var artifactCount int
	if err = adminPool.QueryRow(ctx, `select count(*) from minecraft_loader_artifact_versions`).Scan(&artifactCount); err != nil || artifactCount != 0 {
		t.Fatalf("stale artifacts persisted after retry: count=%d err=%v", artifactCount, err)
	}
	for _, body := range []string{
		`{"versions":[{"code":"1.20.4","type":"release"}],"commonVersions":[],"loaders":[]}`,
		`{"versions":[],"commonVersions":[],"loaders":[]}`,
	} {
		written := httptest.NewRecorder()
		server.updateMinecraftVersions(written, httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/minecraft-versions", strings.NewReader(body)).WithContext(ctx))
		public := httptest.NewRecorder()
		server.publicMinecraftVersions(public, httptest.NewRequest(http.MethodGet, "/api/v1/minecraft/versions", nil).WithContext(ctx))
		var saved, read struct{ Data minecraftVersionConfig }
		if written.Code != http.StatusOK || public.Code != http.StatusOK ||
			json.Unmarshal(written.Body.Bytes(), &saved) != nil || json.Unmarshal(public.Body.Bytes(), &read) != nil {
			t.Fatalf("catalog save/read failed: PUT=%d GET=%d", written.Code, public.Code)
		}
		if read.Data.Loaders == nil || read.Data.CommonVersions == nil || !reflect.DeepEqual(saved.Data, read.Data) {
			t.Fatalf("catalog changed after saving: returned %#v, read back %#v", saved.Data, read.Data)
		}
	}

}
