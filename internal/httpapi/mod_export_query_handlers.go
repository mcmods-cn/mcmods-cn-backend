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

type modExportRevisionSummary struct {
	ID               string         `json:"id"`
	RevisionNo       int64          `json:"revisionNo"`
	Status           string         `json:"status"`
	MinecraftVersion string         `json:"minecraftVersion"`
	Loader           string         `json:"loader"`
	ExporterVersion  string         `json:"exporterVersion"`
	Namespace        string         `json:"namespace"`
	IsActive         bool           `json:"isActive"`
	RegistryCounts   map[string]int `json:"registryCounts"`
	DocumentCounts   map[string]int `json:"documentCounts"`
	AssetCount       int            `json:"assetCount"`
	StructureCount   int            `json:"structureCount"`
	AdvancementCount int            `json:"advancementCount"`
	KeyMappingCount  int            `json:"keyMappingCount"`
	RecipeCount      int            `json:"recipeCount"`
	TagCount         int            `json:"tagCount"`
	Capabilities     map[string]any `json:"capabilities"`
	CreatedAt        time.Time      `json:"createdAt"`
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
	claims := currentClaims(r)
	canSeePending := canEditMod(claims, identity) || hasPermission(claims.Permissions, "project.review")
	rows, err := s.db.Query(r.Context(),
		`select r.id,r.revision_no,r.status,r.minecraft_version,r.loader,r.exporter_version,r.source_namespace,r.is_active,r.created_at,
		 coalesce(stats.registry_counts,'{}'::jsonb),coalesce(stats.document_counts,'{}'::jsonb),
		 coalesce(stats.asset_count,0),coalesce(stats.structure_count,0),coalesce(stats.advancement_count,0),
		 coalesce(stats.key_mapping_count,0),coalesce(stats.recipe_count,0),coalesce(stats.tag_count,0),
		 coalesce(stats.capability_statuses,'{}'::jsonb)
		 from mod_export_revisions r left join mod_export_revision_stats stats on stats.revision_id=r.id
		 where r.mod_id=$1 and (r.is_active or $2)
		 order by r.minecraft_version desc,r.loader,r.source_namespace,r.revision_no desc`, identity.ID, canSeePending)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read export revisions")
		return
	}
	defer rows.Close()
	items := make([]modExportRevisionSummary, 0)
	for rows.Next() {
		var item modExportRevisionSummary
		var counts, documentCounts, capabilities []byte
		if err = rows.Scan(&item.ID, &item.RevisionNo, &item.Status, &item.MinecraftVersion, &item.Loader, &item.ExporterVersion, &item.Namespace, &item.IsActive, &item.CreatedAt, &counts, &documentCounts, &item.AssetCount, &item.StructureCount, &item.AdvancementCount, &item.KeyMappingCount, &item.RecipeCount, &item.TagCount, &capabilities); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export revision")
			return
		}
		_ = json.Unmarshal(counts, &item.RegistryCounts)
		_ = json.Unmarshal(documentCounts, &item.DocumentCounts)
		_ = json.Unmarshal(capabilities, &item.Capabilities)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) modExportRegistry(w http.ResponseWriter, r *http.Request) {
	if !s.canReadModExportRevision(w, r, r.PathValue("revisionId")) {
		return
	}
	registry := strings.ToLower(strings.TrimSpace(r.PathValue("registry")))
	rows, err := s.db.Query(r.Context(), `select resource.entity_id,entity.public_id,resource.canonical_id,
		resource.namespace,resource.resource_path,snapshot.translation_key,snapshot.names,snapshot.data,
		snapshot.icon_path,snapshot.preview_path
		from game_resource_snapshots snapshot
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
		var entityID, publicID, objectID, namespace, objectPath, translationKey, iconPath, previewPath string
		var names, data []byte
		if err = rows.Scan(&entityID, &publicID, &objectID, &namespace, &objectPath, &translationKey, &names, &data, &iconPath, &previewPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode registry")
			return
		}
		item := map[string]any{"entityId": entityID, "publicId": publicID, "id": objectID, "registry": registry,
			"namespace": namespace, "path": objectPath, "translationKey": translationKey,
			"iconPath": iconPath, "previewPath": previewPath}
		item["names"] = jsonValue(names)
		item["data"] = jsonValue(data)
		items = append(items, item)
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
	if r.URL.Query().Get("all") == "1" {
		limit = 10000
	}
	offset := boundedOffset(r.URL.Query().Get("offset"))
	summaryOnly := r.URL.Query().Get("summary") == "1"
	var total int
	err := s.db.QueryRow(r.Context(), `
		select count(*)::int
		from game_resource_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		where snapshot.revision_id=$1 and snapshot.registry=any($2::text[])
		  and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')
		  and (not $4 or snapshot.registry<>'items' or not exists(
			select 1 from game_resource_asset_bindings binding
			join game_resource_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
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
		select resource.entity_id,entity.public_id,resource.canonical_id,snapshot.registry,
			resource.namespace,resource.resource_path,snapshot.translation_key,
			jsonb_strip_nulls(jsonb_build_object('zh_cn',snapshot.names->>'zh_cn','en_us',snapshot.names->>'en_us',$6::text,snapshot.names->>($6::text))),
			snapshot.data-'names',snapshot.icon_path,snapshot.preview_path
		from game_resource_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join catalog_entities entity on entity.id=resource.entity_id
		where snapshot.revision_id=$1 and snapshot.registry=any($2::text[])
		  and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')
		  and (not $7 or snapshot.registry<>'items' or not exists(
			select 1 from game_resource_asset_bindings binding
			join game_resource_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
			where binding.item_resource_id=resource.entity_id and block_snapshot.revision_id=snapshot.revision_id
		  ))
		order by resource.canonical_id,snapshot.registry limit $4 offset $5`,
		revisionID, registries, query, limit, offset, locale, canonicalItemsAndBlocks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read registry entries")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var entityID, publicID, objectID, registry, namespace, objectPath, translationKey, iconPath, previewPath string
		var names, data []byte
		if err = rows.Scan(&entityID, &publicID, &objectID, &registry, &namespace, &objectPath, &translationKey, &names, &data, &iconPath, &previewPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode registry entry")
			return
		}
		items = append(items, map[string]any{
			"entityId": entityID, "publicId": publicID, "id": objectID, "registry": registry, "namespace": namespace, "path": objectPath,
			"translationKey": translationKey, "iconPath": iconPath, "previewPath": previewPath, "names": jsonValue(names), "data": jsonValue(data),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) writeModExportRegistryEntrySummaries(w http.ResponseWriter, r *http.Request, revisionID string, registries []string, query, locale string, total, limit, offset int, canonicalItemsAndBlocks bool) {
	cacheKey := fmt.Sprintf("export-registry-summary:v5:%s:%s:%s:%s:%d:%d:%t", revisionID, strings.Join(registries, ","), query, locale, limit, offset, canonicalItemsAndBlocks)
	payload, err := s.cache.GetOrLoad(r.Context(), cacheKey, func(ctx context.Context) ([]byte, error) {
		rows, loadErr := s.db.Query(ctx, `select resource.entity_id,entity.public_id,resource.canonical_id,snapshot.registry,
		resource.namespace,resource.resource_path,snapshot.translation_key,
		jsonb_strip_nulls(jsonb_build_object('zh_cn',snapshot.names->>'zh_cn','en_us',snapshot.names->>'en_us',$6::text,snapshot.names->>($6::text))),
		'{}'::jsonb,snapshot.icon_path,snapshot.preview_path
		from game_resource_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join catalog_entities entity on entity.id=resource.entity_id
		where snapshot.revision_id=$1 and snapshot.registry=any($2::text[])
		  and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')
		  and (not $7 or snapshot.registry<>'items' or not exists(
			select 1 from game_resource_asset_bindings binding
			join game_resource_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
			where binding.item_resource_id=resource.entity_id and block_snapshot.revision_id=snapshot.revision_id
		  ))
		order by resource.canonical_id,snapshot.registry limit $4 offset $5`, revisionID, registries, query, limit, offset, locale, canonicalItemsAndBlocks)
		if loadErr != nil {
			return nil, loadErr
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		for rows.Next() {
			var entityID, publicID, objectID, registry, namespace, objectPath, translationKey, iconPath, previewPath string
			var names, data []byte
			if loadErr = rows.Scan(&entityID, &publicID, &objectID, &registry, &namespace, &objectPath, &translationKey, &names, &data, &iconPath, &previewPath); loadErr != nil {
				return nil, loadErr
			}
			items = append(items, map[string]any{"entityId": entityID, "publicId": publicID, "id": objectID, "registry": registry, "namespace": namespace, "path": objectPath,
				"translationKey": translationKey, "iconPath": iconPath, "previewPath": previewPath, "names": jsonValue(names), "data": jsonValue(data)})
		}
		if loadErr = rows.Err(); loadErr != nil {
			return nil, loadErr
		}
		return json.Marshal(apiResponse{Data: map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}})
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read registry entry summaries")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=120")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
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
	if r.URL.Query().Get("all") == "1" {
		limit = 10000
	}
	offset := boundedOffset(r.URL.Query().Get("offset"))
	summaryOnly := r.URL.Query().Get("summary") == "1"
	cacheKey := fmt.Sprintf("export-document:v7:%s:%s:%s:%s:%d:%d:%t", revisionID, kind, query, locale, limit, offset, summaryOnly)
	payload, err := s.cache.GetOrLoad(r.Context(), cacheKey, func(ctx context.Context) ([]byte, error) {
		var total int
		if loadErr := s.db.QueryRow(ctx, `select count(*)::int
			from game_resource_snapshots snapshot join game_resources resource on resource.entity_id=snapshot.resource_id
			where snapshot.revision_id=$1 and snapshot.registry=$2
			and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')`,
			revisionID, kind, query).Scan(&total); loadErr != nil {
			return nil, loadErr
		}
		rows, loadErr := s.db.Query(ctx, `select resource.entity_id,entity.public_id,resource.canonical_id,
			jsonb_strip_nulls(jsonb_build_object('zh_cn',snapshot.names->>'zh_cn','en_us',snapshot.names->>'en_us',$6::text,snapshot.names->>($6::text))),
			resource.namespace,snapshot.icon_path,snapshot.preview_path,
			case when not $7 then snapshot.data
				when snapshot.registry='advancements' then jsonb_strip_nulls(jsonb_build_object('parent',snapshot.data->'parent','display',snapshot.data->'display'))
				when snapshot.registry='loot_tables' then jsonb_strip_nulls(jsonb_build_object(
					'category',snapshot.data->'category','path',snapshot.data->'path','possible_item_ids',snapshot.data->'possible_item_ids'))
				else '{}'::jsonb end
			from game_resource_snapshots snapshot
			join game_resources resource on resource.entity_id=snapshot.resource_id
			join catalog_entities entity on entity.id=resource.entity_id
			where snapshot.revision_id=$1 and snapshot.registry=$2
			and ($3='' or resource.canonical_id ilike '%' || $3 || '%' or snapshot.names::text ilike '%' || $3 || '%')
			order by resource.canonical_id limit $4 offset $5`, revisionID, kind, query, limit, offset, locale, summaryOnly)
		if loadErr != nil {
			return nil, loadErr
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		for rows.Next() {
			var entityID, publicID, id, namespace, iconPath, previewPath string
			var names, data []byte
			if loadErr = rows.Scan(&entityID, &publicID, &id, &names, &namespace, &iconPath, &previewPath, &data); loadErr != nil {
				return nil, loadErr
			}
			items = append(items, map[string]any{
				"entityId": entityID, "publicId": publicID, "id": id, "registry": kind, "namespace": namespace, "path": id,
				"translationKey": id, "iconPath": iconPath, "previewPath": previewPath,
				"names": jsonValue(names), "data": jsonValue(data),
			})
		}
		if loadErr = rows.Err(); loadErr != nil {
			return nil, loadErr
		}
		if kind == "loot_tables" {
			if loadErr = s.decorateLootTableResources(ctx, revisionID, items, locale); loadErr != nil {
				return nil, loadErr
			}
		}
		return json.Marshal(apiResponse{Data: map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}})
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read document entry index")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=120")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
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
	if r.URL.Query().Get("pathsOnly") == "1" {
		rows, err := s.db.Query(r.Context(),
			`select asset_path from mod_export_text_assets where revision_id=$1
			 union select asset_path from mod_export_binary_assets where revision_id=$1
			 union select asset_path from mod_export_media where revision_id=$1 order by 1`, revisionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read asset paths")
			return
		}
		defer rows.Close()
		items := make([]string, 0, 4096)
		for rows.Next() {
			var assetPath string
			if err = rows.Scan(&assetPath); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to decode asset path")
				return
			}
			items = append(items, assetPath)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	rows, err := s.db.Query(r.Context(),
		`select asset_path,asset_kind,content_type,sha256,byte_length,false from mod_export_text_assets where revision_id=$1
		 union all select asset_path,asset_kind,content_type,sha256,byte_length,false from mod_export_binary_assets where revision_id=$1
		 union all select asset_path,media_kind,content_type,sha256,byte_length,true from mod_export_media where revision_id=$1
		 order by 1`, revisionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read asset index")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var assetPath, kind, contentType, hash string
		var size int64
		var media bool
		if err = rows.Scan(&assetPath, &kind, &contentType, &hash, &size, &media); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode asset index")
			return
		}
		items = append(items, map[string]any{"path": assetPath, "kind": kind, "contentType": contentType, "sha256": hash, "byteLength": size, "media": media})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
	err := s.db.QueryRow(r.Context(), `select content_type,coalesce(text_content,''),json_content::text from mod_export_text_assets where revision_id=$1 and asset_path=$2`, revisionID, assetPath).Scan(&contentType, &text, &jsonContent)
	if err == nil {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
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
	err = s.db.QueryRow(r.Context(), `select content_type,data from mod_export_binary_assets where revision_id=$1 and asset_path=$2`, revisionID, assetPath).Scan(&contentType, &binary)
	if err == nil {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
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
	rows, err := s.db.Query(r.Context(), `select id,structure_id,asset_path,source_format,summary from mod_export_structures where revision_id=$1 order by structure_id`, revisionID)
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
		item := map[string]any{"id": id, "structureId": structureID, "assetPath": assetPath, "sourceFormat": sourceFormat}
		item["summary"] = jsonValue(summary)
		items = append(items, item)
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
		`select b.content_type,b.asset_path,b.data from mod_export_structures s join mod_export_binary_assets b on b.id=s.template_blob_id where s.id=$1 and s.revision_id=$2`,
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
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
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
	if request.Status != "approved" && request.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "review status must be approved or rejected")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin review")
		return
	}
	defer tx.Rollback(r.Context())
	var modID int64
	var minecraftVersion, loader, namespace, status string
	err = tx.QueryRow(r.Context(), `select mod_id,minecraft_version,loader,source_namespace,status from mod_export_revisions where id=$1 for update`, revisionID).Scan(&modID, &minecraftVersion, &loader, &namespace, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "export revision not found")
		return
	}
	if err != nil || (status != "ready" && status != "partial") {
		writeError(w, http.StatusConflict, "export revision is not ready")
		return
	}
	if request.Status == "rejected" {
		if _, err = tx.Exec(r.Context(), `update mod_export_revisions set status='rejected',is_active=false where id=$1`, revisionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject export revision")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit export review")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": revisionID, "status": "rejected", "isActive": false})
		return
	}
	if _, err = tx.Exec(r.Context(), `update mod_export_revisions set is_active=false,status='superseded' where mod_id=$1 and minecraft_version=$2 and loader=$3 and source_namespace=$4 and is_active`, modID, minecraftVersion, loader, namespace); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to supersede export revision")
		return
	}
	if _, err = tx.Exec(r.Context(), `update mod_export_revisions set is_active=true,activated_at=now() where id=$1`, revisionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate export revision")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit export revision")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": revisionID, "status": status, "isActive": true})
}

func (s *Server) canReadModExportRevision(w http.ResponseWriter, r *http.Request, revisionID string) bool {
	var identity modIdentityRecord
	var active bool
	err := s.db.QueryRow(r.Context(), `select m.id,m.project_code,m.slug,m.created_by,e.is_active from mod_export_revisions e join mods m on m.id=e.mod_id where e.id=$1`, revisionID).Scan(&identity.ID, &identity.UniqueID, &identity.SiteID, &identity.OwnerID, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "export revision not found")
		return false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read export revision")
		return false
	}
	claims := currentClaims(r)
	if !active && !canEditMod(claims, identity) && !hasPermission(claims.Permissions, "project.review") {
		writeError(w, http.StatusForbidden, "export revision is awaiting review")
		return false
	}
	return true
}

func (s *Server) redirectModExportMedia(w http.ResponseWriter, r *http.Request, revisionID, assetPath string) {
	var objectKey, contentType string
	var byteLength int64
	err := s.db.QueryRow(r.Context(), `select f.object_key,m.content_type,m.byte_length from mod_export_media m join oss_files f on f.id=m.oss_file_id where m.revision_id=$1 and m.asset_path=$2 and f.status='active'`, revisionID, assetPath).Scan(&objectKey, &contentType, &byteLength)
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
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = io.Copy(w, io.LimitReader(result.Body, byteLength+1))
		return
	}
	if cfg.DownloadURLMode == ossDownloadModeESAPrivateOrigin {
		http.Redirect(w, r, buildPublicOSSURL(cfg, objectKey), http.StatusTemporaryRedirect)
		return
	}
	client, err := s.ossDownloadClient(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	result, err := client.Presign(r.Context(), &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)}, aliyunoss.PresignExpires(time.Duration(cfg.DownloadURLTTLMinutes)*time.Minute))
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to sign media asset")
		return
	}
	http.Redirect(w, r, result.URL, http.StatusTemporaryRedirect)
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
