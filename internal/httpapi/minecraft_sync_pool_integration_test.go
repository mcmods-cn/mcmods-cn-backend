package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOCT02MinecraftSyncUsesItsSingleLeaseConnectionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	if err := saveMinecraftVersionConfigAndInvalidateStaleArtifacts(ctx, base, minecraftVersionConfig{
		Versions: []minecraftVersionOption{{Code: "1.20.1", Type: "release"}},
		Loaders:  []minecraftLoaderOption{{Code: "Fabric", Name: "Fabric", Versions: []string{"1.20.1"}}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := base.Config()
	cfg.MaxConns, cfg.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	previous := minecraftVersionHTTPClient
	resetMinecraftSourceCacheForTest()
	defer func() { minecraftVersionHTTPClient = previous; resetMinecraftSourceCacheForTest() }()
	var requests atomic.Int32
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		payload := ""
		switch request.URL.String() {
		case mojangVersionManifestURL:
			payload = `{"latest":{"release":"1.20.1"},"versions":[{"id":"1.20.1","type":"release"}]}`
		case fabricGameVersionsURL:
			payload = `[{"version":"1.20.1"}]`
		case fabricLoaderCatalogURL:
			payload = `[{"version":"0.16.9","stable":true}]`
		default:
			return nil, fmt.Errorf("unexpected fixture source URL")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload)), Request: request}, nil
	})}
	syncCtx, syncCancel := context.WithTimeout(ctx, 3*time.Second)
	defer syncCancel()
	result, err := syncMinecraftVersionCatalog(syncCtx, pool)
	if err != nil || result.LastSyncedAt == "" {
		t.Fatalf("single connection sync failed: %v", err)
	}
	var artifacts int
	if err = pool.QueryRow(ctx, `select count(*) from minecraft_loader_artifact_versions where minecraft_version='1.20.1' and loader_type='fabric' and loader_version='0.16.9'`).Scan(&artifacts); err != nil || artifacts != 1 || requests.Load() != 3 {
		t.Fatalf("persistent artifacts=%d requests=%d error=%v", artifacts, requests.Load(), err)
	}
	lease, err := acquireMinecraftVersionSyncLease(ctx, pool)
	if err != nil {
		t.Fatal("completed synchronization retained its lease:", err)
	}
	if err = lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
}
