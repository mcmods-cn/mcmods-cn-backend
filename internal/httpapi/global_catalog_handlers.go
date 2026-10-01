package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

const latestGlobalExportScopeCTE = `
latest_packages as (
	select distinct on (mod_id) mod_id,package_id
	from catalog_import_revisions
	where is_active and status in ('ready','partial')
	  and exists(select 1 from mods where mods.id=catalog_import_revisions.mod_id and review_status='approved')
	order by mod_id,coalesce(activated_at,created_at) desc,created_at desc,package_id desc
), latest_revisions as (
	select revision.* from catalog_import_revisions revision
	join latest_packages package on package.mod_id=revision.mod_id and package.package_id=revision.package_id
	where revision.is_active and revision.status in ('ready','partial')
)`

const latestGlobalResourceSnapshotCTE = `
latest_resource_snapshots as (
	select distinct on (snapshot.resource_id) snapshot.*
	from latest_revisions source
	join resource_import_snapshots snapshot on snapshot.revision_id=source.id
	order by snapshot.resource_id,coalesce(source.activated_at,source.created_at) desc,source.created_at desc,snapshot.id desc
)`

func (s *Server) globalRecipeTypes(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 60)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	key := fmt.Sprintf("recipe-types:v5:list:%s:%s:%s:%d:%d", query, primary, secondary, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		var total int
		if err := s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`, imported as (
			select distinct on (snapshot.recipe_type_id) snapshot.recipe_type_id,snapshot.title_names
			from latest_revisions revision join recipe_type_import_snapshots snapshot on snapshot.revision_id=revision.id
			order by snapshot.recipe_type_id,coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc
		)
			select count(*)::int from recipe_types recipe_type
			join catalog_entities entity on entity.id=recipe_type.entity_id
			left join imported on imported.recipe_type_id=recipe_type.entity_id
			where entity.status='active'
			 and (imported.recipe_type_id is not null or not exists(select 1 from recipe_type_import_snapshots where recipe_type_id=recipe_type.entity_id))
			 and ($1='' or recipe_type.canonical_id ilike '%'||$1||'%'
			 or imported.title_names->>$2 ilike '%'||$1||'%' or imported.title_names->>$3 ilike '%'||$1||'%'
			 or exists(select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))`,
			query, primary, secondary).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := s.db.Query(ctx, `with `+latestGlobalExportScopeCTE+`, imported as (
			select distinct on (snapshot.recipe_type_id) snapshot.recipe_type_id,snapshot.title_names
			from latest_revisions revision join recipe_type_import_snapshots snapshot on snapshot.revision_id=revision.id
			order by snapshot.recipe_type_id,coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc
		)
		select recipe_type.entity_id,entity.public_id,recipe_type.canonical_id,
			coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
			 where localization.catalog_entity_id=entity.id and localization.name<>''),imported.title_names,'{}'::jsonb),
			(select count(*)::int from recipes recipe join catalog_entities recipe_entity on recipe_entity.id=recipe.entity_id
			where recipe.recipe_type_id=recipe_type.entity_id and recipe_entity.status='active'
			 and exists(select 1 from recipe_import_snapshots snapshot join latest_revisions revision on revision.id=snapshot.revision_id where snapshot.recipe_id=recipe.entity_id)),
			(select count(*)::int from recipe_layout_templates template join catalog_entities template_entity on template_entity.id=template.entity_id
			 where template.recipe_type_id=recipe_type.entity_id and template_entity.status='active')
		from recipe_types recipe_type join catalog_entities entity on entity.id=recipe_type.entity_id
		left join imported on imported.recipe_type_id=recipe_type.entity_id
		where entity.status='active'
		 and (imported.recipe_type_id is not null or not exists(select 1 from recipe_type_import_snapshots where recipe_type_id=recipe_type.entity_id))
		 and ($1='' or recipe_type.canonical_id ilike '%'||$1||'%'
		 or imported.title_names->>$2 ilike '%'||$1||'%' or imported.title_names->>$3 ilike '%'||$1||'%'
		 or exists(select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))
		order by recipe_type.canonical_id limit $4 offset $5`, query, primary, secondary, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		entityIDs := make([]int64, 0, limit)
		for rows.Next() {
			var entityID int64
			var publicID, id string
			var names []byte
			var recipeCount, templateCount int
			if err = rows.Scan(&entityID, &publicID, &id, &names, &recipeCount, &templateCount); err != nil {
				return nil, err
			}
			items = append(items, map[string]any{"publicId": publicID, "canonicalId": id,
				"names": json.RawMessage(names), "recipeCount": recipeCount, "templateCount": templateCount,
				"catalysts": []map[string]any{}})
			entityIDs = append(entityIDs, entityID)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		rows.Close()
		for index, item := range items {
			decorated, loadErr := s.catalogRecipeTypeCatalysts(ctx, entityIDs[index], primary, secondary, true)
			if loadErr != nil {
				return nil, loadErr
			}
			item["catalysts"] = decorated
		}
		return map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}, rows.Err()
	})
}

func (s *Server) globalRecipeTypeCatalog(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 80)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "publicId must be a 9-character mcmods.cn public ID")
		return
	}
	key := fmt.Sprintf("recipe-types:v8:detail:%s:%s:%s:%d:%d", publicID, primary, secondary, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		var entityID int64
		var canonicalID string
		var names []byte
		err := s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`, imported as (
			select distinct on (snapshot.recipe_type_id) snapshot.recipe_type_id,snapshot.title_names
			from latest_revisions revision join recipe_type_import_snapshots snapshot on snapshot.revision_id=revision.id
			order by snapshot.recipe_type_id,coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc
		)
			select recipe_type.entity_id,entity.public_id,recipe_type.canonical_id,
				coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
				 where localization.catalog_entity_id=entity.id and localization.name<>''),imported.title_names,'{}'::jsonb)
			from recipe_types recipe_type
			join catalog_entities entity on entity.id=recipe_type.entity_id and entity.status='active'
			left join imported on imported.recipe_type_id=recipe_type.entity_id
			where entity.public_id=$1
			 and (imported.recipe_type_id is not null or not exists(select 1 from recipe_type_import_snapshots where recipe_type_id=recipe_type.entity_id))
			limit 1`, publicID).Scan(&entityID, &publicID, &canonicalID, &names)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errCatalogNotFound
		}
		if err != nil {
			return nil, err
		}
		decoratedCatalysts, err := s.catalogRecipeTypeCatalysts(ctx, entityID, primary, secondary, true)
		if err != nil {
			return nil, err
		}
		content, contentLocale, _, err := s.localizedEntityContent(ctx, entityID, primary, secondary)
		if err != nil {
			return nil, err
		}
		var total, templateCount int
		if err = s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`
			select
				(select count(distinct snapshot.recipe_id)::int from latest_revisions revision
				 join recipe_import_snapshots snapshot on snapshot.revision_id=revision.id
				 join recipes recipe on recipe.entity_id=snapshot.recipe_id
				 join catalog_entities entity on entity.id=recipe.entity_id and entity.status='active' where recipe.recipe_type_id=$1),
				(select count(*)::int from recipe_layout_templates template
				 join catalog_entities entity on entity.id=template.entity_id
				 where template.recipe_type_id=$1 and entity.status='active')`, entityID).Scan(&total, &templateCount); err != nil {
			return nil, err
		}
		rows, err := s.db.Query(ctx, `with `+latestGlobalExportScopeCTE+`, selected as (
			select distinct on (recipe.entity_id) recipe.entity_id,entity.public_id,recipe.canonical_source_id,
				recipe.semantic_fingerprint,recipe.identity_source,snapshot.id snapshot_id,snapshot.source_recipe_id,
				snapshot.source_id_kind,snapshot.revision_id,mod.slug,
				override.note,override.layout_override
			from latest_revisions revision
			join recipe_import_snapshots snapshot on snapshot.revision_id=revision.id
			join recipes recipe on recipe.entity_id=snapshot.recipe_id
			join catalog_entities entity on entity.id=recipe.entity_id
			join mods mod on mod.id=revision.mod_id
			left join recipe_content_overrides override on override.recipe_id=recipe.entity_id
			where recipe.recipe_type_id=$1 and entity.status='active'
			order by recipe.entity_id,coalesce(revision.activated_at,revision.created_at) desc
		)
		select entity_id,public_id,coalesce(canonical_source_id,source_recipe_id),source_id_kind,
			(canonical_source_id is not null),semantic_fingerprint,snapshot_id,revision_id,slug,coalesce(note,''),
			layout_override
		from selected order by entity_id limit $2 offset $3`, entityID, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		recipes := make([]map[string]any, 0, limit)
		for rows.Next() {
			var recipeEntityID, recipePublicID, recipeID, sourceKind, fingerprint, snapshotID, sourceRevisionID, siteID, note string
			var canonical bool
			var raw []byte
			if err = rows.Scan(&recipeEntityID, &recipePublicID, &recipeID, &sourceKind, &canonical, &fingerprint,
				&snapshotID, &sourceRevisionID, &siteID, &note, &raw); err != nil {
				return nil, err
			}
			layout, layoutErr := scanOptionalRecipeOverride(raw)
			if layoutErr != nil {
				return nil, layoutErr
			}
			recipeResult := map[string]any{"publicId": recipePublicID, "recipeTypePublicId": publicID,
				"recipeId": recipeID, "recipeIdSource": sourceKind,
				"recipeIdCanonical": canonical, "semanticFingerprint": fingerprint, "recipeSnapshotId": snapshotID,
				"revisionId": sourceRevisionID, "modSiteId": siteID, "note": note}
			if layout != nil {
				recipeResult["layout"] = layout
			}
			recipes = append(recipes, recipeResult)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		rows.Close()
		if err = s.hydrateRecipeRenderLayouts(ctx, recipes); err != nil {
			return nil, err
		}
		if err = s.decorateRecipeResources(ctx, recipes, primary, secondary); err != nil {
			return nil, err
		}
		return map[string]any{"publicId": publicID, "canonicalId": canonicalID,
			"names": json.RawMessage(names), "contentMarkdown": content, "contentLocale": contentLocale,
			"catalysts": decoratedCatalysts, "templateCount": templateCount,
			"recipes": recipes, "total": total, "limit": limit, "offset": offset}, rows.Err()
	})
}

func (s *Server) globalRecipeRender(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "publicId must be a 9-character mcmods.cn public ID")
		return
	}
	primary, secondary := requestedContentLocales(r)
	var recipeEntityID int64
	var recipePublicID, recipeTypePublicID, recipeID, sourceKind, fingerprint, snapshotID, sourceRevisionID, siteID, note string
	var canonical bool
	var raw []byte
	err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`, selected as (
		select distinct on (recipe.entity_id) recipe.entity_id,entity.public_id,type_entity.public_id recipe_type_public_id,recipe.canonical_source_id,
			recipe.semantic_fingerprint,recipe.identity_source,snapshot.id snapshot_id,snapshot.source_recipe_id,
			snapshot.source_id_kind,snapshot.revision_id,mod.slug,
			override.note,override.layout_override
		from latest_revisions revision
		join recipe_import_snapshots snapshot on snapshot.revision_id=revision.id
		join recipes recipe on recipe.entity_id=snapshot.recipe_id
		join catalog_entities entity on entity.id=recipe.entity_id and entity.status='active'
		join catalog_entities type_entity on type_entity.id=recipe.recipe_type_id and type_entity.status='active'
		join mods mod on mod.id=revision.mod_id
		left join recipe_content_overrides override on override.recipe_id=recipe.entity_id
		where entity.public_id=$1
		order by recipe.entity_id,coalesce(revision.activated_at,revision.created_at) desc
	)
	select entity_id,public_id,recipe_type_public_id,coalesce(canonical_source_id,source_recipe_id),source_id_kind,
		(canonical_source_id is not null),semantic_fingerprint,snapshot_id,revision_id,slug,coalesce(note,''),
		layout_override
	from selected limit 1`, publicID).Scan(&recipeEntityID, &recipePublicID, &recipeTypePublicID, &recipeID, &sourceKind, &canonical,
		&fingerprint, &snapshotID, &sourceRevisionID, &siteID, &note, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "renderable recipe not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipe")
		return
	}
	layout, err := scanOptionalRecipeOverride(raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode recipe layout")
		return
	}
	result := map[string]any{
		"publicId": recipePublicID, "recipeTypePublicId": recipeTypePublicID,
		"recipeId": recipeID, "recipeIdSource": sourceKind, "recipeIdCanonical": canonical,
		"semanticFingerprint": fingerprint, "recipeSnapshotId": snapshotID,
		"revisionId": sourceRevisionID, "modSiteId": siteID, "note": note,
	}
	if layout != nil {
		result["layout"] = layout
	}
	recipes := []map[string]any{result}
	if err = s.hydrateRecipeRenderLayouts(r.Context(), recipes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build recipe layout")
		return
	}
	if err = s.decorateRecipeResources(r.Context(), recipes, primary, secondary); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decorate recipe resources")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) decorateRecipeResources(ctx context.Context, recipes []map[string]any, locales ...string) error {
	keys := make([]exportResourceKey, 0, len(recipes)*8)
	for _, recipe := range recipes {
		revisionID, _ := recipe["revisionId"].(string)
		layout, _ := recipe["layout"].(map[string]any)
		slots, _ := layout["slots"].([]any)
		for _, rawSlot := range slots {
			slot, _ := rawSlot.(map[string]any)
			alternatives, _ := slot["alternatives"].([]any)
			for _, rawAlternative := range alternatives {
				alternative, _ := rawAlternative.(map[string]any)
				kind := firstCatalogString(alternative, slot, "ingredient_kind", "ingredient_type")
				keys = append(keys, exportResourceKey{RevisionID: revisionID, ResourceID: recipeAlternativeItemID(alternative), Kind: kind})
			}
		}
	}
	resolved, err := s.resolveExportResources(ctx, keys)
	if err != nil {
		return err
	}
	for _, recipe := range recipes {
		revisionID, _ := recipe["revisionId"].(string)
		layout, _ := recipe["layout"].(map[string]any)
		slots, _ := layout["slots"].([]any)
		for _, rawSlot := range slots {
			slot, _ := rawSlot.(map[string]any)
			alternatives, _ := slot["alternatives"].([]any)
			for _, rawAlternative := range alternatives {
				alternative, _ := rawAlternative.(map[string]any)
				kind := firstCatalogString(alternative, slot, "ingredient_kind", "ingredient_type")
				itemID := recipeAlternativeItemID(alternative)
				normalizedKind := normalizeExportResourceKind(kind)
				alternative["id"] = itemID
				alternative["kind"] = normalizedKind
				alternative["registry"] = resourceNamespace(itemID)
				for _, alias := range []string{"entityId", "item", "resource_location", "canonicalId", "kindCode", "sourceRegistry", "sourceObjectId"} {
					delete(alternative, alias)
				}
				key := exportResourceKey{RevisionID: revisionID, ResourceID: itemID, Kind: normalizedKind}
				source, exists := resolved[key]
				if !exists {
					continue
				}
				alternative["publicId"] = source.PublicID
				alternative["id"] = source.ObjectID
				alternative["kind"] = source.KindCode
				alternative["registry"] = source.Registry
				alternative["sourceRevisionId"] = source.RevisionID
				alternative["sourceModSiteId"] = source.ModSiteID
				alternative["sourceVersionPublicId"] = source.VersionPublicID
				alternative["detailUrl"] = canonicalResourceDetailURL(source)
				alternative["names"] = localizedExportResourceNames(source.Names, locales...)
				alternative["iconPath"] = source.IconPath
				alternative["previewPath"] = source.PreviewPath
			}
		}
	}
	return nil
}

func firstCatalogString(primary, fallback map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, _ := primary[key].(string); value != "" {
			return value
		}
		if value, _ := fallback[key].(string); value != "" {
			return value
		}
	}
	return ""
}

func recipeAlternativeItemID(alternative map[string]any) string {
	itemID, _ := alternative["id"].(string)
	if itemID == "" {
		itemID, _ = alternative["item"].(string)
	}
	if itemID == "" {
		itemID, _ = alternative["resource_location"].(string)
	}
	return itemID
}

var errCatalogNotFound = errors.New("catalog entry not found")

func (s *Server) writeCachedCatalog(w http.ResponseWriter, r *http.Request, key string, loader func(context.Context) (any, error)) {
	var datasetVersion string
	if err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`
		select md5(concat_ws(':',
			coalesce((select string_agg(id||':'||package_id,',' order by id) from latest_revisions),'empty'),
			coalesce((select max(updated_at)::text from catalog_entities),'empty'),
			coalesce((select max(created_at)::text from content_revisions),'empty')))`).Scan(&datasetVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read catalog version")
		return
	}
	cacheKey := key + ":dataset:" + datasetVersion
	data, err := s.cache.GetOrLoad(r.Context(), cacheKey, func(ctx context.Context) ([]byte, error) {
		value, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		return json.Marshal(apiResponse{Data: value})
	})
	if errors.Is(err, errCatalogNotFound) {
		writeError(w, http.StatusNotFound, "catalog entry not found")
		return
	}
	if err != nil {
		slog.Error("global catalog query failed", "cache_key", key, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to query catalog")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30, stale-while-revalidate=90")
	writeJSONBytes(w, http.StatusOK, data)
}

func requestedContentLocales(r *http.Request) (string, string) {
	primary := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	secondary := normalizeExportContentLocale(r.URL.Query().Get("secondaryLocale"))
	if secondary == primary {
		secondary = "en-US"
	}
	return primary, secondary
}

func (s *Server) localizedEntityContent(ctx context.Context, entityID int64, primary, secondary string) (string, string, *string, error) {
	locales := []string{primary, secondary, "en-US", "zh-CN"}
	var content, locale string
	var revisionID *string
	err := s.db.QueryRow(ctx, `select content_markdown,locale,
		(select revision.public_id from content_revisions revision where revision.id=knowledge_pages.published_revision_id)
		from knowledge_pages
		where entity_id=$1 and locale=any($2::text[]) order by array_position($2::text[],locale) limit 1`, entityID, locales).
		Scan(&content, &locale, &revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, nil
	}
	return content, locale, revisionID, err
}

func (s *Server) decorateCatalysts(ctx context.Context, raw []byte, revisionID string) ([]map[string]any, error) {
	var catalysts []map[string]any
	_ = json.Unmarshal(raw, &catalysts)
	entityIDs := make([]string, 0, len(catalysts))
	for _, catalyst := range catalysts {
		publicID, _ := catalyst["publicId"].(string)
		if publicID == "" {
			publicID, _ = catalyst["entityId"].(string)
		}
		if strings.TrimSpace(publicID) != "" {
			entityIDs = append(entityIDs, publicID)
		}
	}
	if len(entityIDs) > 0 {
		rows, err := s.db.Query(ctx, `select entity.public_id,resource.canonical_id from game_resources resource
			join catalog_entities entity on entity.id=resource.entity_id where entity.public_id=any($1::text[])`, entityIDs)
		if err != nil {
			return nil, err
		}
		canonicalByEntity := make(map[string]string, len(entityIDs))
		for rows.Next() {
			var entityID, canonicalID string
			if err = rows.Scan(&entityID, &canonicalID); err != nil {
				rows.Close()
				return nil, err
			}
			canonicalByEntity[entityID] = canonicalID
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		for _, catalyst := range catalysts {
			publicID, _ := catalyst["publicId"].(string)
			if publicID == "" {
				publicID, _ = catalyst["entityId"].(string)
			}
			if canonicalID := canonicalByEntity[publicID]; canonicalID != "" {
				catalyst["id"] = canonicalID
			}
		}
	}
	keys := make([]exportResourceKey, 0, len(catalysts))
	for _, catalyst := range catalysts {
		keys = append(keys, exportResourceKey{RevisionID: revisionID, ResourceID: recipeAlternativeItemID(catalyst), Kind: "item"})
	}
	resolved, err := s.resolveExportResources(ctx, keys)
	if err != nil {
		return nil, err
	}
	for _, catalyst := range catalysts {
		itemID := recipeAlternativeItemID(catalyst)
		catalyst["id"] = itemID
		if _, exists := catalyst["kind"]; !exists {
			catalyst["kind"] = "minecraft.item"
		}
		if _, exists := catalyst["registry"]; !exists {
			catalyst["registry"] = resourceNamespace(itemID)
		}
		for _, alias := range []string{"entityId", "item", "resource_location", "canonicalId", "kindCode"} {
			delete(catalyst, alias)
		}
		source, exists := resolved[exportResourceKey{RevisionID: revisionID, ResourceID: itemID, Kind: "item"}]
		if !exists {
			continue
		}
		catalyst["publicId"] = source.PublicID
		catalyst["id"] = source.ObjectID
		catalyst["kind"] = source.KindCode
		catalyst["registry"] = source.Registry
		catalyst["names"] = source.Names
		catalyst["revisionId"] = source.RevisionID
		catalyst["modSiteId"] = source.ModSiteID
		catalyst["versionPublicId"] = source.VersionPublicID
		catalyst["detailUrl"] = canonicalResourceDetailURL(source)
		catalyst["iconPath"] = source.IconPath
	}
	return catalysts, nil
}

func resourceNamespace(id string) string {
	if namespace, _, found := strings.Cut(id, ":"); found {
		return namespace
	}
	return ""
}
