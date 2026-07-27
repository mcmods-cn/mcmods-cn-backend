package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

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
	checkpoint := time.Now()
	mark := func(label string) {
		t.Helper()
		now := time.Now()
		t.Logf("%s: %s", label, now.Sub(checkpoint).Round(time.Millisecond))
		checkpoint = now
	}
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
	// Production connections enforce an idle-in-transaction timeout. Keep a
	// deliberately tighter value here so bulk imports must stream rows while
	// PostgreSQL is actively processing COPY instead of encoding giant array
	// parameters while the transaction sits idle.
	if _, err = tx.Exec(context.Background(), `set local idle_in_transaction_session_timeout='15s'`); err != nil {
		t.Fatal(err)
	}
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
	var versionID int64
	var versionPublicID string
	if err = tx.QueryRow(context.Background(), `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status)
		values($1,'1.20.1 / forge',array['1.20.1'],array['forge'],'active') returning id,public_id`, modID).Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(context.Background(), `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status)
		values($1,$2,$3,$4,$5,'ready')`, jobID, modID, packageID, versionID, modExportImporterVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(context.Background(), `insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active)
		values($1,$2,$3,$4,$5,1,'ready','1.20.1','forge','0.6.0','minecraft',false)`, revisionID, modID, packageID, jobID, versionID); err != nil {
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
	mark("recipe types and tags")
	supportedTranslations := make(map[string]map[string]string, len(exportContentLocaleByCode))
	seenLocales := make(map[string]string)
	allLocaleCount := 0
	expectedTranslationValues := 0
	compressedTranslationBytes := int64(0)
	for name, file := range files {
		if !isSupportedExportTranslationFile(name) {
			continue
		}
		value, readErr := readExportZIPFile(file, maxExportJSONSize)
		if readErr != nil {
			t.Fatal(readErr)
		}
		translation, prepareErr := prepareExportTranslation(name, value)
		if prepareErr != nil {
			t.Fatal(prepareErr)
		}
		if previousFile, exists := seenLocales[translation.Locale]; exists {
			t.Fatalf("translation files %s and %s resolve to the same locale %s", previousFile, name, translation.Locale)
		}
		seenLocales[translation.Locale] = name
		allLocaleCount++
		expectedTranslationValues += len(translation.Values)
		if _, editable := supportedEditableContentLocales[translation.Locale]; editable {
			supportedTranslations[translation.Locale] = translation.Values
		}
		bundles, compressed, bundleErr := prepareExportLocaleBundles(
			ossConfigPayload{Prefix: "mcmods"},
			"prj_test",
			map[string]string{"minecraft": revisionID},
			translation,
		)
		if bundleErr != nil || len(bundles) != 1 {
			t.Fatalf("prepare locale bundle %s: bundles=%d error=%v", translation.Locale, len(bundles), bundleErr)
		}
		compressedTranslationBytes += int64(len(compressed))
	}
	if len(supportedTranslations) != len(exportContentLocaleByCode) {
		t.Fatalf("expected %d supported in-memory locales, got %d", len(exportContentLocaleByCode), len(supportedTranslations))
	}
	if allLocaleCount <= len(supportedTranslations) {
		t.Fatalf("expected cold locale bundles beyond the %d editable locales, got %d total", len(supportedTranslations), allLocaleCount)
	}
	t.Logf("exported_locales=%d in_memory_editable_locales=%d translation_values=%d compressed_bundle_bytes=%d",
		allLocaleCount, len(supportedTranslations), expectedTranslationValues, compressedTranslationBytes)
	mark("supported translations")
	fallbackAssetPaths, err := normalizedExportFallbackAssetPaths(files)
	if err != nil {
		t.Fatal(err)
	}
	batch := newModExportWriteBatch()
	registryResourceCount := 0
	textAssetCount := 0
	for name, file := range files {
		if !isExportRegistryFile(name) {
			continue
		}
		value, readErr := readExportZIPFile(file, maxExportJSONSize)
		if readErr != nil {
			t.Fatal(readErr)
		}
		rows, prepareErr := prepareExportRegistryResources(catalogResourceIdentityResolver{}, revisions, name, value)
		if prepareErr != nil {
			t.Fatal(prepareErr)
		}
		for _, row := range rows {
			queueCatalogResource(batch, row)
		}
		registryResourceCount += len(rows)
		if batch.shouldFlush() {
			if err = batch.flush(context.Background(), tx); err != nil {
				t.Fatal(err)
			}
		}
	}
	documentResourceCount := 0
	for name, file := range files {
		if exportDocumentKind(name) == "" {
			continue
		}
		value, readErr := readExportZIPFile(file, maxExportJSONSize)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var document map[string]any
		if err = json.Unmarshal(value, &document); err != nil {
			t.Fatal(err)
		}
		documentResourceCount += len(exportDocumentEntries(name, document))
		if err = queueExportDocumentEntries(batch, catalogResourceIdentityResolver{}, revisionID, name, value); err != nil {
			t.Fatal(err)
		}
		if retainExportTextAsset(name, fallbackAssetPaths) {
			if err = queueExportTextAsset(batch, revisionID, name, value); err != nil {
				t.Fatal(err)
			}
			textAssetCount++
		}
	}
	if err = batch.flush(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	var persistedTextAssetCount int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from catalog_import_text_assets where revision_id=$1`, revisionID).Scan(&persistedTextAssetCount); err != nil {
		t.Fatal(err)
	}
	if persistedTextAssetCount != textAssetCount {
		t.Fatalf("expected %d text assets, got %d", textAssetCount, persistedTextAssetCount)
	}
	t.Logf(
		"staged registry_resources=%d document_resources=%d text_assets=%d catalog_duration=%s text_asset_duration=%s",
		registryResourceCount, documentResourceCount, textAssetCount,
		batch.catalogDuration.Round(time.Millisecond), batch.textAssetDuration.Round(time.Millisecond),
	)
	mark("catalog document resources")
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
	mark("recipe templates")
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
	t.Logf("batch flushes=%d payload_bytes=%d queued=%s catalog=%s text_assets=%s recipes=%s bindings=%s expected_recipes=%d staged_recipes=%d staged_bindings=%d staged_candidates=%d",
		batch.flushCount, batch.payloadBytes, batch.queuedDuration.Round(time.Millisecond), batch.catalogDuration.Round(time.Millisecond),
		batch.textAssetDuration.Round(time.Millisecond), batch.recipeDuration.Round(time.Millisecond), batch.bindingDuration.Round(time.Millisecond), expectedRecipeCount,
		batch.recipeCount, batch.bindingCount, batch.candidateCount)
	mark("recipe rows and bindings")
	blockEntityModels, err := deriveModExportBlockEntityModels(files, catalogResourceIdentityResolver{}, revisions)
	if err != nil {
		t.Fatal(err)
	}
	blockBindings, err := deriveModExportBlockBindings(files, catalogResourceIdentityResolver{}, revisions)
	if err != nil {
		t.Fatal(err)
	}
	if err = persistModExportBlockEntityModels(context.Background(), tx, blockEntityModels); err != nil {
		t.Fatal(err)
	}
	if err = persistModExportBlockBindings(context.Background(), tx, blockBindings); err != nil {
		t.Fatal(err)
	}
	var persistedBlockEntityModels int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from block_entity_model_snapshots where revision_id=$1`, revisionID).Scan(&persistedBlockEntityModels); err != nil {
		t.Fatal(err)
	}
	if persistedBlockEntityModels != len(blockEntityModels) {
		t.Fatalf("expected %d block entity models, got %d", len(blockEntityModels), persistedBlockEntityModels)
	}
	var persistedBlockBindings int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from game_resource_asset_bindings binding
		join resource_import_snapshots snapshot on snapshot.id=binding.snapshot_id
		where snapshot.revision_id=$1`, revisionID).Scan(&persistedBlockBindings); err != nil {
		t.Fatal(err)
	}
	if persistedBlockBindings != len(blockBindings) {
		t.Fatalf("expected %d block bindings, got %d", len(blockBindings), persistedBlockBindings)
	}
	t.Logf("block_entity_models=%d block_bindings=%d", len(blockEntityModels), len(blockBindings))
	mark("block model and asset bindings")
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
	supportedTranslations["en-US"]["effect.minecraft.speed"] = "Speed"
	supportedTranslations["zh-CN"]["effect.minecraft.speed"] = "速度"
	if err = importExportIconCatalogResources(context.Background(), tx, catalogResourceIdentityResolver{}, []modExportPNGMedia{
		{RevisionID: revisionID, AssetPath: "icons/mob_effects/32/minecraft/speed.png"},
		{RevisionID: revisionID, AssetPath: "icons/mob_effects/256/minecraft/speed.png"},
	}, supportedTranslations); err != nil {
		t.Fatal(err)
	}
	if err = syncImportedResourcesToContentVersionTx(context.Background(), tx, []string{revisionID}, versionID, false, 0); err != nil {
		t.Fatalf("sync imported resources into the selected content version: %v", err)
	}
	mark("promotion and content-version sync")
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
	var itemBlockRootsWithSystemCategories int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from (
		select root.id
		from mod_content_sections root
		join mod_content_templates template on template.id=root.template_id
		join mod_content_sections child on child.parent_id=root.id
		  and child.version_id=root.version_id and child.template_id=root.template_id
		  and child.status='active'
		where root.version_id=$1 and root.parent_id is null and root.status='active'
		  and template.code='item_block' and template.builtin
		group by root.id
		having count(*) filter(where child.system_key='items')=1
		   and count(*) filter(where child.system_key='blocks')=1
	) valid_roots`, versionID).
		Scan(&itemBlockRootsWithSystemCategories); err != nil {
		t.Fatal(err)
	}
	if itemBlockRootsWithSystemCategories != 1 {
		t.Fatalf("expected exactly one item/block root with real items and blocks child categories, got %d", itemBlockRootsWithSystemCategories)
	}
	var misplacedItemBlockResources int
	if err = tx.QueryRow(context.Background(), `select count(*)::int
		from mod_content_section_resources placement
		join mod_content_sections section on section.id=placement.section_id
		join mod_content_templates template on template.id=section.template_id
		where placement.version_id=$1 and template.code='item_block'
		  and section.system_key not in ('items','blocks')`, versionID).Scan(&misplacedItemBlockResources); err != nil {
		t.Fatal(err)
	}
	if misplacedItemBlockResources != 0 {
		t.Fatalf("expected every imported item/block placement under its real child category, got %d misplaced rows", misplacedItemBlockResources)
	}
	var duplicatedItemBlockIDs int
	if err = tx.QueryRow(context.Background(), `select count(*)::int from (
		select resource.canonical_id
		from mod_content_section_resources placement
		join mod_content_sections section on section.id=placement.section_id
		join mod_content_templates template on template.id=section.template_id
		join game_resources resource on resource.entity_id=placement.resource_id
		where placement.version_id=$1 and template.code='item_block'
		group by resource.canonical_id having count(*)>1
	) duplicates`, versionID).Scan(&duplicatedItemBlockIDs); err != nil {
		t.Fatal(err)
	}
	if duplicatedItemBlockIDs != 0 {
		t.Fatalf("item/block content still contains %d duplicated canonical IDs", duplicatedItemBlockIDs)
	}
	var boundItemAliases int
	if err = tx.QueryRow(context.Background(), `select count(*)::int
		from mod_content_section_resources placement
		join game_resource_asset_bindings binding on binding.item_resource_id=placement.resource_id
		join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
		where placement.version_id=$1 and block_snapshot.revision_id=$2
		  and binding.block_resource_id is not null
		  and binding.block_resource_id is distinct from binding.item_resource_id`,
		versionID, revisionID).Scan(&boundItemAliases); err != nil {
		t.Fatal(err)
	}
	if boundItemAliases != 0 {
		t.Fatalf("item/block content still contains %d item aliases that have authoritative block bindings", boundItemAliases)
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
		var protectedInternalID int64
		var protectedPublicID string
		if err = tx.QueryRow(context.Background(), `select id,public_id from catalog_entities where identity_key=$1`, protectedIdentity.ID).
			Scan(&protectedInternalID, &protectedPublicID); err != nil {
			t.Fatal(err)
		}
		var beforeWidth int
		var beforeDefinition string
		if err = tx.QueryRow(context.Background(), `select canvas_width,definition::text from recipe_layout_templates where entity_id=$1`, protectedInternalID).
			Scan(&beforeWidth, &beforeDefinition); err != nil {
			t.Fatal(err)
		}
		var nextRevisionNumber, humanRevisionID int64
		if err = tx.QueryRow(context.Background(), `select coalesce(max(revision_no),0)+1 from content_revisions
			where aggregate_type=$1 and aggregate_key=$2`, catalogAggregateRecipeTemplate, protectedPublicID).Scan(&nextRevisionNumber); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRow(context.Background(), `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,
			snapshot,snapshot_hash,source) values('recipe_template',$1,$2,$3,$4,'{}'::jsonb,$5,'user') returning id`, protectedInternalID,
			catalogAggregateRecipeTemplate, protectedPublicID, nextRevisionNumber, "integration-human-template").Scan(&humanRevisionID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(context.Background(), `update catalog_entities set published_revision_id=$2 where id=$1`, protectedInternalID, humanRevisionID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(context.Background(), `update recipe_layout_templates set published_revision_id=$2 where entity_id=$1`, protectedInternalID, humanRevisionID); err != nil {
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
			join recipe_template_import_snapshots snapshot on snapshot.id=template.import_snapshot_id where template.entity_id=$1`, protectedInternalID).
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
			or recipe.entity_id is null)::int
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
	mark("assertions")
}
