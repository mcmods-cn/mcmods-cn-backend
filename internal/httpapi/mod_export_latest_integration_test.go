package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
)

func TestLatestExporterCatalogImportIntegration(t *testing.T) {
	archivePath := os.Getenv("MCMODS_EXPORT_TEST_ZIP")
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if archivePath == "" || databaseURL == "" {
		t.Skip("set MCMODS_EXPORT_TEST_ZIP and MCMODS_TEST_DATABASE_URL to run the latest exporter integration test")
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	files, err := validateExportZIP(archive.File)
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		file := files[name]
		if file == nil {
			t.Fatalf("missing %s", name)
		}
		value, readErr := readExportZIPFile(file, maxExportJSONSize)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return value
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var modID int64
	if err = tx.QueryRow(context.Background(), `insert into mods(project_code,slug,primary_name,review_status)
		values('tst9z9x','catalog-import-integration-test','Catalog import integration test','approved') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	packageID := "00000000-0000-4000-8000-000000000001"
	revisionID := "00000000-0000-4000-8000-000000000002"
	if _, err = tx.Exec(context.Background(), `insert into mod_export_packages(id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest)
		values($1,$2,'test.zip','mcmods-export/v1','0.6.0','1.20.1','forge','{}')`, packageID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(context.Background(), `insert into mod_export_revisions(id,mod_id,package_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active)
		values($1,$2,$3,1,'ready','1.20.1','forge','0.6.0','minecraft',true)`, revisionID, modID, packageID); err != nil {
		t.Fatal(err)
	}
	if err = importExportRecipeTypes(context.Background(), tx, map[string]string{"minecraft": revisionID}, read("recipes/jei/categories.json")); err != nil {
		t.Fatal(err)
	}
	if err = importExportTags(context.Background(), tx, map[string]string{"minecraft": revisionID}, read("tags/tags.json")); err != nil {
		t.Fatal(err)
	}
	batch := newModExportWriteBatch()
	layoutDocumentCount := 0
	expectedLayoutCount := 0
	for name, file := range files {
		if len(name) < len("recipes/jei/layouts/") || name[:len("recipes/jei/layouts/")] != "recipes/jei/layouts/" {
			continue
		}
		value, readErr := readExportZIPFile(file, maxExportJSONSize)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err = queueExportRecipeLayout(batch, packageID, revisionID, name, value); err != nil {
			t.Fatal(err)
		}
		layoutDocumentCount++
		var document map[string]any
		if err = json.Unmarshal(value, &document); err != nil {
			t.Fatal(err)
		}
		if layouts, ok := document["layouts"].([]any); ok {
			expectedLayoutCount += len(layouts)
		} else {
			expectedLayoutCount++
		}
		if batch.shouldFlush() {
			if err = batch.flush(context.Background(), tx); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = batch.flush(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	var recipeTypes, layouts, recipeItems int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from mod_export_recipe_types where revision_id=$1`, revisionID).Scan(&recipeTypes); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from mod_export_recipe_layouts where revision_id=$1`, revisionID).Scan(&layouts); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from mod_export_recipe_items where revision_id=$1`, revisionID).Scan(&recipeItems); err != nil {
		t.Fatal(err)
	}
	if recipeTypes == 0 || layouts != expectedLayoutCount || layouts == 0 || recipeItems == 0 {
		t.Fatalf("unexpected normalized counts: recipe types=%d layouts=%d recipe items=%d archive layout documents=%d expected layouts=%d", recipeTypes, layouts, recipeItems, layoutDocumentCount, expectedLayoutCount)
	}
	var canonicalLayouts, generatedLayouts, invalidIdentities int
	if err = tx.QueryRow(context.Background(), `select
		count(*) filter(where recipe_id_canonical)::int,
		count(*) filter(where recipe_id_source='generated_index')::int,
		count(*) filter(where (recipe_id_source in ('minecraft_recipe','jei_category')) <> recipe_id_canonical
			or (recipe_id_source='generated_index' and recipe_id_canonical)
			or recipe_key='')::int
		from mod_export_recipe_layouts where revision_id=$1`, revisionID).Scan(&canonicalLayouts, &generatedLayouts, &invalidIdentities); err != nil {
		t.Fatal(err)
	}
	if canonicalLayouts == 0 || generatedLayouts == 0 || invalidIdentities != 0 {
		t.Fatalf("unexpected recipe identity distribution: canonical=%d generated=%d invalid=%d", canonicalLayouts, generatedLayouts, invalidIdentities)
	}
	rows, err := tx.Query(context.Background(), `with `+latestGlobalExportScopeCTE+`
		select recipe_type.recipe_type_id,(jsonb_agg(recipe_type.title_names order by revision.activated_at desc nulls last)->0),
		(select count(distinct layout.recipe_key)::int from latest_revisions source
		 join mod_export_recipe_layouts layout on layout.revision_id=source.id where layout.recipe_type_id=recipe_type.recipe_type_id),
		coalesce(jsonb_agg(distinct (catalyst.value || jsonb_build_object('revisionId',recipe_type.revision_id)))
		 filter(where catalyst.value is not null),'[]'::jsonb),(array_agg(recipe_type.revision_id order by revision.activated_at desc nulls last))[1]
		from latest_revisions revision join mod_export_recipe_types recipe_type on recipe_type.revision_id=revision.id
		left join lateral jsonb_array_elements(recipe_type.catalysts) catalyst(value) on true
		group by recipe_type.recipe_type_id order by recipe_type.recipe_type_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	globalTypes := 0
	for rows.Next() {
		var id, sourceRevision string
		var names, catalysts []byte
		var recipeCount int
		if err = rows.Scan(&id, &names, &recipeCount, &catalysts, &sourceRevision); err != nil {
			t.Fatal(err)
		}
		globalTypes++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if globalTypes < recipeTypes {
		t.Fatalf("global recipe type query returned %d types, expected at least %d", globalTypes, recipeTypes)
	}
}
