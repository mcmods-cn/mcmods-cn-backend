package httpapi

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/activitycatalog"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestREUSE003AndMAP003PersistentCatalogAuthoritiesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify persistent catalog authorities")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	assertActivityDictionaryRows(t, ctx, pool, "activity_actions", activitycatalog.ActionDefinitions())
	assertActivityDictionaryRows(t, ctx, pool, "activity_object_types", activitycatalog.ObjectTypeDefinitions())

	const catalog = `{"versions":[{"code":"25w10a","type":"snapshot"},{"code":"1.20.6","type":"release"},{"code":"1.21.1","type":"release"}],"commonVersions":["1.20.6"],"loaders":[{"code":"Fabric","name":"Fabric","versions":["1.20.6"]}]}`
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
		on conflict(key) do update set value=excluded.value`, minecraftVersionsSettingKey, catalog); err != nil {
		t.Fatal(err)
	}
	versionConfig, err := loadMinecraftVersionConfig(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	versions, _ := projectFileFilters([]projectFileItem{
		{GameVersions: []string{"1.21.1", "unknown-z", "25w10a"}},
		{GameVersions: []string{"unknown-a", "1.20.6"}},
	}, minecraftVersionOrder(versionConfig))
	want := []string{"25w10a", "1.20.6", "1.21.1", "unknown-a", "unknown-z"}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("persisted catalog order produced %#v, want %#v", versions, want)
	}
}

func assertActivityDictionaryRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, want []activitycatalog.Entry) {
	t.Helper()
	rows, err := pool.Query(ctx, "select id,code,name from "+table+" order by id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make([]activitycatalog.Entry, 0, len(want))
	for rows.Next() {
		var entry activitycatalog.Entry
		if err = rows.Scan(&entry.ID, &entry.Code, &entry.Name); err != nil {
			t.Fatal(err)
		}
		got = append(got, entry)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s rows=%#v, want registry %#v", table, got, want)
	}
}
