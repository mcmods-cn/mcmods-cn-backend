package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestCatalogCardAssociationPlansAtMillionRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify million-row catalog card plans")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err = pool.Exec(ctx, `create temp table simple_projects(
		id bigint primary key,default_locale text not null,project_type text not null);
		create temp table simple_project_localizations(
		project_id bigint not null,locale text not null,name text not null,summary text not null,body_markdown text not null,
		primary key(project_id,locale));
		insert into simple_projects select value,'zh-CN','plugin' from generate_series(1,125000) value;
		insert into simple_project_localizations
		select project.id,locale.code,'name-'||project.id||'-'||locale.code,'summary','detail-only-body'
		from simple_projects project cross join (values
			('zh-CN'),('zh-TW'),('en-US'),('ja-JP'),('ru-RU'),('fr-FR'),('de-DE'),('es-ES')) locale(code);
		analyze simple_projects; analyze simple_project_localizations`); err != nil {
		t.Fatal(err)
	}
	projectIDs := make([]int64, 100)
	for index := range projectIDs {
		projectIDs[index] = int64(124901 + index)
	}
	localizationPlan := explainCatalogCardPlan(t, ctx, pool,
		"explain (analyze,buffers,format text) "+simpleProjectCatalogLocalizationsSQL, projectIDs, "en-US")
	if strings.Contains(localizationPlan, "Seq Scan on simple_project_localizations") ||
		!strings.Contains(localizationPlan, "simple_project_localizations_pkey") {
		t.Fatalf("million-row localization projection did not use its project/locale index:\n%s", localizationPlan)
	}

	if _, err = pool.Exec(ctx, `create temp table modpack_loader_compatibilities(
		modpack_id bigint not null,loader text not null,minecraft_version text not null,
		primary key(modpack_id,loader,minecraft_version));
		insert into modpack_loader_compatibilities
		select pack,'loader-'||lpad(loader::text,2,'0'),'1.'||lpad(version::text,2,'0')
		from generate_series(1,1250) pack cross join generate_series(1,20) loader cross join generate_series(1,40) version;
		analyze modpack_loader_compatibilities`); err != nil {
		t.Fatal(err)
	}
	modpackIDs := make([]int64, 100)
	for index := range modpackIDs {
		modpackIDs[index] = int64(1151 + index)
	}
	compatibilityPlan := explainCatalogCardPlan(t, ctx, pool,
		"explain (analyze,buffers,format text) "+modpackCatalogCompatibilitiesSQL, modpackIDs)
	if strings.Contains(compatibilityPlan, "Seq Scan on modpack_loader_compatibilities") ||
		!strings.Contains(compatibilityPlan, "modpack_loader_compatibilities_pkey") {
		t.Fatalf("million-row compatibility projection did not use its pack/loader/version index:\n%s", compatibilityPlan)
	}
	t.Logf("localizations plan:\n%s", localizationPlan)
	t.Logf("compatibilities plan:\n%s", compatibilityPlan)
}

func explainCatalogCardPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, arguments ...any) string {
	t.Helper()
	rows, err := pool.Query(ctx, query, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	lines := make([]string, 0, 32)
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
