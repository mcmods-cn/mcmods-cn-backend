package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

const maxModExportReviewNoteBytes = 4096

type modExportRevisionSummary struct {
	ID                    string         `json:"id"`
	RevisionNo            int64          `json:"revisionNo"`
	Status                string         `json:"status"`
	MinecraftVersion      string         `json:"minecraftVersion"`
	Loader                string         `json:"loader"`
	ExporterVersion       string         `json:"exporterVersion"`
	SourceKind            string         `json:"sourceKind"`
	Namespace             string         `json:"namespace"`
	TargetVersionPublicID string         `json:"targetVersionPublicId"`
	IsActive              bool           `json:"isActive"`
	RegistryCounts        map[string]int `json:"registryCounts"`
	DocumentCounts        map[string]int `json:"documentCounts"`
	AssetCount            int            `json:"assetCount"`
	StructureCount        int            `json:"structureCount"`
	AdvancementCount      int            `json:"advancementCount"`
	KeyMappingCount       int            `json:"keyMappingCount"`
	RecipeCount           int            `json:"recipeCount"`
	TagCount              int            `json:"tagCount"`
	Capabilities          map[string]any `json:"capabilities"`
	CreatedAt             time.Time      `json:"createdAt"`
}

func (s *Server) modExportDataSummary(w http.ResponseWriter, r *http.Request) {
	identity, err := s.modIdentity(r.Context(), r.PathValue("siteId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod")
		return
	}
	rows, err := s.db.Query(r.Context(),
		`select r.id,r.revision_no,r.status,r.minecraft_version,r.loader,r.exporter_version,r.source_kind,r.source_namespace,version.public_id,
		 (r.is_active and version.status='active'),r.created_at,
		 coalesce(stats.registry_counts,'{}'::jsonb),coalesce(stats.document_counts,'{}'::jsonb),
		 coalesce(stats.asset_count,0),coalesce(stats.structure_count,0),coalesce(stats.advancement_count,0),
		 coalesce(stats.key_mapping_count,0),coalesce(stats.recipe_count,0),coalesce(stats.tag_count,0),
		 coalesce(stats.capability_statuses,'{}'::jsonb)
		 from catalog_import_revisions r
		 join mod_content_versions version on version.id=r.target_version_id
		 left join catalog_import_revision_stats stats on stats.revision_id=r.id
		 where r.mod_id=$1 and r.is_active and version.status='active'
		 order by r.minecraft_version desc,r.loader,r.source_kind,r.source_namespace,r.revision_no desc`, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read export revisions")
		return
	}
	defer rows.Close()
	items := make([]modExportRevisionSummary, 0)
	for rows.Next() {
		var item modExportRevisionSummary
		var counts, documentCounts, capabilities []byte
		if err = rows.Scan(&item.ID, &item.RevisionNo, &item.Status, &item.MinecraftVersion, &item.Loader, &item.ExporterVersion, &item.SourceKind, &item.Namespace, &item.TargetVersionPublicID, &item.IsActive, &item.CreatedAt, &counts, &documentCounts, &item.AssetCount, &item.StructureCount, &item.AdvancementCount, &item.KeyMappingCount, &item.RecipeCount, &item.TagCount, &capabilities); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export revision")
			return
		}
		if err = decodeModExportJSON(counts, &item.RegistryCounts, "revision registry counts"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export revision")
			return
		}
		if err = decodeModExportJSON(documentCounts, &item.DocumentCounts, "revision document counts"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export revision")
			return
		}
		if err = decodeModExportJSON(capabilities, &item.Capabilities, "revision capability statuses"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export revision")
			return
		}
		if item.RegistryCounts == nil || item.DocumentCounts == nil || item.Capabilities == nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export revision")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read export revisions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) modExportRegistry(w http.ResponseWriter, r *http.Request) {
	if !s.canReadModExportRevision(w, r, r.PathValue("revisionId")) {
		return
	}
	registry := strings.ToLower(strings.TrimSpace(r.PathValue("registry")))
	rows, err := s.db.Query(r.Context(), `select entity.public_id,resource.canonical_id,
		resource.namespace,resource.resource_path,snapshot.translation_key,snapshot.names,snapshot.data,
		snapshot.icon_path,snapshot.preview_path
		from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join catalog_entities entity on entity.id=resource.entity_id
		where snapshot.revision_id=$1 and snapshot.registry=$2
		order by resource.canonical_id`, r.PathValue("revisionId"), registry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read registry")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, objectID, namespace, objectPath, translationKey, iconPath, previewPath string
		var names, data []byte
		if err = rows.Scan(&publicID, &objectID, &namespace, &objectPath, &translationKey, &names, &data, &iconPath, &previewPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode registry")
			return
		}
		namesValue, decodeErr := decodeModExportJSONObjectValue(names, "registry entry names")
		if decodeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode registry")
			return
		}
		dataValue, decodeErr := decodeModExportJSONObjectValue(data, "registry entry data")
		if decodeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode registry")
			return
		}
		item := map[string]any{"publicId": publicID, "id": objectID, "registry": registry,
			"namespace": namespace, "path": objectPath, "translationKey": translationKey,
			"iconPath": iconPath, "previewPath": previewPath, "names": namesValue, "data": dataValue}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read registry")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"registry": registry, "items": items})
}

func (s *Server) modExportRegistryEntries(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	registries := make([]string, 0, 8)
	seen := make(map[string]bool)
	for _, value := range strings.Split(r.URL.Query().Get("registries"), ",") {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] || !isExportRegistryName(value) {
			continue
		}
		seen[value] = true
		registries = append(registries, value)
		if len(registries) == 8 {
			break
		}
	}
	if len(registries) == 0 {
		writeError(w, http.StatusBadRequest, "at least one registry is required")
		return
	}
	canonicalItemsAndBlocks := seen["items"] && seen["blocks"]
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 80, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	summaryOnly := r.URL.Query().Get("summary") == "1"
	var total int
	err := s.db.QueryRow(r.Context(), `
		select count(*)::int
		from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		where snapshot.revision_id=$1 and snapshot.registry=any($2::text[])
		  and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')
		  and (not $4 or snapshot.registry<>'items' or not exists(
			select 1 from game_resource_asset_bindings binding
			join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
			where binding.item_resource_id=resource.entity_id and block_snapshot.revision_id=snapshot.revision_id
		  ))`,
		revisionID, registries, query, canonicalItemsAndBlocks).Scan(&total)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count registry entries")
		return
	}
	if summaryOnly {
		s.writeModExportRegistryEntrySummaries(w, r, revisionID, registries, query, locale, total, limit, offset, canonicalItemsAndBlocks)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		select entity.public_id,resource.canonical_id,snapshot.registry,
			resource.namespace,resource.resource_path,snapshot.translation_key,
			snapshot.names,
			snapshot.data-'names',snapshot.icon_path,snapshot.preview_path
		from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join catalog_entities entity on entity.id=resource.entity_id
		where snapshot.revision_id=$1 and snapshot.registry=any($2::text[])
		  and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')
		  and (not $6 or snapshot.registry<>'items' or not exists(
			select 1 from game_resource_asset_bindings binding
			join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
			where binding.item_resource_id=resource.entity_id and block_snapshot.revision_id=snapshot.revision_id
		  ))
		order by resource.canonical_id,snapshot.registry limit $4 offset $5`,
		revisionID, registries, query, limit, offset, canonicalItemsAndBlocks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read registry entries")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var publicID, objectID, registry, namespace, objectPath, translationKey, iconPath, previewPath string
		var names, data []byte
		if err = rows.Scan(&publicID, &objectID, &registry, &namespace, &objectPath, &translationKey, &names, &data, &iconPath, &previewPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode registry entry")
			return
		}
		namesValue, decodeErr := decodeModExportJSONObjectValue(names, "registry entry names")
		if decodeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode registry entry")
			return
		}
		dataValue, decodeErr := decodeModExportJSONObjectValue(data, "registry entry data")
		if decodeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode registry entry")
			return
		}
		items = append(items, map[string]any{
			"publicId": publicID, "id": objectID, "registry": registry, "namespace": namespace, "path": objectPath,
			"translationKey": translationKey, "iconPath": iconPath, "previewPath": previewPath, "names": namesValue, "data": dataValue,
		})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read registry entries")
		return
	}
	if err = s.decorateExportTranslationNames(r.Context(), revisionID, locale, items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve registry translations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) writeModExportRegistryEntrySummaries(w http.ResponseWriter, r *http.Request, revisionID string, registries []string, query, locale string, total, limit, offset int, canonicalItemsAndBlocks bool) {
	cacheKey := fmt.Sprintf("export-registry-summary:v6:%s:%s:%s:%s:%d:%d:%t", revisionID, strings.Join(registries, ","), query, locale, limit, offset, canonicalItemsAndBlocks)
	payload, err := s.cache.GetOrLoad(r.Context(), cacheKey, func(ctx context.Context) ([]byte, error) {
		rows, loadErr := s.db.Query(ctx, `select entity.public_id,resource.canonical_id,snapshot.registry,
		resource.namespace,resource.resource_path,snapshot.translation_key,
		snapshot.names,
		'{}'::jsonb,snapshot.icon_path,snapshot.preview_path
		from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join catalog_entities entity on entity.id=resource.entity_id
		where snapshot.revision_id=$1 and snapshot.registry=any($2::text[])
		  and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')
		  and (not $6 or snapshot.registry<>'items' or not exists(
			select 1 from game_resource_asset_bindings binding
			join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
			where binding.item_resource_id=resource.entity_id and block_snapshot.revision_id=snapshot.revision_id
		  ))
		order by resource.canonical_id,snapshot.registry limit $4 offset $5`, revisionID, registries, query, limit, offset, canonicalItemsAndBlocks)
		if loadErr != nil {
			return nil, loadErr
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		for rows.Next() {
			var publicID, objectID, registry, namespace, objectPath, translationKey, iconPath, previewPath string
			var names, data []byte
			if loadErr = rows.Scan(&publicID, &objectID, &registry, &namespace, &objectPath, &translationKey, &names, &data, &iconPath, &previewPath); loadErr != nil {
				return nil, loadErr
			}
			namesValue, decodeErr := decodeModExportJSONObjectValue(names, "registry summary names")
			if decodeErr != nil {
				return nil, decodeErr
			}
			dataValue, decodeErr := decodeModExportJSONObjectValue(data, "registry summary data")
			if decodeErr != nil {
				return nil, decodeErr
			}
			items = append(items, map[string]any{"publicId": publicID, "id": objectID, "registry": registry, "namespace": namespace, "path": objectPath,
				"translationKey": translationKey, "iconPath": iconPath, "previewPath": previewPath, "names": namesValue, "data": dataValue})
		}
		if loadErr = rows.Err(); loadErr != nil {
			return nil, loadErr
		}
		if loadErr = s.decorateExportTranslationNames(ctx, revisionID, locale, items); loadErr != nil {
			return nil, loadErr
		}
		return json.Marshal(apiResponse{Data: map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}})
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read registry entry summaries")
		return
	}
	setPublicCacheControlIfAllowed(w, "public, max-age=60, stale-while-revalidate=120")
	writeJSONBytes(w, http.StatusOK, payload)
}

func (s *Server) modExportDocumentEntries(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if !isExportDocumentKind(kind) {
		writeError(w, http.StatusBadRequest, "unsupported document entry kind")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 80, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	summaryOnly := r.URL.Query().Get("summary") == "1"
	cacheKey := fmt.Sprintf("export-document:v10:%s:%s:%s:%s:%d:%d:%t", revisionID, kind, query, locale, limit, offset, summaryOnly)
	payload, err := s.cache.GetOrLoad(r.Context(), cacheKey, func(ctx context.Context) ([]byte, error) {
		var total int
		if loadErr := s.db.QueryRow(ctx, `select count(*)::int
			from resource_import_snapshots snapshot join game_resources resource on resource.entity_id=snapshot.resource_id
			where snapshot.revision_id=$1 and snapshot.registry=$2
			and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')`,
			revisionID, kind, query).Scan(&total); loadErr != nil {
			return nil, loadErr
		}
		rows, loadErr := s.db.Query(ctx, `select entity.public_id,resource.canonical_id,
			snapshot.names,resource.namespace,snapshot.translation_key,snapshot.icon_path,snapshot.preview_path,
			case when not $6 then snapshot.data
				when snapshot.registry='advancements' then jsonb_strip_nulls(jsonb_build_object('parent',coalesce(snapshot.data->'parentId',snapshot.data->'parent'),'display',snapshot.data->'display'))
				when snapshot.registry='loot_tables' then jsonb_strip_nulls(jsonb_build_object(
					'category',snapshot.data->'category','path',snapshot.data->'path','possible_item_ids',snapshot.data->'possibleItemIds'))
				when snapshot.registry='natural_generation' then jsonb_strip_nulls(jsonb_build_object(
					'entry_kind',snapshot.data->'entryKind','category',snapshot.data->'category',
					'feature_type',snapshot.data->'featureType','carver_type',snapshot.data->'carverType',
					'normalization_status',snapshot.data->'normalizationStatus','outputs',snapshot.data->'outputs',
					'generation_steps',snapshot.data->'generationSteps'))
				when snapshot.registry='world_structures' then jsonb_strip_nulls(jsonb_build_object(
					'catalog_kind',snapshot.data->'catalogKind','structure_type',snapshot.data->'structureType',
					'generation_step',snapshot.data->'generationStep','biomes',snapshot.data->'biomes',
					'terrain_adaptation',snapshot.data->'terrainAdaptation','structure_set_ids',snapshot.data->'structureSetIds'))
				else '{}'::jsonb end
			from resource_import_snapshots snapshot
			join game_resources resource on resource.entity_id=snapshot.resource_id
			join catalog_entities entity on entity.id=resource.entity_id
			where snapshot.revision_id=$1 and snapshot.registry=$2
			and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')
			order by resource.canonical_id limit $4 offset $5`, revisionID, kind, query, limit, offset, summaryOnly)
		if loadErr != nil {
			return nil, loadErr
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		for rows.Next() {
			var publicID, id, namespace, translationKey, iconPath, previewPath string
			var names, data []byte
			if loadErr = rows.Scan(&publicID, &id, &names, &namespace, &translationKey, &iconPath, &previewPath, &data); loadErr != nil {
				return nil, loadErr
			}
			namesValue, decodeErr := decodeModExportJSONObjectValue(names, "document entry names")
			if decodeErr != nil {
				return nil, decodeErr
			}
			dataValue, decodeErr := decodeModExportJSONObjectValue(data, "document entry data")
			if decodeErr != nil {
				return nil, decodeErr
			}
			items = append(items, map[string]any{
				"publicId": publicID, "id": id, "registry": kind, "namespace": namespace, "path": id,
				"translationKey": translationKey, "iconPath": iconPath, "previewPath": previewPath,
				"names": namesValue, "data": dataValue,
			})
		}
		if loadErr = rows.Err(); loadErr != nil {
			return nil, loadErr
		}
		if loadErr = s.decorateExportTranslationNames(ctx, revisionID, locale, items); loadErr != nil {
			return nil, loadErr
		}
		if kind == "loot_tables" {
			if loadErr = s.decorateLootTableResources(ctx, revisionID, items, locale); loadErr != nil {
				return nil, loadErr
			}
			if loadErr = s.decorateLootTableReferences(ctx, revisionID, items, locale); loadErr != nil {
				return nil, loadErr
			}
		}
		return json.Marshal(apiResponse{Data: map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}})
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read document entry index")
		return
	}
	setPublicCacheControlIfAllowed(w, "public, max-age=60, stale-while-revalidate=120")
	writeJSONBytes(w, http.StatusOK, payload)
}

func isExportDocumentKind(value string) bool {
	switch value {
	case "advancements", "key_mappings", "biomes", "dimensions", "natural_generation",
		"world_structures", "loot_tables", "ingredients", "worldgen_data":
		return true
	default:
		return false
	}
}

func isExportRegistryName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}

func boundedOffset(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0
	}
	return min(value, 1_000_000)
}

func (s *Server) modExportAssetIndex(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	pathsOnly := r.URL.Query().Get("pathsOnly") == "1"
	limit := boundedLimit(r.URL.Query().Get("limit"), 200, 500)
	scope := "assets:full"
	if pathsOnly {
		scope = "assets:paths"
	}
	cursor, err := decodeModExportPageCursor(r.URL.Query().Get("cursor"), revisionID, scope)
	if err != nil || cursor.Second != "" || (pathsOnly && cursor.Ordinal != 0) || (!pathsOnly && cursor.Ordinal > 2) {
		writeError(w, http.StatusBadRequest, "invalid asset cursor")
		return
	}
	rows, hasMore, err := loadModExportAssetPage(r.Context(), s.db, revisionID, pathsOnly, cursor.First, cursor.Ordinal, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read asset index")
		return
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeModExportPageCursor(modExportPageCursor{RevisionID: revisionID, Scope: scope, First: last.Path, Ordinal: last.SourceOrder})
	}
	if pathsOnly {
		items := make([]string, len(rows))
		for index, row := range rows {
			items[index] = row.Path
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "hasMore": hasMore, "nextCursor": nextCursor})
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"path": row.Path, "kind": row.Kind, "contentType": row.ContentType,
			"sha256": row.SHA256, "byteLength": row.ByteLength, "media": row.Media})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "hasMore": hasMore, "nextCursor": nextCursor})
}

func (s *Server) modExportAssetContent(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	assetPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if assetPath == "" {
		writeError(w, http.StatusBadRequest, "asset path is required")
		return
	}
	var contentType, text string
	var jsonContent, binary []byte
	err := s.db.QueryRow(r.Context(), `select content_type,coalesce(text_content,''),json_content::text from catalog_import_text_assets where revision_id=$1 and asset_path=$2`, revisionID, assetPath).Scan(&contentType, &text, &jsonContent)
	if err == nil {
		w.Header().Set("Content-Type", contentType)
		setPublicCacheControlIfAllowed(w, "public, max-age=31536000, immutable")
		if len(jsonContent) > 0 && string(jsonContent) != "null" {
			_, _ = w.Write(jsonContent)
		} else {
			_, _ = w.Write([]byte(text))
		}
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to read asset")
		return
	}
	err = s.db.QueryRow(r.Context(), `select content_type,data from catalog_import_binary_assets where revision_id=$1 and asset_path=$2`, revisionID, assetPath).Scan(&contentType, &binary)
	if err == nil {
		w.Header().Set("Content-Type", contentType)
		setPublicCacheControlIfAllowed(w, "public, max-age=31536000, immutable")
		_, _ = w.Write(binary)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to read asset")
		return
	}
	s.redirectModExportMedia(w, r, revisionID, assetPath)
}

func (s *Server) modExportStructures(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	rows, err := s.db.Query(r.Context(), `select id,structure_id,asset_path,source_format,summary from catalog_import_structures where revision_id=$1 order by structure_id`, revisionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read structures")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, structureID, assetPath, sourceFormat string
		var summary []byte
		if err = rows.Scan(&id, &structureID, &assetPath, &sourceFormat, &summary); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode structures")
			return
		}
		summaryValue, decodeErr := decodeModExportJSONObjectValue(summary, "structure summary")
		if decodeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode structures")
			return
		}
		item := map[string]any{"id": id, "structureId": structureID, "assetPath": assetPath, "sourceFormat": sourceFormat, "summary": summaryValue}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read structures")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) modExportStructureTemplate(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	var contentType, name string
	var data []byte
	err := s.db.QueryRow(r.Context(),
		`select b.content_type,b.asset_path,b.data from catalog_import_structures s join catalog_import_binary_assets b on b.id=s.template_blob_id where s.id=$1 and s.revision_id=$2`,
		r.PathValue("structureId"), revisionID).Scan(&contentType, &name, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "structure not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read structure")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename*=UTF-8''%s", url.PathEscape(name)))
	setPublicCacheControlIfAllowed(w, "public, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

func (s *Server) activateModExportRevision(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	var request struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid review request")
		return
	}
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.Note = strings.TrimSpace(request.Note)
	if request.Status != "approved" && request.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "review status must be approved or rejected")
		return
	}
	if len(request.Note) > maxModExportReviewNoteBytes {
		writeError(w, http.StatusBadRequest, "review note is too long")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin review")
		return
	}
	defer tx.Rollback(r.Context())
	var modID int64
	var namespace, sourceKind, status string
	var isActive bool
	var targetVersionID int64
	var overwriteExistingImportData bool
	err = tx.QueryRow(r.Context(), `select revision.mod_id,revision.source_namespace,revision.source_kind,revision.status,revision.is_active,revision.target_version_id,job.overwrite_existing
		from catalog_import_revisions revision join catalog_import_jobs job on job.id=revision.job_id where revision.id=$1 for update`, revisionID).
		Scan(&modID, &namespace, &sourceKind, &status, &isActive, &targetVersionID, &overwriteExistingImportData)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "export revision not found")
		return
	}
	if err != nil || (status != "ready" && status != "partial") {
		writeError(w, http.StatusConflict, "export revision is not ready")
		return
	}
	audit := modExportRevisionReviewAudit{
		RevisionID: revisionID, Decision: request.Status, Note: request.Note,
		BeforeStatus: status, BeforeActive: isActive, ModID: modID, TargetVersionID: targetVersionID,
		Namespace: namespace, SourceKind: sourceKind, ActorID: currentClaims(r).Subject,
		IP: s.requestClientLocation(r).IP, UserAgent: r.UserAgent(),
	}
	if request.Status == "rejected" {
		if _, err = tx.Exec(r.Context(), `update catalog_import_revisions set status='rejected',is_active=false where id=$1`, revisionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject export revision")
			return
		}
		audit.AfterStatus = "rejected"
		audit.AfterActive = false
		if err = recordModExportRevisionReviewTx(r.Context(), tx, audit); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record export review")
			return
		}
		if err = bumpCatalogDatasetVersionTx(r.Context(), tx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update catalog version")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit export review")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": revisionID, "status": "rejected", "isActive": false})
		return
	}
	if _, err = tx.Exec(r.Context(), `update catalog_import_revisions set is_active=false,status='superseded' where mod_id=$1 and target_version_id=$2 and source_namespace=$3 and source_kind=$4 and id<>$5 and is_active`, modID, targetVersionID, namespace, sourceKind, revisionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to supersede export revision")
		return
	}
	if _, err = tx.Exec(r.Context(), `update catalog_import_revisions set is_active=true,activated_at=now() where id=$1`, revisionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate export revision")
		return
	}
	if err = syncImportedResourcesToContentVersionTx(r.Context(), tx, []string{revisionID}, targetVersionID, overwriteExistingImportData, currentClaims(r).Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to merge imported data into the selected content version")
		return
	}
	if err = promoteImportedRecipeTemplatesTx(r.Context(), tx, revisionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to promote imported recipe templates")
		return
	}
	audit.AfterStatus = status
	audit.AfterActive = true
	if err = recordModExportRevisionReviewTx(r.Context(), tx, audit); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record export review")
		return
	}
	if err = bumpCatalogDatasetVersionTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update catalog version")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit export revision")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": revisionID, "status": status, "isActive": true})
}

type modExportRevisionReviewAudit struct {
	RevisionID      string
	Decision        string
	Note            string
	BeforeStatus    string
	BeforeActive    bool
	AfterStatus     string
	AfterActive     bool
	ModID           int64
	TargetVersionID int64
	Namespace       string
	SourceKind      string
	ActorID         int64
	IP              string
	UserAgent       string
}

func recordModExportRevisionReviewTx(ctx context.Context, tx pgx.Tx, audit modExportRevisionReviewAudit) error {
	_, err := tx.Exec(ctx, `insert into audit_events(
		aggregate_type,aggregate_key,actor_id,action,ip,user_agent,metadata)
		values('catalog_import_revision',$1,$2,$3,$4,$5,jsonb_build_object(
			'decision',$6::text,'note',$7::text,'beforeStatus',$8::text,'beforeActive',$9::boolean,
			'afterStatus',$10::text,'afterActive',$11::boolean,'modId',$12::bigint,'targetVersionId',$13::bigint,
			'namespace',$14::text,'sourceKind',$15::text))`,
		audit.RevisionID, audit.ActorID, "review_"+audit.Decision, audit.IP, audit.UserAgent,
		audit.Decision, audit.Note, audit.BeforeStatus, audit.BeforeActive,
		audit.AfterStatus, audit.AfterActive, audit.ModID, audit.TargetVersionID,
		audit.Namespace, audit.SourceKind)
	if err != nil {
		return fmt.Errorf("record mod export revision review: %w", err)
	}
	return nil
}

func (s *Server) canReadModExportRevision(w http.ResponseWriter, r *http.Request, revisionID string) bool {
	var identity modIdentityRecord
	var active bool
	err := s.db.QueryRow(r.Context(), `select m.id,m.project_code,m.slug,m.submitted_by,
		(e.is_active and version.status='active')
		from catalog_import_revisions e
		join mods m on m.id=e.mod_id
		join mod_content_versions version on version.id=e.target_version_id
		where e.id=$1`, revisionID).Scan(&identity.ID, &identity.UniqueID, &identity.SiteID, &identity.SubmittedByID, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "export revision not found")
		return false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read export revision")
		return false
	}
	claims := currentClaims(r)
	if !active && !canEditMod(claims, identity) && !claimsAllow(claims, "project.review") {
		writeError(w, http.StatusForbidden, "export revision is awaiting review")
		return false
	}
	if !active {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Add("Vary", "Authorization")
		w.Header().Add("Vary", "Cookie")
	}
	return true
}

func setPublicCacheControlIfAllowed(w http.ResponseWriter, value string) {
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", value)
	}
}

func (s *Server) redirectModExportMedia(w http.ResponseWriter, r *http.Request, revisionID, assetPath string) {
	var objectKey, contentType string
	var byteLength int64
	err := s.db.QueryRow(r.Context(), `select f.object_key,m.content_type,m.byte_length from catalog_import_media m join oss_files f on f.id=m.oss_file_id where m.revision_id=$1 and m.asset_path=$2 and f.status='active'`, revisionID, assetPath).Scan(&objectKey, &contentType, &byteLength)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read media asset")
		return
	}
	cfg := s.ossConfigFromSettings(r.Context())
	if r.URL.Query().Get("proxy") == "renderer" && isRendererTextureAsset(assetPath) {
		if byteLength > 4*1024*1024 {
			writeError(w, http.StatusRequestEntityTooLarge, "renderer texture exceeds proxy limit")
			return
		}
		client, clientErr := s.ossDownloadClient(r.Context(), cfg)
		if clientErr != nil {
			writeError(w, http.StatusServiceUnavailable, clientErr.Error())
			return
		}
		result, getErr := client.GetObject(r.Context(), &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
		if getErr != nil {
			writeError(w, http.StatusBadGateway, "failed to read renderer texture")
			return
		}
		defer result.Body.Close()
		w.Header().Set("Content-Type", contentType)
		setPublicCacheControlIfAllowed(w, "public, max-age=31536000, immutable")
		_, _ = io.Copy(w, io.LimitReader(result.Body, byteLength+1))
		return
	}
	s.redirectOSSObjectAccess(w, r, objectKey, ossObjectAccessOptions{})
}

func isRendererTextureAsset(assetPath string) bool {
	return strings.HasPrefix(assetPath, "assets/") && strings.Contains(assetPath, "/textures/") && strings.HasSuffix(strings.ToLower(assetPath), ".png")
}

func jsonValue(raw []byte) any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return map[string]any{}
	}
	return value
}
