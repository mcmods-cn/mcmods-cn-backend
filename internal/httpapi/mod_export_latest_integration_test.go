package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
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
	var existingRevisionID, existingItemID string
	lookupErr := pool.QueryRow(context.Background(), `
		select snapshot.revision_id,resource.canonical_id
		from game_resource_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join mod_export_revisions revision on revision.id=snapshot.revision_id
		where snapshot.registry='items' and revision.is_active
		order by revision.created_at desc limit 1`).Scan(&existingRevisionID, &existingItemID)
	if lookupErr == nil {
		server := &Server{db: pool}
		if _, resolveErr := server.resolveExportResources(context.Background(), []exportResourceKey{{
			RevisionID: existingRevisionID, ResourceID: existingItemID, Kind: "item",
		}}); resolveErr != nil {
			t.Fatalf("resolve imported resource: %v", resolveErr)
		}
	} else if lookupErr != pgx.ErrNoRows {
		t.Fatal(lookupErr)
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
	categoryRaw := read("recipes/jei/categories.json")
	var categoryDocument exportJEICategoryDocument
	if err = json.Unmarshal(categoryRaw, &categoryDocument); err != nil {
		t.Fatal(err)
	}
	revisions := map[string]string{"minecraft": revisionID}
	for _, category := range categoryDocument.Categories {
		revisions[exportResourceNamespace(category.RecipeTypeID)] = revisionID
	}
	if err = importExportRecipeTypesV5(context.Background(), tx, packageID, revisions, categoryRaw); err != nil {
		t.Fatal(err)
	}
	if err = importExportTags(context.Background(), tx, revisions, read("tags/tags.json")); err != nil {
		t.Fatal(err)
	}
	batch := newModExportWriteBatch()
	templateDocumentCount := 0
	expectedTemplateCount := 0
	for name, file := range files {
		if len(name) < len("recipes/jei/templates/") || name[:len("recipes/jei/templates/")] != "recipes/jei/templates/" {
			continue
		}
		value, readErr := readExportZIPFile(file, maxExportJSONSize)
		if readErr != nil {
			t.Fatal(readErr)
		}
		document, decodeErr := decodeExportJEITemplateCollection(value)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if err = queueExportJEITemplateCollection(batch, revisions, name, document); err != nil {
			t.Fatal(err)
		}
		templateDocumentCount++
		expectedTemplateCount += len(document.Templates)
		if batch.shouldFlush() {
			if err = batch.flush(context.Background(), tx); err != nil {
				t.Fatal(err)
			}
		}
	}
	expectedRecipeCount := 0
	for name, file := range files {
		if len(name) < len("recipes/jei/recipes/") || name[:len("recipes/jei/recipes/")] != "recipes/jei/recipes/" {
			continue
		}
		value, readErr := readExportZIPFile(file, maxExportJSONSize)
		if readErr != nil {
			t.Fatal(readErr)
		}
		document, decodeErr := decodeExportJEIRecipeCollection(value)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if err = queueExportJEIRecipeCollection(batch, packageID, revisions, name, document); err != nil {
			t.Fatal(err)
		}
		expectedRecipeCount += len(document.Recipes)
		if batch.shouldFlush() {
			if err = batch.flush(context.Background(), tx); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = batch.flush(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	var recipeTypes, templates, recipes, alternatives int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_type_snapshots where revision_id=$1`, revisionID).Scan(&recipeTypes); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_layout_templates where revision_id=$1`, revisionID).Scan(&templates); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_snapshots where revision_id=$1`, revisionID).Scan(&recipes); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_binding_alternatives alternative
		join recipe_bindings binding on binding.id=alternative.binding_id
		join recipe_snapshots snapshot on snapshot.id=binding.recipe_snapshot_id where snapshot.revision_id=$1`, revisionID).Scan(&alternatives); err != nil {
		t.Fatal(err)
	}
	if recipeTypes == 0 || templates != expectedTemplateCount || recipes != expectedRecipeCount || alternatives == 0 {
		t.Fatalf("unexpected normalized counts: types=%d templates=%d/%d recipes=%d/%d alternatives=%d template documents=%d",
			recipeTypes, templates, expectedTemplateCount, recipes, expectedRecipeCount, alternatives, templateDocumentCount)
	}
	var canonicalLayouts, generatedLayouts, invalidIdentities int
	if err = tx.QueryRow(context.Background(), `select
		count(*) filter(where recipe.canonical_source_id is not null)::int,
		count(*) filter(where snapshot.source_id_kind='generated_index')::int,
		count(*) filter(where (snapshot.source_id_kind in ('minecraft_recipe','jei_category')) <> (recipe.canonical_source_id is not null)
			or (snapshot.source_id_kind='generated_index' and recipe.canonical_source_id is not null)
			or recipe.entity_id='')::int
		from recipe_snapshots snapshot join recipes recipe on recipe.entity_id=snapshot.recipe_id
		where snapshot.revision_id=$1`, revisionID).Scan(&canonicalLayouts, &generatedLayouts, &invalidIdentities); err != nil {
		t.Fatal(err)
	}
	if canonicalLayouts == 0 || invalidIdentities != 0 {
		t.Fatalf("unexpected recipe identity distribution: canonical=%d generated=%d invalid=%d", canonicalLayouts, generatedLayouts, invalidIdentities)
	}
	rows, err := tx.Query(context.Background(), `with `+latestGlobalExportScopeCTE+`
		select recipe_type.canonical_id,(jsonb_agg(snapshot.title_names order by revision.activated_at desc nulls last)->0),
		(select count(distinct recipe_snapshot.recipe_id)::int from latest_revisions source
		 join recipe_snapshots recipe_snapshot on recipe_snapshot.revision_id=source.id
		 join recipes recipe on recipe.entity_id=recipe_snapshot.recipe_id where recipe.recipe_type_id=recipe_type.entity_id),
		coalesce(jsonb_agg(distinct (catalyst.value || jsonb_build_object('revisionId',snapshot.revision_id)))
		 filter(where catalyst.value is not null),'[]'::jsonb),(array_agg(snapshot.revision_id order by revision.activated_at desc nulls last))[1]
		from latest_revisions revision join recipe_type_snapshots snapshot on snapshot.revision_id=revision.id
		join recipe_types recipe_type on recipe_type.entity_id=snapshot.recipe_type_id
		left join lateral jsonb_array_elements(snapshot.catalysts) catalyst(value) on true
		group by recipe_type.entity_id,recipe_type.canonical_id order by recipe_type.canonical_id`)
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
