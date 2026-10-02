package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCatalogDatasetVersionReplacesRevisionAggregation(t *testing.T) {
	raw, err := os.ReadFile("global_catalog_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "func (s *Server) writeCachedCatalog(")
	end := strings.Index(text, "func requestedContentLocales(")
	if start < 0 || end <= start {
		t.Fatal("catalog cache function boundary was not found")
	}
	cacheSource := text[start:end]
	for _, forbidden := range []string{"string_agg", "latestGlobalExportScopeCTE", "max(updated_at)", "max(created_at)"} {
		if strings.Contains(cacheSource, forbidden) {
			t.Fatalf("catalog cache retains O(dataset) version expression %q", forbidden)
		}
	}
	if !strings.Contains(cacheSource, "select version::text from catalog_dataset_state where singleton") {
		t.Fatal("catalog cache does not read the singleton dataset version")
	}
	for _, filename := range []string{"catalog_editor_service.go", "mod_export_handlers.go", "mod_embedded_icon_import.go", "mod_export_query_handlers.go"} {
		source, readErr := os.ReadFile(filename)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(source), "bumpCatalogDatasetVersionTx") {
			t.Errorf("%s does not bump the catalog version in its publication transaction", filename)
		}
	}
}

func TestCatalogDatasetVersionCommitRollbackAndPlanIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `create temp table catalog_dataset_state(
		singleton boolean primary key default true check(singleton),version bigint not null check(version>0),updated_at timestamptz not null default now());
		insert into catalog_dataset_state(singleton,version) values(true,1);
		create temp table catalog_import_revisions_scale(id bigint primary key,payload text);
		insert into catalog_import_revisions_scale(id,payload) select value,'x' from generate_series(1,100000) value;
		analyze catalog_dataset_state; analyze catalog_import_revisions_scale`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	if err = bumpCatalogDatasetVersionTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err = db.QueryRow(ctx, `select version from catalog_dataset_state where singleton`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("rolled-back version=%d err=%v", version, err)
	}
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	if err = bumpCatalogDatasetVersionTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `select version from catalog_dataset_state where singleton`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("committed version=%d err=%v", version, err)
	}

	rows, err := db.Query(ctx, `explain (costs off) select version::text from catalog_dataset_state where singleton`)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	planText := strings.ToLower(plan.String())
	if !strings.Contains(planText, "catalog_dataset_state") || strings.Contains(planText, "catalog_import_revisions_scale") {
		t.Fatalf("catalog version plan is not isolated from 100k revision history:\n%s", plan.String())
	}

	if _, err = db.Exec(ctx, `delete from catalog_dataset_state`); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	if err = bumpCatalogDatasetVersionTx(ctx, tx); err == nil || !strings.Contains(err.Error(), "singleton is missing") {
		t.Fatalf("missing version singleton returned %v", err)
	}
	_ = tx.Rollback(ctx)
}
