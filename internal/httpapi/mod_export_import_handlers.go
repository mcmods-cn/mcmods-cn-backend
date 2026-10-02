package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"mime"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

func exportString(value any) string {
	result, _ := value.(string)
	return result
}

func exportIngredientKind(ingredientType string) string {
	value := strings.ToLower(strings.TrimSpace(ingredientType))
	switch {
	case value == "item_stack" || value == "minecraft:item_stack":
		return "item"
	case value == "fluid_stack" || strings.Contains(value, "fluidstack"):
		return "fluid"
	case strings.Contains(value, ".gas.") || strings.HasSuffix(value, "gasstack"):
		return "mekanism_gas"
	case strings.Contains(value, ".infuse.") || strings.Contains(value, "infusionstack"):
		return "mekanism_infuse_type"
	case strings.Contains(value, ".pigment.") || strings.HasSuffix(value, "pigmentstack"):
		return "mekanism_pigment"
	case strings.Contains(value, ".slurry.") || strings.HasSuffix(value, "slurrystack"):
		return "mekanism_slurry"
	case value == "":
		return "unknown"
	default:
		return value
	}
}

func exportRecipeAmount(alternative map[string]any) float64 {
	for _, key := range []string{"count", "amount"} {
		if value, ok := alternative[key].(float64); ok && value > 0 {
			return value
		}
	}
	return 1
}

func nonEmptyJSON(value json.RawMessage, fallback string) string {
	if len(value) == 0 || string(value) == "null" {
		return fallback
	}
	return string(value)
}

func validateExportZIP(files []*zip.File) (map[string]*zip.File, error) {
	if len(files) == 0 || len(files) > maxExportFileCount {
		return nil, errors.New("export ZIP file count is invalid")
	}
	result := make(map[string]*zip.File, len(files))
	seenFolded := make(map[string]struct{}, len(files))
	var total int64
	for _, file := range files {
		name, err := normalizeExportPath(file.Name)
		if err != nil {
			return nil, err
		}
		folded := strings.ToLower(name)
		if _, exists := seenFolded[folded]; exists {
			return nil, fmt.Errorf("duplicate ZIP path: %s", name)
		}
		seenFolded[folded] = struct{}{}
		if file.UncompressedSize64 > uint64(maxExportSingleFileSize) {
			return nil, fmt.Errorf("ZIP entry too large: %s", name)
		}
		total += int64(file.UncompressedSize64)
		if total > maxExportUncompressedSize {
			return nil, errors.New("export ZIP uncompressed size exceeds limit")
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > 1000 {
			return nil, fmt.Errorf("ZIP compression ratio is unsafe: %s", name)
		}
		result[name] = file
	}
	return result, nil
}

func normalizeExportPath(value string) (string, error) {
	if strings.ContainsRune(value, 0) || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || filepath.IsAbs(value) || strings.Contains(value, ":") {
		return "", fmt.Errorf("unsafe ZIP path: %q", value)
	}
	cleaned := path.Clean(strings.TrimPrefix(value, "./"))
	if cleaned == "." || cleaned == "" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("unsafe ZIP path: %q", value)
	}
	return cleaned, nil
}

func readExportZIPFile(file *zip.File, limit int64) ([]byte, error) {
	if limit < 0 || file.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("ZIP entry exceeds read limit")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("ZIP entry exceeds read limit")
	}
	return data, nil
}

func exportAssetFileNames(fileNames []string, files map[string]*zip.File) []string {
	result := make([]string, 0, len(fileNames))
	for _, name := range fileNames {
		file := files[name]
		if file == nil || file.FileInfo().IsDir() || name == "recipes/jei/categories.json" ||
			strings.HasPrefix(name, "recipes/jei/templates/") || strings.HasPrefix(name, "recipes/jei/recipes/") {
			continue
		}
		result = append(result, name)
	}
	return result
}

func exportReadBatchEnd(names []string, files map[string]*zip.File, start int) int {
	end := start
	var byteCount uint64
	for end < len(names) && end-start < maxExportReadBatchFiles {
		fileSize := files[names[end]].UncompressedSize64
		if end > start && (fileSize > uint64(maxExportReadBatchBytes) || byteCount > uint64(maxExportReadBatchBytes)-fileSize) {
			break
		}
		byteCount += fileSize
		end++
	}
	return max(start+1, end)
}

func readExportZIPFiles(ctx context.Context, files map[string]*zip.File, names []string) ([]modExportArchiveFile, error) {
	if len(names) == 0 {
		return nil, nil
	}
	workerCount := min(len(names), maxExportReadConcurrency)
	type readJob struct {
		index int
		name  string
	}
	jobs := make(chan readJob)
	results := make([]modExportArchiveFile, len(names))
	var waitGroup sync.WaitGroup
	var firstErr error
	var errorMutex sync.Mutex
	for range workerCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for job := range jobs {
				if context.Cause(ctx) != nil {
					continue
				}
				data, err := readExportZIPFile(files[job.name], maxExportSingleFileSize)
				if err != nil {
					errorMutex.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("read %s: %w", job.name, err)
					}
					errorMutex.Unlock()
					continue
				}
				results[job.index] = modExportArchiveFile{Name: job.name, Data: data}
			}
		}()
	}
	for index, name := range names {
		if context.Cause(ctx) != nil {
			break
		}
		jobs <- readJob{index: index, name: name}
	}
	close(jobs)
	waitGroup.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func isExportTranslationFile(name string) bool {
	return strings.HasPrefix(name, "translations/") && strings.HasSuffix(name, ".json") && path.Base(name) != "languages.json"
}

func isExportRegistryFile(name string) bool {
	return strings.HasPrefix(name, "registries/") && strings.HasSuffix(name, ".json")
}

type preparedExportTranslation struct {
	Name        string
	Locale      string
	Values      map[string]string
	SkippedKeys []string
	Total       int
}

func prepareExportTranslation(name string, raw []byte) (preparedExportTranslation, error) {
	sourceLocale := strings.TrimSuffix(path.Base(name), path.Ext(name))
	locale, valid := canonicalExportLocaleTag(sourceLocale)
	if !valid {
		return preparedExportTranslation{}, fmt.Errorf("invalid translation locale: %s", sourceLocale)
	}
	if supported, exists := exportContentLocale(sourceLocale); exists {
		locale = supported
	}
	prepared := preparedExportTranslation{
		Name:   name,
		Locale: locale,
	}
	values, skippedKeys, total, err := decodeExportTranslationValues(raw)
	if err != nil {
		return prepared, fmt.Errorf("decode translations %s: %w", name, err)
	}
	prepared.Values = values
	prepared.SkippedKeys = skippedKeys
	prepared.Total = total
	return prepared, nil
}

func decodeExportTranslationValues(raw []byte) (map[string]string, []string, int, error) {
	var rawValues map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawValues); err != nil {
		return nil, nil, 0, err
	}
	// Exporter v1 wraps the effective language map with locale/namespace metadata.
	// Older packages may contain the effective map directly, so both forms remain valid.
	if wrapped, hasWrapped := rawValues["translations"]; hasWrapped {
		if _, hasCount := rawValues["translation_count"]; hasCount {
			var translations map[string]json.RawMessage
			if err := json.Unmarshal(wrapped, &translations); err != nil {
				return map[string]string{}, []string{"translations"}, 1, nil
			}
			rawValues = translations
		}
	}
	values := make(map[string]string, len(rawValues))
	skippedKeys := make([]string, 0, 8)
	for key, rawValue := range rawValues {
		var value string
		if bytes.Equal(bytes.TrimSpace(rawValue), []byte("null")) || json.Unmarshal(rawValue, &value) != nil {
			if len(skippedKeys) < 8 {
				skippedKeys = append(skippedKeys, key)
			}
			continue
		}
		values[key] = value
	}
	sort.Strings(skippedKeys)
	return values, skippedKeys, len(rawValues), nil
}

func prepareExportRegistryResources(resolver catalogResourceIdentityResolver, revisions map[string]string, name string, raw []byte) ([]catalogResourceImportRow, error) {
	var document struct {
		Registry string            `json:"registry"`
		Entries  []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode registry %s: %w", name, err)
	}
	if document.Registry == "" {
		document.Registry = strings.TrimSuffix(path.Base(name), path.Ext(name))
	}
	rowsByKey := make(map[string]catalogResourceImportRow, len(document.Entries))
	for _, entryRaw := range document.Entries {
		var entry map[string]any
		if err := json.Unmarshal(entryRaw, &entry); err != nil {
			return nil, err
		}
		objectID, _ := entry["id"].(string)
		namespace, resourcePath, valid := exportSourceResourceParts(document.Registry, objectID, exportString(entry["namespace"]))
		if !valid {
			if document.Registry == "key_mappings" {
				return nil, fmt.Errorf("registry %s entry %q has invalid key-mapping namespace", document.Registry, objectID)
			}
			continue
		}
		revisionID, revisionErr := exportRevisionForNamespace(revisions, namespace)
		if revisionErr != nil {
			return nil, fmt.Errorf("registry %s entry %q: %w", document.Registry, objectID, revisionErr)
		}
		kindCode := resourceKindForRegistry(document.Registry)
		identity := resolveExportResourceIdentity(resolver, kindCode, document.Registry, objectID, namespace)
		translationKey, _ := entry["translation_key"].(string)
		names, _ := json.Marshal(supportedExportNames(entry["names"]))
		filterSupportedExportLocalizedFields(entry)
		filteredEntry, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if string(names) == "null" || len(names) == 0 {
			names = []byte("{}")
		}
		iconPath, previewPath := exportRegistryMediaPaths(document.Registry, namespace, resourcePath)
		rowsByKey[revisionID+"\x00"+kindCode+"\x00"+objectID] = catalogResourceImportRow{
			EntityID: identity.ID, PublicID: identity.PublicID, KindCode: kindCode, CanonicalID: identity.CanonicalID, RawID: identity.RawID,
			Namespace: identity.Namespace, ResourcePath: identity.ResourcePath, RevisionID: revisionID,
			SnapshotID: catalogSnapshotID("resource", revisionID, identity.ID, ""), Registry: document.Registry,
			TranslationKey: translationKey, Names: string(names), Data: string(compactImportJSONObject(
				filteredEntry,
				"id", "namespace", "path", "translation_key", "names",
			)),
			IconPath: iconPath, PreviewPath: previewPath,
		}
	}
	rowKeys := make([]string, 0, len(rowsByKey))
	for key := range rowsByKey {
		rowKeys = append(rowKeys, key)
	}
	sort.Strings(rowKeys)
	if len(rowKeys) == 0 {
		return nil, nil
	}
	rows := make([]catalogResourceImportRow, len(rowKeys))
	for index, key := range rowKeys {
		rows[index] = rowsByKey[key]
	}
	return rows, nil
}

func exportRegistryMediaPaths(registry, namespace, resourcePath string) (string, string) {
	switch registry {
	case "items", "blocks", "mob_effects":
		return fmt.Sprintf("icons/%s/32/%s/%s.png", registry, namespace, resourcePath),
			fmt.Sprintf("icons/%s/256/%s/%s.png", registry, namespace, resourcePath)
	case "entity_types":
		return fmt.Sprintf("entities/renders/32/%s/%s.png", namespace, resourcePath),
			fmt.Sprintf("entities/renders/256/%s/%s.png", namespace, resourcePath)
	default:
		return "", ""
	}
}

type exportTagRow struct {
	RevisionID string
	Registry   string
	TagID      string
	Members    []string
}

func persistExportTags(ctx context.Context, tx pgx.Tx, resolver catalogResourceIdentityResolver, rows []exportTagRow, memberCount int) error {
	if len(rows) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `create temporary table import_tag_stage(
		entity_id text,public_id text,snapshot_id text,revision_id text,registry text,canonical_id text,member_count integer
	) on commit drop`); err != nil {
		return err
	}
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"import_tag_stage"},
		[]string{"entity_id", "public_id", "snapshot_id", "revision_id", "registry", "canonical_id", "member_count"},
		pgx.CopyFromSlice(len(rows), func(index int) ([]any, error) {
			row := rows[index]
			identity := tagIdentity(row.Registry, row.TagID)
			return []any{identity.ID, identity.PublicID, catalogSnapshotID("tag", row.RevisionID, identity.ID, ""), row.RevisionID, row.Registry, row.TagID, len(row.Members)}, nil
		}))
	if err != nil {
		return err
	}
	if copied != int64(len(rows)) {
		return fmt.Errorf("copy tags: copied %d of %d rows", copied, len(rows))
	}
	if _, err = tx.Exec(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		select distinct entity_id,public_id,'tag','active' from import_tag_stage
		on conflict(identity_key) do update set status='active',archived_at=null,updated_at=now()
		where catalog_entities.status='placeholder'`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into catalog_tags(entity_id,registry,canonical_id)
		select distinct entity.id,stage.registry,stage.canonical_id from import_tag_stage stage
		join catalog_entities entity on entity.identity_key=stage.entity_id
		on conflict(registry,canonical_id) do nothing`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into tag_import_snapshots(id,tag_id,revision_id,member_count)
		select stage.snapshot_id,entity.id,stage.revision_id,stage.member_count from import_tag_stage stage
		join catalog_entities entity on entity.identity_key=stage.entity_id
		on conflict(tag_id,revision_id) do update set member_count=excluded.member_count`); err != nil {
		return err
	}
	if memberCount == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `create temporary table import_tag_member_stage(
		tag_snapshot_id text,resource_id text,resource_public_id text,kind_code text,raw_member_id text,canonical_id text,namespace text,resource_path text,ordinal integer
	) on commit drop`); err != nil {
		return err
	}
	rowIndex, memberIndex := 0, 0
	copied, err = tx.CopyFrom(ctx, pgx.Identifier{"import_tag_member_stage"},
		[]string{"tag_snapshot_id", "resource_id", "resource_public_id", "kind_code", "raw_member_id", "canonical_id", "namespace", "resource_path", "ordinal"},
		pgx.CopyFromFunc(func() ([]any, error) {
			for rowIndex < len(rows) && memberIndex >= len(rows[rowIndex].Members) {
				rowIndex++
				memberIndex = 0
			}
			if rowIndex >= len(rows) {
				return nil, nil
			}
			row := rows[rowIndex]
			tag := tagIdentity(row.Registry, row.TagID)
			memberID := row.Members[memberIndex]
			kindCode := resourceKindForRegistry(row.Registry)
			resource := resolver.resolve(kindCode, memberID)
			values := []any{catalogSnapshotID("tag", row.RevisionID, tag.ID, ""), resource.ID, resource.PublicID, kindCode, memberID, resource.CanonicalID, resource.Namespace, resource.ResourcePath, memberIndex}
			memberIndex++
			return values, nil
		}))
	if err != nil {
		return err
	}
	if copied != int64(memberCount) {
		return fmt.Errorf("copy tag members: copied %d of %d rows", copied, memberCount)
	}
	statements := []string{
		`insert into resource_kinds(code,family,user_visible)
		 select distinct kind_code,case when kind_code like 'import.document.%' then 'document' else split_part(kind_code,'.',1) end,true
		 from import_tag_member_stage on conflict(code) do nothing`,
		`insert into catalog_entities(identity_key,public_id,entity_type,status)
		 select distinct stage.resource_id,stage.resource_public_id,'resource','placeholder'
		 from import_tag_member_stage stage
		 left join game_resources existing
		  on existing.kind_code=stage.kind_code and existing.canonical_id=stage.canonical_id
		 where existing.entity_id is null
		 on conflict(identity_key) do nothing`,
		`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		 select distinct coalesce(existing.entity_id,entity.id),stage.kind_code,stage.canonical_id,stage.namespace,stage.resource_path,false
		 from import_tag_member_stage stage
		 left join game_resources existing
		  on existing.kind_code=stage.kind_code and existing.canonical_id=stage.canonical_id
		 left join catalog_entities entity on entity.identity_key=stage.resource_id
		 where existing.entity_id is not null or entity.id is not null
		 on conflict(kind_code,canonical_id) do nothing`,
		`insert into game_resource_aliases(kind_code,alias_id,resource_id,source)
		 select distinct stage.kind_code,stage.raw_member_id,resource.entity_id,'mod_id'
		 from import_tag_member_stage stage
		 join game_resources resource
		  on resource.kind_code=stage.kind_code and resource.canonical_id=stage.canonical_id
		 on conflict(kind_code,alias_id) do update set resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`,
		`insert into tag_import_members(tag_snapshot_id,resource_id,raw_member_id,ordinal)
		 select stage.tag_snapshot_id,resource.entity_id,stage.raw_member_id,stage.ordinal
		 from import_tag_member_stage stage
		 join game_resources resource
		  on resource.kind_code=stage.kind_code and resource.canonical_id=stage.canonical_id
		 on conflict(tag_snapshot_id,raw_member_id) do update set resource_id=excluded.resource_id,ordinal=excluded.ordinal`,
	}
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func decodeExportTags(revisions map[string]string, raw []byte) ([]exportTagRow, int, error) {
	var document struct {
		SchemaVersion string `json:"schema_version"`
		Registries    []struct {
			Registry string `json:"registry"`
			Tags     []struct {
				ID     string   `json:"id"`
				Values []string `json:"values"`
			} `json:"tags"`
		} `json:"registries"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, 0, fmt.Errorf("decode tags/tags.json: %w", err)
	}
	if document.SchemaVersion != "" && document.SchemaVersion != "mcmods-tags/v1" {
		return nil, 0, fmt.Errorf("unsupported tags schema: %s", document.SchemaVersion)
	}
	rows := make([]exportTagRow, 0)
	memberCount := 0
	for _, registry := range document.Registries {
		registryName := strings.ToLower(strings.TrimSpace(registry.Registry))
		if registryName == "" {
			continue
		}
		for _, tag := range registry.Tags {
			tagID := strings.TrimSpace(tag.ID)
			if tagID == "" {
				continue
			}
			members := uniqueExportResourceIDs(tag.Values)
			relevant := make(map[string]string)
			if revisionID := revisions[exportResourceNamespace(tagID)]; revisionID != "" {
				relevant[revisionID] = revisionID
			}
			for _, memberID := range members {
				if revisionID := revisions[exportResourceNamespace(memberID)]; revisionID != "" {
					relevant[revisionID] = revisionID
				}
			}
			for revisionID := range relevant {
				rows = append(rows, exportTagRow{RevisionID: revisionID, Registry: registryName, TagID: tagID, Members: members})
				memberCount += len(members)
				if len(rows) > maxExportTagCount || memberCount > maxExportTagMemberCount {
					return nil, 0, errors.New("export tag data exceeds limit")
				}
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RevisionID != rows[j].RevisionID {
			return rows[i].RevisionID < rows[j].RevisionID
		}
		if rows[i].Registry != rows[j].Registry {
			return rows[i].Registry < rows[j].Registry
		}
		return rows[i].TagID < rows[j].TagID
	})
	return rows, memberCount, nil
}

func uniqueExportResourceIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func exportResourceNamespace(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func newModExportWriteBatch() *modExportWriteBatch {
	return &modExportWriteBatch{batch: &pgx.Batch{}}
}

func (batch *modExportWriteBatch) queue(query string, byteCount int64, arguments ...any) {
	batch.batch.Queue(query, arguments...)
	batch.rows++
	batch.byteCount += byteCount
}

func (batch *modExportWriteBatch) shouldFlush() bool {
	bulkRows := len(batch.catalogRows) + len(batch.textAssets) + len(batch.recipes) + len(batch.recipeBindings) + len(batch.recipeCandidates)
	return batch.rows >= maxExportWriteBatchRows ||
		bulkRows >= maxExportBulkWriteRows ||
		batch.byteCount >= maxExportWriteBatchBytes
}

func (batch *modExportWriteBatch) flush(ctx context.Context, tx pgx.Tx) error {
	batch.flushCount++
	batch.recipeCount += len(batch.recipes)
	batch.bindingCount += len(batch.recipeBindings)
	batch.candidateCount += len(batch.recipeCandidates)
	batch.payloadBytes += batch.byteCount
	if batch.rows > 0 {
		started := time.Now()
		results := tx.SendBatch(ctx, batch.batch)
		var firstErr error
		for index := 0; index < batch.rows; index++ {
			if _, err := results.Exec(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := results.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if firstErr != nil {
			return firstErr
		}
		batch.queuedDuration += time.Since(started)
	}
	if len(batch.catalogRows) > 0 {
		started := time.Now()
		if err := persistCatalogResources(ctx, tx, batch.catalogRows); err != nil {
			return err
		}
		batch.catalogDuration += time.Since(started)
	}
	if len(batch.textAssets) > 0 {
		started := time.Now()
		if err := persistExportTextAssets(ctx, tx, batch.textAssets); err != nil {
			return err
		}
		batch.textAssetDuration += time.Since(started)
	}
	if len(batch.recipes) > 0 {
		if batch.onStage != nil {
			if err := batch.onStage("recipes"); err != nil {
				return err
			}
		}
		started := time.Now()
		if err := persistRecipeImports(ctx, tx, batch.recipes); err != nil {
			return err
		}
		batch.recipeDuration += time.Since(started)
	}
	if len(batch.recipeBindings) > 0 || len(batch.recipeCandidates) > 0 {
		started := time.Now()
		if err := persistRecipeImportBindings(ctx, tx, batch.recipeBindings, batch.recipeCandidates, batch.onStage); err != nil {
			return err
		}
		batch.bindingDuration += time.Since(started)
	}
	batch.batch = &pgx.Batch{}
	batch.catalogRows = batch.catalogRows[:0]
	batch.textAssets = batch.textAssets[:0]
	batch.recipes = batch.recipes[:0]
	batch.recipeBindings = batch.recipeBindings[:0]
	batch.recipeCandidates = batch.recipeCandidates[:0]
	batch.rows = 0
	batch.byteCount = 0
	return nil
}

func queueExportTextAsset(batch *modExportWriteBatch, revisionID, name string, data []byte) error {
	digest := sha256Hex(data)
	extension := strings.ToLower(path.Ext(name))
	contentType := mime.TypeByExtension(extension)
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	assetKind := strings.TrimPrefix(extension, ".")
	if extension == ".json" || extension == ".mcmeta" {
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("decode JSON asset %s: %w", name, err)
		}
		canonical, _ := json.Marshal(value)
		batch.textAssets = append(batch.textAssets, exportTextAssetWrite{
			RevisionID: revisionID, AssetPath: name, AssetKind: assetKind,
			ContentType: "application/json", Digest: digest, ByteLength: int64(len(data)),
			Content: string(canonical), JSON: true,
		})
		batch.byteCount += int64(len(canonical))
		return nil
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("text asset is not valid UTF-8: %s", name)
	}
	batch.textAssets = append(batch.textAssets, exportTextAssetWrite{
		RevisionID: revisionID, AssetPath: name, AssetKind: assetKind,
		ContentType: contentType, Digest: digest, ByteLength: int64(len(data)),
		Content: string(data),
	})
	batch.byteCount += int64(len(data))
	return nil
}

func queueExportBinary(batch *modExportWriteBatch, revisionID, name string, data []byte) error {
	assetID := newExportID()
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	batch.queue(`insert into catalog_import_binary_assets(id,revision_id,asset_path,asset_kind,sha256,byte_length,data) values($1,$2,$3,$4,$5,$6,$7)`, int64(len(data)), assetID, revisionID, name, extension, sha256Hex(data), len(data), data)
	if strings.Contains(strings.ToLower(name), "/structures/") || extension == "schem" || extension == "schematic" || extension == "litematic" {
		structureID := strings.TrimSuffix(strings.TrimPrefix(name, "data/"), path.Ext(name))
		batch.queue(`insert into catalog_import_structures(id,revision_id,structure_id,asset_path,source_format,template_blob_id) values($1,$2,$3,$4,$5,$6) on conflict(revision_id,structure_id) do nothing`, 0, newExportID(), revisionID, structureID, name, extension, assetID)
	}
	return nil
}

// Decode one import image at a time across workers: a legal image can reach
// the existing 100-million-pixel budget. Upload concurrency is independent.
var exportPNGDecodeSlots = make(chan struct{}, 1)

func inspectExportPNG(ctx context.Context, cfg ossConfigPayload, revisionID, uniqueID, name string, data []byte, resolver catalogResourceIdentityResolver) (modExportPNGMedia, error) {
	imageConfig, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || imageConfig.Width <= 0 || imageConfig.Height <= 0 || int64(imageConfig.Width)*int64(imageConfig.Height) > 100_000_000 {
		return modExportPNGMedia{}, fmt.Errorf("invalid or oversized PNG: %s", name)
	}
	if err = context.Cause(ctx); err != nil {
		return modExportPNGMedia{}, err
	}
	select {
	case exportPNGDecodeSlots <- struct{}{}:
		defer func() { <-exportPNGDecodeSlots }()
	case <-ctx.Done():
		return modExportPNGMedia{}, context.Cause(ctx)
	}
	if err = context.Cause(ctx); err != nil {
		return modExportPNGMedia{}, err
	}
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		return modExportPNGMedia{}, fmt.Errorf("invalid or truncated PNG: %s", name)
	}
	if err = context.Cause(ctx); err != nil {
		return modExportPNGMedia{}, err
	}
	digest := sha256Hex(data)
	return modExportPNGMedia{RevisionID: revisionID, AssetPath: name, ObjectKey: modExportResolvedMediaObjectKey(cfg.Prefix, uniqueID, revisionID, name, resolver), Digest: digest, ByteLength: int64(len(data)), Width: imageConfig.Width, Height: imageConfig.Height, Original: path.Base(name)}, nil
}

func modExportMediaObjectKey(prefix, projectUniqueID, revisionID, assetPath string) string {
	category := modExportMediaObjectCategory(projectUniqueID, revisionID, assetPath)
	return path.Join(ossObjectPrefix(prefix, category), modExportMediaObjectSuffix(assetPath))
}

func modExportResolvedMediaObjectKey(prefix, projectUniqueID, revisionID, assetPath string, resolver catalogResourceIdentityResolver) string {
	cleaned := sanitizeOSSAssetPath(assetPath)
	parts := strings.Split(cleaned, "/")
	if len(parts) >= 5 && parts[0] == "icons" {
		registry := parts[1]
		resourcePath := strings.TrimSuffix(path.Join(parts[4:]...), path.Ext(parts[len(parts)-1]))
		resolved := resolver.resolve(resourceKindForRegistry(registry), parts[3]+":"+resourcePath)
		if resolved.PublicID != "" {
			category := modExportMediaObjectCategory(projectUniqueID, revisionID, assetPath)
			return path.Join(ossObjectPrefix(prefix, category), resolved.PublicID+".png")
		}
	}
	return modExportMediaObjectKey(prefix, projectUniqueID, revisionID, assetPath)
}

func newModExportPNGUploadPool(ctx context.Context, client *aliyunoss.Client, bucket string) *modExportPNGUploadPool {
	return &modExportPNGUploadPool{ctx: ctx, client: client, bucket: bucket, semaphore: make(chan struct{}, maxExportPNGConcurrency)}
}

func (pool *modExportPNGUploadPool) submit(objectKey, name, digest string, data []byte) {
	pool.submitAsset(objectKey, name, digest, "image/png", data)
}

func (pool *modExportPNGUploadPool) submitAsset(objectKey, name, digest, contentType string, data []byte) {
	select {
	case pool.semaphore <- struct{}{}:
	case <-pool.ctx.Done():
		pool.setError(pool.ctx.Err())
		return
	}
	pool.waitGroup.Add(1)
	go func() {
		defer pool.waitGroup.Done()
		defer func() { <-pool.semaphore }()
		upload := func(ctx context.Context) error {
			_, err := pool.client.PutObject(ctx, &aliyunoss.PutObjectRequest{Bucket: aliyunoss.Ptr(pool.bucket), Key: aliyunoss.Ptr(objectKey), ContentType: aliyunoss.Ptr(contentType), ContentLength: aliyunoss.Ptr(int64(len(data))), Body: bytes.NewReader(data), Metadata: map[string]string{"sha256": digest}})
			return err
		}
		var err error
		if pool.uploadGuard != nil {
			err = pool.uploadGuard(pool.ctx, objectKey, upload)
		} else {
			err = upload(pool.ctx)
		}
		if err != nil {
			pool.setError(fmt.Errorf("upload import asset %s: %w", name, err))
		}
	}()
}

func (pool *modExportPNGUploadPool) setError(err error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if pool.firstErr == nil {
		pool.firstErr = err
	}
}

func (pool *modExportPNGUploadPool) wait() error {
	pool.waitGroup.Wait()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.firstErr
}

func persistExportPNGMedia(ctx context.Context, tx pgx.Tx, media []modExportPNGMedia) error {
	if len(media) == 0 {
		return nil
	}
	sort.Slice(media, func(left, right int) bool {
		if media[left].RevisionID == media[right].RevisionID {
			return media[left].AssetPath < media[right].AssetPath
		}
		return media[left].RevisionID < media[right].RevisionID
	})
	for _, item := range media {
		if item.FileID <= 0 {
			return fmt.Errorf("PNG artifact is not registered: %s", item.ObjectKey)
		}
	}
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"catalog_import_media"}, []string{"revision_id", "asset_path", "media_kind", "oss_file_id", "sha256", "content_type", "byte_length", "width", "height", "has_alpha"}, pgx.CopyFromSlice(len(media), func(index int) ([]any, error) {
		item := media[index]
		return []any{item.RevisionID, item.AssetPath, "png", item.FileID, item.Digest, "image/png", item.ByteLength, item.Width, item.Height, true}, nil
	}))
	if err != nil {
		return err
	}
	if copied != int64(len(media)) {
		return fmt.Errorf("copy PNG media: copied %d of %d rows", copied, len(media))
	}
	return nil
}

func exportRevisionForPath(revisions map[string]string, name string) string {
	parts := strings.Split(strings.ToLower(name), "/")
	for _, part := range parts {
		if revisionID := revisions[part]; revisionID != "" {
			return revisionID
		}
	}
	if len(revisions) == 1 {
		for _, revisionID := range revisions {
			return revisionID
		}
	}
	return ""
}

func sortedExportRevisionIDs(revisions map[string]string) []string {
	unique := make(map[string]struct{}, len(revisions))
	for _, revisionID := range revisions {
		if revisionID != "" {
			unique[revisionID] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for revisionID := range unique {
		result = append(result, revisionID)
	}
	sort.Strings(result)
	return result
}

func normalizeExportNamespaces(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		valid := true
		for _, char := range value {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' && char != '.' {
				valid = false
				break
			}
		}
		if valid {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func normalizeExportLoader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "fabric" && value != "forge" && value != "neoforge" {
		return "unknown"
	}
	return value
}

func newExportID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		// Import identifiers are visible outside the process. Continuing with a
		// timestamp fallback would make them predictable, so fail closed if the
		// operating system CSPRNG is unavailable.
		panic(fmt.Errorf("generate secure import identifier: %w", err))
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func nullableUserID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func (s *Server) notifyModExportResult(ctx context.Context, jobID, status string, failure error) {
	var recipientID int64
	var siteID, modName string
	var skipped int
	err := s.db.QueryRow(ctx,
		`select coalesce(j.created_by,0),m.slug,m.primary_name,coalesce((j.error_detail->>'translationValuesSkipped')::int,0)
		 from catalog_import_jobs j join mods m on m.id=j.mod_id where j.id=$1`, jobID,
	).Scan(&recipientID, &siteID, &modName, &skipped)
	if err != nil || recipientID <= 0 {
		return
	}
	code := "mod_import_success"
	values := map[string]string{"name": modName, "skipped": fmt.Sprintf("%d", skipped), "error": ""}
	if status == "partial" {
		code = "mod_import_partial"
	}
	if status == "failed" {
		code = "mod_import_failure"
		values["error"] = "未知错误"
		if failure != nil {
			values["error"] = failure.Error()
		}
	}
	s.sendTemplatedNotification(ctx, recipientID, code, values, map[string]any{
		"type": "mod_export_import", "jobId": jobID, "modSiteId": siteID, "status": status,
		"translationValuesSkipped": skipped, "targetLabel": modName, "url": "/mods/" + siteID,
	})
}
