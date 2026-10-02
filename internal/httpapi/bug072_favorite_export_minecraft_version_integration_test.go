package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestFavoriteExportOnlyAcceptsEnabledAuthoritativeMinecraftVersionIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify favorite export version authority")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer database.DropEphemeralSchema(context.Background(), pool)

	var ownerID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('bug072-owner','bug072-owner@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_collections(public_id,user_id,name)
		values('b072vers1',$1,'BUG-072 versions')`, ownerID); err != nil {
		t.Fatal(err)
	}
	catalog := minecraftVersionConfig{
		Versions:       []minecraftVersionOption{{Code: "1.21.1", Type: "release"}},
		CommonVersions: []string{"1.21.1"},
		Loaders:        []minecraftLoaderOption{{Code: "Fabric", Name: "Fabric", Versions: []string{"1.21.1"}}},
	}
	if err = saveSynchronizedMinecraftVersionConfig(ctx, pool, catalog, []minecraftLoaderArtifactSnapshot{{
		MinecraftVersion: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14",
		SourceURL: fabricLoaderCatalogURL, ObservedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	claimsContext := context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID})
	preflight := func(minecraftVersion string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/favorite-collections/b072vers1/modpack-exports/preflight",
			strings.NewReader(`{"minecraftVersion":"`+minecraftVersion+`","loader":"fabric"}`))
		request.SetPathValue("id", "b072vers1")
		request = request.WithContext(claimsContext)
		response := httptest.NewRecorder()
		server.preflightFavoriteModpackExport(response, request)
		return response
	}
	for _, version := range []string{"9.9.9", "../../1.21.1"} {
		response := preflight(version)
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "MODPACK_EXPORT_INVALID_MINECRAFT_VERSION") {
			t.Fatalf("invalid version %q status=%d body=%s", version, response.Code, response.Body.String())
		}
	}
	validResponse := preflight("1.21.1")
	if validResponse.Code != http.StatusOK || !strings.Contains(validResponse.Body.String(), `"minecraftVersion":"1.21.1"`) {
		t.Fatalf("enabled version status=%d body=%s", validResponse.Code, validResponse.Body.String())
	}
	var previewCount int
	if err = pool.QueryRow(ctx, `select count(*) from favorite_modpack_export_previews`).Scan(&previewCount); err != nil {
		t.Fatal(err)
	}
	if previewCount != 1 {
		t.Fatalf("persisted previews=%d want only the enabled version", previewCount)
	}
}
