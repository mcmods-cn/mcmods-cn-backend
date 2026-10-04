package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOCT03MinecraftSyncReleasesLeaseAfterFetchCancellationAndPersistenceFailuresIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	for _, scenario := range []string{"upstream error", "cancelled fetch", "settings read failure", "artifact publication failure"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			poolA, databaseName := newTEST033IsolatedDatabase(t, ctx)
			poolB := newTEST033DatabasePool(t, ctx, databaseName)
			if err := saveMinecraftVersionConfig(ctx, poolA, minecraftVersionConfig{
				Versions: []minecraftVersionOption{{Code: "1.20.1", Type: "release"}},
				Loaders:  []minecraftLoaderOption{{Code: "Fabric", Name: "Fabric", Versions: []string{"1.20.1"}}},
			}); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := poolA.QueryRow(ctx, `select value::text from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&before); err != nil {
				t.Fatal(err)
			}
			previous := minecraftVersionHTTPClient
			resetMinecraftSourceCacheForTest()
			t.Cleanup(func() { minecraftVersionHTTPClient = previous; resetMinecraftSourceCacheForTest() })
			syncCtx, syncCancel := context.WithCancel(ctx)
			defer syncCancel()
			minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				switch scenario {
				case "upstream error":
					return nil, errors.New("owned OCT03 upstream unavailable")
				case "cancelled fetch":
					syncCancel()
					return nil, context.Canceled
				default:
					return test033MinecraftSourceResponse(request), nil
				}
			})}
			if scenario == "settings read failure" {
				if _, err := poolA.Exec(ctx, `alter table system_settings rename to oct03_hidden_settings`); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "artifact publication failure" {
				if _, err := poolA.Exec(ctx, `alter table minecraft_loader_artifact_versions add constraint oct03_reject_publication check(loader_version='oct03-impossible') not valid`); err != nil {
					t.Fatal(err)
				}
			}
			_, syncErr := syncMinecraftVersionCatalog(syncCtx, poolA)
			if syncErr == nil {
				t.Fatal("fault-injected synchronization returned success")
			}
			if scenario == "upstream error" && !strings.Contains(syncErr.Error(), "owned OCT03 upstream unavailable") {
				t.Fatalf("upstream error was not preserved: %v", syncErr)
			}
			if scenario == "settings read failure" {
				if !strings.Contains(syncErr.Error(), "load Minecraft version configuration") {
					t.Fatalf("settings failure was not observable: %v", syncErr)
				}
				if _, err := poolA.Exec(ctx, `alter table oct03_hidden_settings rename to system_settings`); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "artifact publication failure" {
				var databaseErr *pgconn.PgError
				if !errors.As(syncErr, &databaseErr) || databaseErr.Code != "23514" {
					t.Fatalf("publication constraint failure was not preserved: %v", syncErr)
				}
			}
			lease, err := acquireMinecraftVersionSyncLease(ctx, poolB)
			if err != nil {
				t.Fatal("failed or cancelled synchronization retained its cross-pool lease:", err)
			}
			if err = lease.Release(ctx); err != nil {
				t.Fatal(err)
			}
			var after string
			var artifacts int
			if err = poolB.QueryRow(ctx, `select value::text from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if err = poolB.QueryRow(ctx, `select count(*) from minecraft_loader_artifact_versions`).Scan(&artifacts); err != nil {
				t.Fatal(err)
			}
			if before != after || artifacts != 0 {
				t.Fatalf("failed synchronization published settings/artifacts: changed=%t artifacts=%d", before != after, artifacts)
			}
		})
	}
}
