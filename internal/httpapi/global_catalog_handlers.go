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
	select distinct on (revision.mod_id) revision.mod_id,revision.package_id
	from catalog_import_revisions revision join mods public_mod on public_mod.id=revision.mod_id
	where revision.is_active and revision.status in ('ready','partial') and public_mod.review_status='approved'
	order by revision.mod_id,coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc,revision.package_id desc
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

// Public recipes prefer the human-editable canonical definition. Import
// snapshots remain a fallback observation only when a recipe has not yet been
// materialized into that canonical layer.
const publicRecipeSelectionCTE = latestGlobalExportScopeCTE + `,
latest_recipe_observations as (
	select distinct on (recipe.entity_id)
		recipe.entity_id,snapshot.id snapshot_id,snapshot.source_recipe_id,snapshot.source_id_kind,
		snapshot.revision_id,mod.slug mod_site_id
	from latest_revisions revision
	join recipe_import_snapshots snapshot on snapshot.revision_id=revision.id
	join recipes recipe on recipe.entity_id=snapshot.recipe_id
	join mods mod on mod.id=revision.mod_id
	order by recipe.entity_id,coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc,snapshot.id desc
), public_recipes as (
	select recipe.entity_id,entity.public_id,type_entity.public_id recipe_type_public_id,
		coalesce(recipe.canonical_source_id,entity.public_id) recipe_id,
		case when recipe.canonical_source_id is null then recipe.identity_source else 'canonical' end source_id_kind,
		(recipe.canonical_source_id is not null) recipe_id_canonical,recipe.semantic_fingerprint,
		''::text snapshot_id,coalesce(revision.public_id,'') source_revision_id,coalesce(mod.slug,'') mod_site_id,
		coalesce(override.note,'') note,override.layout_override,true authoritative
	from recipes recipe
	join recipe_definitions definition on definition.recipe_id=recipe.entity_id
	join catalog_entities entity on entity.id=recipe.entity_id and entity.status='active'
	join catalog_entities type_entity on type_entity.id=recipe.recipe_type_id and type_entity.status='active'
	left join content_revisions revision on revision.id=definition.published_revision_id
	left join mods mod on mod.id=recipe.owner_mod_id and mod.review_status='approved'
	left join recipe_content_overrides override on override.recipe_id=recipe.entity_id
	union all
	select recipe.entity_id,entity.public_id,type_entity.public_id,
		coalesce(recipe.canonical_source_id,observation.source_recipe_id),observation.source_id_kind,
		(recipe.canonical_source_id is not null),recipe.semantic_fingerprint,
		observation.snapshot_id,observation.revision_id,observation.mod_site_id,
		coalesce(override.note,''),override.layout_override,false
	from latest_recipe_observations observation
	join recipes recipe on recipe.entity_id=observation.entity_id
	join catalog_entities entity on entity.id=recipe.entity_id and entity.status='active'
	join catalog_entities type_entity on type_entity.id=recipe.recipe_type_id and type_entity.status='active'
	left join recipe_content_overrides override on override.recipe_id=recipe.entity_id
	where not exists(select 1 from recipe_definitions definition where definition.recipe_id=recipe.entity_id)
)`

func (s *Server) globalRecipeTypes(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	canonicalID := strings.TrimSpace(r.URL.Query().Get("canonicalId"))
	if len(canonicalID) > 512 {
		writeError(w, http.StatusBadRequest, "invalid exact recipe type filter")
		return
	}
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 60)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	key := fmt.Sprintf("recipe-types:v6:list:%s:%s:%s:%d:%d:exact:%s", query, primary, secondary, limit, offset, canonicalID)
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
			where entity.status='active' and `+publicCatalogEntitySQL("entity", "recipe_type")+` and ($4='' or recipe_type.canonical_id=$4) and ($1='' or recipe_type.canonical_id ilike '%'||$1||'%'
			 or imported.title_names->>$2 ilike '%'||$1||'%' or imported.title_names->>$3 ilike '%'||$1||'%'
			 or exists(select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))`,
			query, primary, secondary, canonicalID).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := s.db.Query(ctx, `with `+publicRecipeSelectionCTE+`, imported as (
			select distinct on (snapshot.recipe_type_id) snapshot.recipe_type_id,snapshot.title_names
			from latest_revisions revision join recipe_type_import_snapshots snapshot on snapshot.revision_id=revision.id
			order by snapshot.recipe_type_id,coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc
		)
		select recipe_type.entity_id,entity.public_id,recipe_type.canonical_id,
			coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
			 where localization.catalog_entity_id=entity.id and localization.name<>''),imported.title_names,'{}'::jsonb),
			(select count(*)::int from public_recipes public_recipe join recipes recipe on recipe.entity_id=public_recipe.entity_id
			 where recipe.recipe_type_id=recipe_type.entity_id),
			(select count(*)::int from recipe_layout_templates template join catalog_entities template_entity on template_entity.id=template.entity_id
			 where template.recipe_type_id=recipe_type.entity_id and template_entity.status='active' and `+publicCatalogEntitySQL("template_entity", "recipe_template")+`)
		from recipe_types recipe_type join catalog_entities entity on entity.id=recipe_type.entity_id
		left join imported on imported.recipe_type_id=recipe_type.entity_id
		where entity.status='active' and `+publicCatalogEntitySQL("entity", "recipe_type")+` and ($6='' or recipe_type.canonical_id=$6) and ($1='' or recipe_type.canonical_id ilike '%'||$1||'%'
		 or imported.title_names->>$2 ilike '%'||$1||'%' or imported.title_names->>$3 ilike '%'||$1||'%'
		 or exists(select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))
		order by recipe_type.canonical_id limit $4 offset $5`, query, primary, secondary, limit, offset, canonicalID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		typeIDs := make([]int64, 0, limit)
		for rows.Next() {
			var entityID int64
			var publicID, id string
			var names []byte
			var recipeCount, templateCount int
			if err = rows.Scan(&entityID, &publicID, &id, &names, &recipeCount, &templateCount); err != nil {
				return nil, err
			}
			typeIDs = append(typeIDs, entityID)
			items = append(items, map[string]any{"publicId": publicID, "canonicalId": id,
				"names": json.RawMessage(names), "recipeCount": recipeCount, "templateCount": templateCount,
				"catalysts": []map[string]any{}})
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		rows.Close()
		catalysts, err := s.catalogRecipeTypesCatalysts(ctx, typeIDs, primary, secondary, true)
		if err != nil {
			return nil, err
		}
		for index, typeID := range typeIDs {
			items[index]["catalysts"] = catalysts[typeID]
		}
		return map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}, nil
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
			where entity.public_id=$1 and `+publicCatalogEntitySQL("entity", "recipe_type")+`
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
		if err = s.db.QueryRow(ctx, `with `+publicRecipeSelectionCTE+`
			select
				(select count(*)::int from public_recipes public_recipe
				 join recipes recipe on recipe.entity_id=public_recipe.entity_id where recipe.recipe_type_id=$1),
				(select count(*)::int from recipe_layout_templates template
				 join catalog_entities entity on entity.id=template.entity_id
				 where template.recipe_type_id=$1 and entity.status='active' and `+publicCatalogEntitySQL("entity", "recipe_template")+`)`, entityID).Scan(&total, &templateCount); err != nil {
			return nil, err
		}
		rows, err := s.db.Query(ctx, `with `+publicRecipeSelectionCTE+`
		select public_recipe.entity_id,public_recipe.public_id,public_recipe.recipe_id,public_recipe.source_id_kind,
			public_recipe.recipe_id_canonical,public_recipe.semantic_fingerprint,public_recipe.snapshot_id,
			public_recipe.source_revision_id,public_recipe.mod_site_id,public_recipe.note,
			public_recipe.layout_override,public_recipe.authoritative
		from public_recipes public_recipe join recipes recipe on recipe.entity_id=public_recipe.entity_id
		where recipe.recipe_type_id=$1 order by public_recipe.entity_id limit $2 offset $3`, entityID, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		recipes := make([]map[string]any, 0, limit)
		for rows.Next() {
			var recipeEntityID, recipePublicID, recipeID, sourceKind, fingerprint, snapshotID, sourceRevisionID, siteID, note string
			var canonical, authoritative bool
			var raw []byte
			if err = rows.Scan(&recipeEntityID, &recipePublicID, &recipeID, &sourceKind, &canonical, &fingerprint,
				&snapshotID, &sourceRevisionID, &siteID, &note, &raw, &authoritative); err != nil {
				return nil, err
			}
			layout, layoutErr := scanOptionalRecipeOverride(raw)
			if layoutErr != nil {
				return nil, layoutErr
			}
			recipeResult := map[string]any{"publicId": recipePublicID, "recipeTypePublicId": publicID,
				"recipeId": recipeID, "recipeIdSource": sourceKind,
				"recipeIdCanonical": canonical, "semanticFingerprint": fingerprint, "recipeSnapshotId": snapshotID,
				"revisionId": sourceRevisionID, "modSiteId": siteID, "note": note,
				"_recipeEntityId": recipeEntityID, "_authoritative": authoritative}
			if layout != nil {
				recipeResult["layout"] = layout
			}
			recipes = append(recipes, recipeResult)
		}
		if err = s.hydratePublicRecipeRenderLayouts(ctx, recipes); err != nil {
			return nil, err
		}
		if err = s.decorateRecipeResources(ctx, recipes, primary, secondary); err != nil {
			return nil, err
		}
		stripPublicRecipeInternalFields(recipes)
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
	var canonical, authoritative bool
	var raw []byte
	err := s.db.QueryRow(r.Context(), `with `+publicRecipeSelectionCTE+`
	select entity_id,public_id,recipe_type_public_id,recipe_id,source_id_kind,recipe_id_canonical,
		semantic_fingerprint,snapshot_id,source_revision_id,mod_site_id,note,layout_override,authoritative
	from public_recipes where public_id=$1 limit 1`, publicID).Scan(&recipeEntityID, &recipePublicID, &recipeTypePublicID, &recipeID, &sourceKind, &canonical,
		&fingerprint, &snapshotID, &sourceRevisionID, &siteID, &note, &raw, &authoritative)
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
		"_recipeEntityId": recipeEntityID, "_authoritative": authoritative,
	}
	if layout != nil {
		result["layout"] = layout
	}
	recipes := []map[string]any{result}
	if err = s.hydratePublicRecipeRenderLayouts(r.Context(), recipes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build recipe layout")
		return
	}
	if err = s.decorateRecipeResources(r.Context(), recipes, primary, secondary); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decorate recipe resources")
		return
	}
	stripPublicRecipeInternalFields(recipes)
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
	resolved, err := s.resolveExportResources(publicCatalogContext(ctx), keys)
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
				for _, field := range []string{"publicId", "detailUrl", "names", "iconPath", "iconUrl", "iconURL", "previewPath", "previewUrl", "sourceRevisionId", "sourceModSiteId", "sourceVersionPublicId"} {
					delete(alternative, field)
				}
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
	if err := s.db.QueryRow(r.Context(), `select version::text from catalog_dataset_state where singleton`).Scan(&datasetVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read catalog version")
		return
	}
	cacheKey := "public-source:v1:" + key + ":dataset:" + datasetVersion
	data, err := s.cache.GetOrLoad(publicCatalogContext(r.Context()), cacheKey, func(ctx context.Context) ([]byte, error) {
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
	decorated, err := s.decorateCatalystBatches(ctx, []catalogCatalystBatch{{TypeID: 0, Raw: raw, RevisionID: revisionID}})
	return decorated[0], err
}

type catalogCatalystBatch struct {
	TypeID     int64
	Raw        []byte
	RevisionID string
}

func decodeCatalogCatalysts(raw []byte, revisionID string) ([]map[string]any, error) {
	var catalysts []map[string]any
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("decode recipe catalysts for revision %s: expected array", revisionID)
	}
	if err := json.Unmarshal(raw, &catalysts); err != nil {
		return nil, fmt.Errorf("decode recipe catalysts for revision %s: %w", revisionID, err)
	}
	if catalysts == nil {
		return nil, fmt.Errorf("decode recipe catalysts for revision %s: expected array", revisionID)
	}
	return catalysts, nil
}

func (s *Server) decorateCatalystBatches(ctx context.Context, batches []catalogCatalystBatch) (map[int64][]map[string]any, error) {
	result := make(map[int64][]map[string]any, len(batches))
	entityIDs := make([]string, 0)
	keys := make([]exportResourceKey, 0)
	for _, batch := range batches {
		catalysts, err := decodeCatalogCatalysts(batch.Raw, batch.RevisionID)
		if err != nil {
			return nil, err
		}
		result[batch.TypeID] = catalysts
		for _, catalyst := range catalysts {
			publicID, _ := catalyst["publicId"].(string)
			if publicID == "" {
				publicID, _ = catalyst["entityId"].(string)
			}
			if strings.TrimSpace(publicID) != "" {
				entityIDs = append(entityIDs, publicID)
			}
			keys = append(keys, exportResourceKey{RevisionID: batch.RevisionID, ResourceID: recipeAlternativeItemID(catalyst), Kind: "item"})
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
		for _, catalysts := range result {
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
	}
	resolved, err := s.resolveExportResources(publicCatalogContext(ctx), keys)
	if err != nil {
		return nil, err
	}
	for _, batch := range batches {
		for _, catalyst := range result[batch.TypeID] {
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
			for _, field := range []string{"publicId", "detailUrl", "names", "iconPath", "iconUrl", "iconURL", "previewPath", "previewUrl", "revisionId", "modSiteId", "versionPublicId"} {
				delete(catalyst, field)
			}
			source, exists := resolved[exportResourceKey{RevisionID: batch.RevisionID, ResourceID: itemID, Kind: "item"}]
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
	}
	return result, nil
}

func resourceNamespace(id string) string {
	if namespace, _, found := strings.Cut(id, ":"); found {
		return namespace
	}
	return ""
}
