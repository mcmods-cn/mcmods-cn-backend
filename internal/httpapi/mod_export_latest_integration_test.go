package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestLatestExporterWorldgenV2Contract(t *testing.T) {
	archivePath := os.Getenv("MCMODS_EXPORT_TEST_ZIP")
	if archivePath == "" {
		t.Skip("set MCMODS_EXPORT_TEST_ZIP to validate the latest exporter worldgen contract")
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
	for _, assetPath := range []string{"worldgen/natural_generation.json", "worldgen/structures.json"} {
		file := files[assetPath]
		if file == nil {
			t.Fatalf("missing %s", assetPath)
		}
		raw, readErr := readExportZIPFile(file, maxExportJSONSize)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var document map[string]any
		if decodeErr := json.Unmarshal(raw, &document); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if contractErr := validateCatalogDocumentContract(assetPath, document); contractErr != nil {
			t.Fatal(contractErr)
		}
		entries := exportDocumentEntries(assetPath, document)
		countKey := "count"
		if assetPath == "worldgen/natural_generation.json" {
			countKey = "entry_count"
		}
		expected, _ := document[countKey].(float64)
		if len(entries) != int(expected) {
			t.Fatalf("%s projected %d entries, root declares %.0f", assetPath, len(entries), expected)
		}
		seen := make(map[string]struct{}, len(entries))
		for _, entry := range entries {
			if _, duplicate := seen[entry.ID]; duplicate {
				t.Fatalf("%s contains duplicate catalog identity %s", assetPath, entry.ID)
			}
			seen[entry.ID] = struct{}{}
		}
	}
}

func TestLatestExporterCatalogImportIntegration(t *testing.T) {
	archivePath := os.Getenv("MCMODS_EXPORT_TEST_ZIP")
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if archivePath == "" {
		t.Skip("set MCMODS_EXPORT_TEST_ZIP to run the latest exporter integration test")
	}
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the latest exporter integration test")
		}
		databaseURL = config.Load().DB.ConnString()
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
		from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
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
		values('tst9z9x01','catalog-import-integration-test','Catalog import integration test','approved') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	packageID := "00000000-0000-4000-8000-000000000001"
	revisionID := "00000000-0000-4000-8000-000000000002"
	jobID := "00000000-0000-4000-8000-000000000003"
	if _, err = tx.Exec(context.Background(), `insert into catalog_import_packages(id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest)
		values($1,$2,'test.zip','mcmods-export/v1','0.6.0','1.20.1','forge','{}')`, packageID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	var versionPublicID string
	if err = tx.QueryRow(context.Background(), `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status)
		values($1,'1.20.1 / forge',array['1.20.1'],array['forge'],'active') returning public_id`, modID).Scan(&versionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(context.Background(), `insert into catalog_import_jobs(id,mod_id,package_id,target_version_public_id,importer_version,status)
		values($1,$2,$3,$4,$5,'ready')`, jobID, modID, packageID, versionPublicID, modExportImporterVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(context.Background(), `insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_public_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active)
		values($1,$2,$3,$4,$5,1,'ready','1.20.1','forge','0.6.0','minecraft',false)`, revisionID, modID, packageID, jobID, versionPublicID); err != nil {
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
	if err = importExportRecipeTypes(context.Background(), tx, packageID, revisions, categoryRaw); err != nil {
		t.Fatal(err)
	}
	if err = importExportTags(context.Background(), tx, catalogResourceIdentityResolver{}, revisions, read("tags/tags.json")); err != nil {
		t.Fatal(err)
	}
	batch := newModExportWriteBatch()
	templateDocumentCount := 0
	expectedTemplateCount := 0
	var firstTemplateDocument exportJEITemplateCollection
	var firstTemplatePath string
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
		if len(firstTemplateDocument.Templates) == 0 && len(document.Templates) > 0 {
			firstTemplateDocument = document
			firstTemplatePath = name
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
		if err = queueExportJEIRecipeCollection(batch, catalogResourceIdentityResolver{}, packageID, revisions, name, document); err != nil {
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
	// The promotion helper is deliberately defensive: even an accidental
	// caller cannot publish a pending import before the revision is activated.
	if err = promoteImportedRecipeTemplatesTx(context.Background(), tx, revisionID); err != nil {
		t.Fatal(err)
	}
	var prematurelyPromoted int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_template_import_snapshots snapshot
		join recipe_layout_templates template on template.import_snapshot_id=snapshot.id where snapshot.revision_id=$1`, revisionID).
		Scan(&prematurelyPromoted); err != nil {
		t.Fatal(err)
	}
	if prematurelyPromoted != 0 {
		t.Fatalf("pending import promoted %d recipe templates before activation", prematurelyPromoted)
	}
	if _, err = tx.Exec(context.Background(), `update catalog_import_revisions set is_active=true,activated_at=now() where id=$1`, revisionID); err != nil {
		t.Fatal(err)
	}
	if err = promoteImportedRecipeTemplatesTx(context.Background(), tx, revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(context.Background(), `insert into catalog_import_translations(revision_id,locale,translation_key,value)
		values($1,'en_us','effect.minecraft.speed','Speed'),($1,'zh_cn','effect.minecraft.speed','速度')`, revisionID); err != nil {
		t.Fatal(err)
	}
	if err = importExportIconCatalogResources(context.Background(), tx, catalogResourceIdentityResolver{}, []modExportPNGMedia{
		{RevisionID: revisionID, AssetPath: "icons/mob_effects/32/minecraft/speed.png"},
		{RevisionID: revisionID, AssetPath: "icons/mob_effects/256/minecraft/speed.png"},
	}); err != nil {
		t.Fatal(err)
	}
	if err = syncImportedResourcesToContentVersionTx(context.Background(), tx, []string{revisionID}, versionPublicID, false, 0); err != nil {
		t.Fatalf("sync imported resources into the selected content version: %v", err)
	}
	var effectSectionResources int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from mod_content_section_resources section_resource
		join mod_content_sections section on section.id=section_resource.section_id
		join mod_content_templates template on template.id=section.template_id
		where section.version_id=(select id from mod_content_versions where public_id=$1) and template.code='mob_effect'`, versionPublicID).
		Scan(&effectSectionResources); err != nil {
		t.Fatal(err)
	}
	if effectSectionResources != 1 {
		t.Fatalf("normalized icon effect was not attached to the unified content section: got %d", effectSectionResources)
	}
	var recipeTypes, templates, recipes, alternatives int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_type_import_snapshots where revision_id=$1`, revisionID).Scan(&recipeTypes); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_template_import_snapshots where revision_id=$1`, revisionID).Scan(&templates); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_import_snapshots where revision_id=$1`, revisionID).Scan(&recipes); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_import_binding_candidates alternative
		join recipe_import_bindings binding on binding.id=alternative.binding_id
		join recipe_import_snapshots snapshot on snapshot.id=binding.recipe_snapshot_id where snapshot.revision_id=$1`, revisionID).Scan(&alternatives); err != nil {
		t.Fatal(err)
	}
	if recipeTypes == 0 || templates != expectedTemplateCount || recipes != expectedRecipeCount || alternatives == 0 {
		t.Fatalf("unexpected normalized counts: types=%d templates=%d/%d recipes=%d/%d alternatives=%d template documents=%d",
			recipeTypes, templates, expectedTemplateCount, recipes, expectedRecipeCount, alternatives, templateDocumentCount)
	}
	var canonicalTemplates, invalidCanonicalTemplates, invalidCanonicalSlots, recipesWithoutCanonicalTemplate int
	if err = tx.QueryRow(context.Background(), `select count(*)::int,
		count(*) filter(where snapshot.canonical_template_id is null or entity.public_id is null or template.entity_id is null)::int
		from recipe_template_import_snapshots snapshot
		left join catalog_entities entity on entity.id=snapshot.canonical_template_id and entity.entity_type='recipe_template'
		left join recipe_layout_templates template on template.entity_id=snapshot.canonical_template_id
		where snapshot.revision_id=$1`, revisionID).Scan(&canonicalTemplates, &invalidCanonicalTemplates); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_template_import_slots source_slot
		join recipe_template_import_snapshots snapshot on snapshot.id=source_slot.template_id
		left join recipe_template_slots canonical_slot on canonical_slot.template_id=snapshot.canonical_template_id
		 and canonical_slot.slot_key=source_slot.source_slot_id
		where snapshot.revision_id=$1 and source_slot.role in ('input','output','byproduct','catalyst')
		 and (canonical_slot.id is null or canonical_slot.role<>case when source_slot.role='byproduct' then 'output' else source_slot.role end)`, revisionID).
		Scan(&invalidCanonicalSlots); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(context.Background(), `select count(*)::int from recipe_import_snapshots recipe_snapshot
		join recipe_template_import_snapshots template_snapshot on template_snapshot.id=recipe_snapshot.template_id
		left join catalog_entities canonical_template on canonical_template.id=template_snapshot.canonical_template_id
		where recipe_snapshot.revision_id=$1 and recipe_snapshot.layout_available
		 and (canonical_template.id is null or canonical_template.entity_type<>'recipe_template')`, revisionID).
		Scan(&recipesWithoutCanonicalTemplate); err != nil {
		t.Fatal(err)
	}
	if canonicalTemplates != templates || invalidCanonicalTemplates != 0 || invalidCanonicalSlots != 0 || recipesWithoutCanonicalTemplate != 0 {
		t.Fatalf("canonical template promotion failed: canonical=%d/%d invalidTemplates=%d invalidSlots=%d unmappedRecipes=%d",
			canonicalTemplates, templates, invalidCanonicalTemplates, invalidCanonicalSlots, recipesWithoutCanonicalTemplate)
	}
	if len(firstTemplateDocument.Templates) > 0 {
		protectedSource := firstTemplateDocument.Templates[0]
		typeIdentity := recipeTypeIdentity(firstTemplateDocument.RecipeTypeID)
		protectedIdentity := catalogEditorIdentityForTemplate(typeIdentity.ID, protectedSource.TemplateID)
		var beforeWidth int
		var beforeDefinition string
		if err = tx.QueryRow(context.Background(), `select canvas_width,definition::text from recipe_layout_templates where entity_id=$1`, protectedIdentity.ID).
			Scan(&beforeWidth, &beforeDefinition); err != nil {
			t.Fatal(err)
		}
		var nextRevisionNumber, humanRevisionID int64
		if err = tx.QueryRow(context.Background(), `select coalesce(max(revision_no),0)+1 from content_revisions
			where aggregate_type=$1 and aggregate_key=$2`, catalogAggregateRecipeTemplate, protectedIdentity.ID).Scan(&nextRevisionNumber); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRow(context.Background(), `insert into content_revisions(entity_id,aggregate_type,aggregate_key,revision_no,
			snapshot,snapshot_hash,source) values($1,$2,$1,$3,'{}'::jsonb,$4,'user') returning id`, protectedIdentity.ID,
			catalogAggregateRecipeTemplate, nextRevisionNumber, "integration-human-template").Scan(&humanRevisionID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(context.Background(), `update catalog_entities set published_revision_id=$2 where id=$1`, protectedIdentity.ID, humanRevisionID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(context.Background(), `update recipe_layout_templates set published_revision_id=$2 where entity_id=$1`, protectedIdentity.ID, humanRevisionID); err != nil {
			t.Fatal(err)
		}
		canvas, canvasErr := canonicalImportedTemplateCanvas(firstTemplateDocument)
		if canvasErr != nil {
			t.Fatal(canvasErr)
		}
		canvas.Width++
		firstTemplateDocument.Canvas, _ = json.Marshal(canvas)
		firstTemplateDocument.Templates[0].BackgroundContainsIngredients = !firstTemplateDocument.Templates[0].BackgroundContainsIngredients
		refreshBatch := newModExportWriteBatch()
		if err = queueExportJEITemplateCollection(refreshBatch, revisions, firstTemplatePath, firstTemplateDocument); err != nil {
			t.Fatal(err)
		}
		if err = refreshBatch.flush(context.Background(), tx); err != nil {
			t.Fatal(err)
		}
		var afterWidth, observedWidth int
		var afterDefinition string
		if err = tx.QueryRow(context.Background(), `select template.canvas_width,template.definition::text,
			(snapshot.canvas->>'width')::int from recipe_layout_templates template
			join recipe_template_import_snapshots snapshot on snapshot.id=template.import_snapshot_id where template.entity_id=$1`, protectedIdentity.ID).
			Scan(&afterWidth, &afterDefinition, &observedWidth); err != nil {
			t.Fatal(err)
		}
		if afterWidth != beforeWidth || afterDefinition != beforeDefinition || observedWidth != canvas.Width {
			t.Fatalf("human template was overwritten by import refresh: canonicalWidth=%d->%d observedWidth=%d expected=%d",
				beforeWidth, afterWidth, observedWidth, canvas.Width)
		}
	}
	var canonicalLayouts, generatedLayouts, invalidIdentities int
	if err = tx.QueryRow(context.Background(), `select
		count(*) filter(where recipe.canonical_source_id is not null)::int,
		count(*) filter(where snapshot.source_id_kind='generated_index')::int,
		count(*) filter(where (snapshot.source_id_kind in ('minecraft_recipe','jei_category')) <> (recipe.canonical_source_id is not null)
			or (snapshot.source_id_kind='generated_index' and recipe.canonical_source_id is not null)
			or recipe.entity_id='')::int
		from recipe_import_snapshots snapshot join recipes recipe on recipe.entity_id=snapshot.recipe_id
		where snapshot.revision_id=$1`, revisionID).Scan(&canonicalLayouts, &generatedLayouts, &invalidIdentities); err != nil {
		t.Fatal(err)
	}
	if canonicalLayouts == 0 || invalidIdentities != 0 {
		t.Fatalf("unexpected recipe identity distribution: canonical=%d generated=%d invalid=%d", canonicalLayouts, generatedLayouts, invalidIdentities)
	}
	rows, err := tx.Query(context.Background(), `with `+latestGlobalExportScopeCTE+`
		select recipe_type.canonical_id,(jsonb_agg(snapshot.title_names order by revision.activated_at desc nulls last)->0),
		(select count(distinct recipe_snapshot.recipe_id)::int from latest_revisions source
		 join recipe_import_snapshots recipe_snapshot on recipe_snapshot.revision_id=source.id
		 join recipes recipe on recipe.entity_id=recipe_snapshot.recipe_id where recipe.recipe_type_id=recipe_type.entity_id),
		coalesce(jsonb_agg(distinct (catalyst.value || jsonb_build_object('revisionId',snapshot.revision_id)))
		 filter(where catalyst.value is not null),'[]'::jsonb),(array_agg(snapshot.revision_id order by revision.activated_at desc nulls last))[1]
		from latest_revisions revision join recipe_type_import_snapshots snapshot on snapshot.revision_id=revision.id
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
