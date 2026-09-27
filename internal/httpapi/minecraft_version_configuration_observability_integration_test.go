package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestMinecraftVersionConfigurationDistinguishesMissingFromBrokenIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify Minecraft configuration failure semantics")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temp table system_settings(key text primary key,value jsonb not null,updated_at timestamptz not null default now())`); err != nil {
		t.Fatal(err)
	}

	missing, err := loadMinecraftVersionConfig(ctx, pool)
	if err != nil || len(missing.Versions) == 0 || len(missing.Loaders) == 0 {
		t.Fatalf("missing setting=(versions=%d loaders=%d err=%v); want explicit safe default", len(missing.Versions), len(missing.Loaders), err)
	}
	for _, loader := range missing.Loaders {
		if loader.Versions == nil || len(loader.Versions) != 0 {
			t.Fatalf("missing setting loader %q versions=%#v; want explicit unknown/empty compatibility", loader.Code, loader.Versions)
		}
	}
	server := &Server{db: pool}
	response := httptest.NewRecorder()
	server.publicMinecraftVersions(response, httptest.NewRequest(http.MethodGet, "/api/v1/minecraft/versions", nil).WithContext(ctx))
	var publicMissing minecraftVersionConfig
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &publicMissing) != nil {
		t.Fatalf("public missing configuration status=%d body=%s; want decodable safe default", response.Code, response.Body.String())
	}
	for _, loader := range publicMissing.Loaders {
		if loader.Versions == nil || len(loader.Versions) != 0 {
			t.Fatalf("public missing loader %q versions=%#v; want JSON [] for unknown compatibility", loader.Code, loader.Versions)
		}
	}
	response = httptest.NewRecorder()
	duplicateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/minecraft/versions", strings.NewReader(
		`{"versions":[{"code":"1.21.1","type":"release"}],"loaders":[{"code":"Forge","versions":["1.21.1"]},{"code":"forge","versions":[]}]}`,
	)).WithContext(ctx)
	server.updateMinecraftVersions(response, duplicateRequest)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate loader update status=%d body=%s; want 400", response.Code, response.Body.String())
	}

	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,'{"versions":"broken"}'::jsonb)`, minecraftVersionsSettingKey); err != nil {
		t.Fatal(err)
	}
	if broken, loadErr := loadMinecraftVersionConfig(ctx, pool); loadErr == nil || !errors.Is(loadErr, errMinecraftVersionConfigUnavailable) || !reflect.DeepEqual(broken, minecraftVersionConfig{}) {
		t.Fatalf("broken JSON shape=(%#v,%v); want zero/error", broken, loadErr)
	}
	response = httptest.NewRecorder()
	server.publicMinecraftVersions(response, httptest.NewRequest(http.MethodGet, "/api/v1/minecraft/versions", nil).WithContext(ctx))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("public broken configuration status=%d body=%s; want 500", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/minecraft/versions", strings.NewReader(
		`{"versions":[{"code":"1.20.1","type":"release"}],"commonVersions":["1.20.1"],"loaders":[{"code":"Forge","name":"Forge","versions":["1.20.1"]}]}`,
	)).WithContext(ctx)
	server.updateMinecraftVersions(response, updateRequest)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("admin update over broken configuration status=%d body=%s; want 500", response.Code, response.Body.String())
	}

	tooLongCode := strings.Repeat("x", 81)
	if _, err = pool.Exec(ctx, `update system_settings set value=jsonb_build_object('versions',jsonb_build_array(jsonb_build_object('code',$2::text,'type','release')))
		where key=$1`, minecraftVersionsSettingKey, tooLongCode); err != nil {
		t.Fatal(err)
	}
	if invalid, loadErr := loadMinecraftVersionConfig(ctx, pool); loadErr == nil || !reflect.DeepEqual(invalid, minecraftVersionConfig{}) {
		t.Fatalf("invalid normalized configuration=(%#v,%v); want zero/error", invalid, loadErr)
	}

	if _, err = pool.Exec(ctx, `update system_settings set value='{"versions":[{"code":"1.21.1","type":"release"}],"loaders":[{"code":"Forge","versions":["1.21.1"]},{"code":"forge","versions":[]}]}'::jsonb
		where key=$1`, minecraftVersionsSettingKey); err != nil {
		t.Fatal(err)
	}
	if duplicate, loadErr := loadMinecraftVersionConfig(ctx, pool); loadErr == nil || !reflect.DeepEqual(duplicate, minecraftVersionConfig{}) {
		t.Fatalf("duplicate stored loader configuration=(%#v,%v); want zero/error", duplicate, loadErr)
	}

	if _, err = pool.Exec(ctx, `update system_settings set value='{"versions":[{"code":"1.20.1","type":"release"}],"commonVersions":["1.20.1"],"loaders":[{"code":"Forge","name":"Forge","versions":["1.20.1"]}]}'::jsonb
		where key=$1`, minecraftVersionsSettingKey); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadMinecraftVersionConfig(ctx, pool)
	if err != nil || len(loaded.Versions) != 1 || loaded.Versions[0].Code != "1.20.1" || len(loaded.Loaders) != 1 || loaded.Loaders[0].Code != "Forge" {
		t.Fatalf("valid stored configuration=%#v err=%v", loaded, err)
	}

	if _, err = pool.Exec(ctx, `alter table system_settings rename column value to arch016_broken_value`); err != nil {
		t.Fatal(err)
	}
	if failed, loadErr := loadMinecraftVersionConfig(ctx, pool); loadErr == nil || !reflect.DeepEqual(failed, minecraftVersionConfig{}) {
		t.Fatalf("database failure=(%#v,%v); want zero/error", failed, loadErr)
	}
}
