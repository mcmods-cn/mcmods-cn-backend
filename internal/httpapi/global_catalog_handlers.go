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

type globalTagSnapshot struct {
	EntityID        string   `json:"entityId"`
	Registry        string   `json:"registry"`
	TagID           string   `json:"tagId"`
	Locale          string   `json:"locale"`
	ContentMarkdown string   `json:"contentMarkdown"`
	MemberIDs       []string `json:"memberIds"`
}

type globalRecipeTypeSnapshot struct {
	EntityID        string           `json:"entityId"`
	RecipeTypeID    string           `json:"recipeTypeId"`
	Locale          string           `json:"locale"`
	ContentMarkdown string           `json:"contentMarkdown"`
	Catalysts       []map[string]any `json:"catalysts"`
}

type globalRecipeSnapshot struct {
	EntityID       string         `json:"entityId"`
	RecipeKey      string         `json:"recipeKey"`
	Note           string         `json:"note"`
	LayoutOverride map[string]any `json:"layoutOverride,omitempty"`
}

func (s *Server) globalModTags(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 60)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	key := fmt.Sprintf("global-tags:v3:list:%s:%s:%d:%d", registry, query, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		var total int
		if err := s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`
			select count(distinct tag.entity_id)::int
			from latest_revisions revision
			join tag_import_snapshots snapshot on snapshot.revision_id=revision.id
			join catalog_tags tag on tag.entity_id=snapshot.tag_id
			where ($1='' or tag.registry=$1) and ($2='' or tag.canonical_id ilike '%'||$2||'%')`, registry, query).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := s.db.Query(ctx, `with `+latestGlobalExportScopeCTE+`
			select tag.entity_id,entity.public_id,tag.registry,tag.canonical_id,
				count(distinct member.raw_member_id)::int
			from latest_revisions revision
			join tag_import_snapshots snapshot on snapshot.revision_id=revision.id
			join catalog_tags tag on tag.entity_id=snapshot.tag_id
			join catalog_entities entity on entity.id=tag.entity_id
			left join tag_import_members member on member.tag_snapshot_id=snapshot.id
			where ($1='' or tag.registry=$1) and ($2='' or tag.canonical_id ilike '%'||$2||'%')
			group by tag.entity_id,entity.public_id,tag.registry,tag.canonical_id
			order by tag.registry,tag.canonical_id limit $3 offset $4`, registry, query, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		for rows.Next() {
			var entityID int64
			var publicID, registryName, tagID string
			var memberCount int
			if err = rows.Scan(&entityID, &publicID, &registryName, &tagID, &memberCount); err != nil {
				return nil, err
			}
			previews, previewErr := s.globalTagMemberRows(ctx, entityID, registryName, 10, 0)
			if previewErr != nil {
				return nil, previewErr
			}
			items = append(items, map[string]any{"entityId": publicID, "publicId": publicID, "registry": registryName,
				"tagId": tagID, "memberCount": memberCount, "previews": previews})
		}
		return map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}, rows.Err()
	})
}

func (s *Server) globalModTagDetail(w http.ResponseWriter, r *http.Request) {
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	tagID := strings.TrimSpace(r.URL.Query().Get("tagId"))
	entityPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entityId")))
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 80, 200)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	if (entityPublicID == "" && (registry == "" || tagID == "")) || len(registry) > 160 || len(tagID) > 512 {
		writeError(w, http.StatusBadRequest, "tag identity is required")
		return
	}
	key := fmt.Sprintf("global-tags:v3:detail:%s:%s:%s:%s:%s:%d:%d", entityPublicID, registry, tagID, primary, secondary, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		var entityID int64
		var publicID string
		err := s.db.QueryRow(ctx, `select tag.entity_id,entity.public_id,tag.registry,tag.canonical_id
			from catalog_tags tag join catalog_entities entity on entity.id=tag.entity_id
			where (($1<>'' and entity.public_id=$1) or ($1='' and tag.registry=$2 and tag.canonical_id=$3))`,
			entityPublicID, registry, tagID).Scan(&entityID, &publicID, &registry, &tagID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errCatalogNotFound
		}
		if err != nil {
			return nil, err
		}
		content, contentLocale, publishedRevisionID, err := s.localizedEntityContent(ctx, entityID, primary, secondary)
		if err != nil {
			return nil, err
		}
		members, err := s.globalTagMemberRows(ctx, entityID, registry, limit, offset)
		if err != nil {
			return nil, err
		}
		var total int
		err = s.db.QueryRow(ctx, `select coalesce(cardinality(resource_ids),0) from tag_member_overrides where tag_id=$1`, entityID).Scan(&total)
		if errors.Is(err, pgx.ErrNoRows) {
			err = s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`
				select count(distinct member.raw_member_id)::int
				from latest_revisions revision
				join tag_import_snapshots snapshot on snapshot.revision_id=revision.id
				join tag_import_members member on member.tag_snapshot_id=snapshot.id
				where snapshot.tag_id=$1`, entityID).Scan(&total)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"entityId": publicID, "publicId": publicID, "registry": registry, "tagId": tagID,
			"contentMarkdown": content, "contentLocale": contentLocale, "publishedRevisionId": publishedRevisionID,
			"memberCount": total, "members": members, "limit": limit, "offset": offset}, nil
	})
}

func (s *Server) globalRecipeTypes(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 60)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	key := fmt.Sprintf("recipe-types:v4:list:%s:%s:%s:%d:%d", query, primary, secondary, limit, offset)
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
			where entity.status='active' and ($1='' or recipe_type.canonical_id ilike '%'||$1||'%'
			 or imported.title_names->>$2 ilike '%'||$1||'%' or imported.title_names->>$3 ilike '%'||$1||'%'
			 or exists(select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))`,
			query, primary, secondary).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := s.db.Query(ctx, `with `+latestGlobalExportScopeCTE+`, imported as (
			select distinct on (snapshot.recipe_type_id) snapshot.recipe_type_id,snapshot.title_names,snapshot.catalysts,snapshot.revision_id
			from latest_revisions revision join recipe_type_import_snapshots snapshot on snapshot.revision_id=revision.id
			order by snapshot.recipe_type_id,coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc
		)
		select recipe_type.entity_id,entity.public_id,recipe_type.canonical_id,
			coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
			 where localization.catalog_entity_id=entity.id and localization.name<>''),imported.title_names,'{}'::jsonb),
			case when exists(select 1 from recipe_type_catalysts catalyst where catalyst.recipe_type_id=recipe_type.entity_id)
			 then coalesce((select jsonb_agg(jsonb_build_object('entityId',resource_entity.public_id,'publicId',resource_entity.public_id,
			  'kindCode',resource.kind_code,'canonicalId',resource.canonical_id,'item',resource.canonical_id,'resource_location',resource.canonical_id)
			  order by catalyst.ordinal) from recipe_type_catalysts catalyst join game_resources resource on resource.entity_id=catalyst.resource_id
			  join catalog_entities resource_entity on resource_entity.id=resource.entity_id where catalyst.recipe_type_id=recipe_type.entity_id),'[]'::jsonb)
			 else coalesce(imported.catalysts,'[]'::jsonb) end,
			coalesce((select content_revision.public_id from content_revisions content_revision where content_revision.id=entity.published_revision_id),imported.revision_id::text,''),
			exists(select 1 from recipe_type_catalysts catalyst where catalyst.recipe_type_id=recipe_type.entity_id),
			(select count(*)::int from recipes recipe join catalog_entities recipe_entity on recipe_entity.id=recipe.entity_id
			 where recipe.recipe_type_id=recipe_type.entity_id and recipe_entity.status='active'),
			(select count(*)::int from recipe_layout_templates template join catalog_entities template_entity on template_entity.id=template.entity_id
			 where template.recipe_type_id=recipe_type.entity_id and template_entity.status='active')
		from recipe_types recipe_type join catalog_entities entity on entity.id=recipe_type.entity_id
		left join imported on imported.recipe_type_id=recipe_type.entity_id
		where entity.status='active' and ($1='' or recipe_type.canonical_id ilike '%'||$1||'%'
		 or imported.title_names->>$2 ilike '%'||$1||'%' or imported.title_names->>$3 ilike '%'||$1||'%'
		 or exists(select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))
		order by recipe_type.canonical_id limit $4 offset $5`, query, primary, secondary, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		for rows.Next() {
			var entityID int64
			var publicID, id, revisionID string
			var names, catalysts []byte
			var canonicalCatalysts bool
			var recipeCount, templateCount int
			if err = rows.Scan(&entityID, &publicID, &id, &names, &catalysts, &revisionID, &canonicalCatalysts, &recipeCount, &templateCount); err != nil {
				return nil, err
			}
			var decorated []map[string]any
			if canonicalCatalysts {
				if err = json.Unmarshal(catalysts, &decorated); err != nil {
					return nil, err
				}
			} else {
				decorated, err = s.decorateCatalysts(ctx, catalysts, revisionID)
				if err != nil {
					return nil, err
				}
			}
			items = append(items, map[string]any{"entityId": publicID, "publicId": publicID, "recipeTypeId": id,
				"canonicalId": id, "names": json.RawMessage(names), "recipeCount": recipeCount, "templateCount": templateCount,
				"catalysts": decorated, "revisionId": revisionID})
		}
		return map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}, rows.Err()
	})
}

func (s *Server) globalRecipeTypeDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	entityPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entityId")))
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 80)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	if id == "" && entityPublicID == "" {
		writeError(w, http.StatusBadRequest, "recipe type identity is required")
		return
	}
	key := fmt.Sprintf("recipe-types:v7:detail:%s:%s:%s:%s:%d:%d", entityPublicID, id, primary, secondary, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		var entityID int64
		var publicID, revisionID string
		var names, catalysts []byte
		var width, height, scale int
		err := s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`
			select recipe_type.entity_id,entity.public_id,recipe_type.canonical_id,snapshot.title_names,snapshot.catalysts,
				snapshot.revision_id,snapshot.width,snapshot.height,snapshot.image_scale
			from latest_revisions revision
			join recipe_type_import_snapshots snapshot on snapshot.revision_id=revision.id
			join recipe_types recipe_type on recipe_type.entity_id=snapshot.recipe_type_id
			join catalog_entities entity on entity.id=recipe_type.entity_id
			where (($1<>'' and entity.public_id=$1) or ($1='' and recipe_type.canonical_id=$2))
			order by coalesce(revision.activated_at,revision.created_at) desc limit 1`, entityPublicID, id).Scan(
			&entityID, &publicID, &id, &names, &catalysts, &revisionID, &width, &height, &scale)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errCatalogNotFound
		}
		if err != nil {
			return nil, err
		}
		var hasCatalystOverride bool
		var overrideCatalysts []byte
		if overrideErr := s.db.QueryRow(ctx, `select exists(select 1 from recipe_type_catalyst_overrides where recipe_type_id=$1),
			coalesce(jsonb_agg(jsonb_build_object(
			'entityId',entity.public_id,'publicId',entity.public_id,'item',resource.canonical_id,
			'resource_location',resource.canonical_id,'canonicalId',resource.canonical_id,'kindCode',resource.kind_code)
			order by requested.ordinal) filter(where resource.entity_id is not null),'[]'::jsonb)
			from recipe_type_catalyst_overrides override
			left join lateral unnest(override.catalyst_resource_ids) with ordinality requested(resource_id,ordinal) on true
			left join game_resources resource on resource.entity_id=requested.resource_id
			left join catalog_entities entity on entity.id=resource.entity_id
			where override.recipe_type_id=$1`, entityID).Scan(&hasCatalystOverride, &overrideCatalysts); overrideErr != nil {
			return nil, overrideErr
		} else if hasCatalystOverride {
			catalysts = overrideCatalysts
		}
		decoratedCatalysts, err := s.decorateCatalysts(ctx, catalysts, revisionID)
		if err != nil {
			return nil, err
		}
		content, contentLocale, publishedRevisionID, err := s.localizedEntityContent(ctx, entityID, primary, secondary)
		if err != nil {
			return nil, err
		}
		var total, templateCount int
		if err = s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`
			select
				(select count(distinct snapshot.recipe_id)::int from latest_revisions revision
				 join recipe_import_snapshots snapshot on snapshot.revision_id=revision.id
				 join recipes recipe on recipe.entity_id=snapshot.recipe_id where recipe.recipe_type_id=$1),
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
			where recipe.recipe_type_id=$1
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
			recipeResult := map[string]any{"entityId": recipePublicID, "publicId": recipePublicID,
				"recipeKey": recipePublicID, "recipeId": recipeID, "recipeIdSource": sourceKind,
				"recipeIdCanonical": canonical, "semanticFingerprint": fingerprint, "recipeSnapshotId": snapshotID,
				"revisionId": sourceRevisionID, "modSiteId": siteID, "note": note}
			if layout != nil {
				recipeResult["layout"] = layout
			}
			recipes = append(recipes, recipeResult)
		}
		if err = s.hydrateRecipeRenderLayouts(ctx, recipes); err != nil {
			return nil, err
		}
		if err = s.decorateRecipeResources(ctx, recipes, primary, secondary); err != nil {
			return nil, err
		}
		return map[string]any{"entityId": publicID, "publicId": publicID, "recipeTypeId": id,
			"names": json.RawMessage(names), "contentMarkdown": content, "contentLocale": contentLocale,
			"publishedRevisionId": publishedRevisionID, "catalysts": decoratedCatalysts, "backgroundPath": "",
			"revisionId": revisionID, "width": width, "height": height, "imageScale": scale,
			"backgroundContainsIngredients": false, "templateCount": templateCount,
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
	var recipePublicID, recipeID, sourceKind, fingerprint, snapshotID, sourceRevisionID, siteID, note string
	var canonical bool
	var raw []byte
	err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`, selected as (
		select distinct on (recipe.entity_id) recipe.entity_id,entity.public_id,recipe.canonical_source_id,
			recipe.semantic_fingerprint,recipe.identity_source,snapshot.id snapshot_id,snapshot.source_recipe_id,
			snapshot.source_id_kind,snapshot.revision_id,mod.slug,
			override.note,override.layout_override
		from latest_revisions revision
		join recipe_import_snapshots snapshot on snapshot.revision_id=revision.id
		join recipes recipe on recipe.entity_id=snapshot.recipe_id
		join catalog_entities entity on entity.id=recipe.entity_id and entity.status='active'
		join mods mod on mod.id=revision.mod_id
		left join recipe_content_overrides override on override.recipe_id=recipe.entity_id
		where entity.public_id=$1
		order by recipe.entity_id,coalesce(revision.activated_at,revision.created_at) desc
	)
	select entity_id,public_id,coalesce(canonical_source_id,source_recipe_id),source_id_kind,
		(canonical_source_id is not null),semantic_fingerprint,snapshot_id,revision_id,slug,coalesce(note,''),
		layout_override
	from selected limit 1`, publicID).Scan(&recipeEntityID, &recipePublicID, &recipeID, &sourceKind, &canonical,
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
		"entityId": recipePublicID, "publicId": recipePublicID, "recipeKey": recipePublicID,
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
				key := exportResourceKey{RevisionID: revisionID, ResourceID: recipeAlternativeItemID(alternative), Kind: normalizeExportResourceKind(kind)}
				source, exists := resolved[key]
				if !exists {
					continue
				}
				alternative["entityId"] = source.EntityID
				alternative["publicId"] = source.PublicID
				alternative["kindCode"] = source.KindCode
				alternative["sourceRevisionId"] = source.RevisionID
				alternative["sourceModSiteId"] = source.ModSiteID
				alternative["sourceVersionPublicId"] = source.VersionPublicID
				alternative["detailUrl"] = canonicalResourceDetailURL(source)
				alternative["sourceRegistry"] = source.Registry
				alternative["sourceObjectId"] = source.ObjectID
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
	itemID, _ := alternative["item"].(string)
	if itemID == "" {
		itemID, _ = alternative["resource_location"].(string)
	}
	return itemID
}

var errCatalogNotFound = errors.New("catalog entry not found")

func (s *Server) writeCachedCatalog(w http.ResponseWriter, r *http.Request, key string, loader func(context.Context) (any, error)) {
	var datasetVersion string
	if err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`
		select coalesce(md5(string_agg(id||':'||package_id,',' order by id)),'empty') from latest_revisions`).Scan(&datasetVersion); err != nil {
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

func (s *Server) globalTagMemberRows(ctx context.Context, tagEntityID int64, tagRegistry string, limit, offset int) ([]map[string]any, error) {
	var override []int64
	if err := s.db.QueryRow(ctx, `select resource_ids from tag_member_overrides where tag_id=$1`, tagEntityID).Scan(&override); err == nil {
		return s.globalResourceRows(ctx, override, limit, offset)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `with `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`, members as (
		select distinct member.raw_member_id from latest_revisions revision
		join tag_import_snapshots tag_snapshot on tag_snapshot.revision_id=revision.id
		join tag_import_members member on member.tag_snapshot_id=tag_snapshot.id
		where tag_snapshot.tag_id=$1
		order by member.raw_member_id limit $2 offset $3
	)
	select coalesce(entity.public_id,''),coalesce(entity.public_id,''),members.raw_member_id,
		coalesce(resolved.registry,''),coalesce(resolved.names,'{}'::jsonb),coalesce(resolved.revision_id::text,''),
		coalesce(mod.slug,''),coalesce(resolved.icon_path,'')
	from members
	left join lateral (
		select resource.entity_id,snapshot.registry,snapshot.names,snapshot.revision_id,snapshot.icon_path
		from game_resources resource
		join latest_resource_snapshots snapshot on snapshot.resource_id=resource.entity_id
		where resource.canonical_id=members.raw_member_id
		order by (coalesce(snapshot.icon_path,'')<>'') desc,
			case when resource.kind_code=$4 then 0 when resource.kind_code='minecraft.item' then 1 when resource.kind_code='minecraft.block' then 2 else 3 end,
			resource.entity_id
		limit 1
	) resolved on true
	left join catalog_entities entity on entity.id=resolved.entity_id
	left join catalog_import_revisions revision on revision.id=resolved.revision_id
	left join mods mod on mod.id=revision.mod_id order by members.raw_member_id`, tagEntityID, limit, offset, resourceKindForRegistry(tagRegistry))
	if err != nil {
		return nil, err
	}
	return scanGlobalResources(rows)
}

func (s *Server) globalResourceRows(ctx context.Context, resourceIDs []int64, limit, offset int) ([]map[string]any, error) {
	if offset >= len(resourceIDs) {
		return []map[string]any{}, nil
	}
	selected := resourceIDs[offset:min(len(resourceIDs), offset+limit)]
	rows, err := s.db.Query(ctx, `with `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`
	select entity.public_id,entity.public_id,resource.canonical_id,snapshot.registry,snapshot.names,
		snapshot.revision_id,mod.slug,snapshot.icon_path
	from unnest($1::bigint[]) with ordinality requested(entity_id,ordinal)
	join game_resources resource on resource.entity_id=requested.entity_id
	join catalog_entities entity on entity.id=resource.entity_id
	join latest_resource_snapshots snapshot on snapshot.resource_id=resource.entity_id
	join catalog_import_revisions revision on revision.id=snapshot.revision_id
	join mods mod on mod.id=revision.mod_id order by requested.ordinal`, selected)
	if err != nil {
		return nil, err
	}
	return scanGlobalResources(rows)
}

func scanGlobalResources(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var entityID, publicID, id, registry, revisionID, siteID, iconPath string
		var names []byte
		if err := rows.Scan(&entityID, &publicID, &id, &registry, &names, &revisionID, &siteID, &iconPath); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"entityId": publicID, "publicId": publicID, "id": id,
			"registry": registry, "names": json.RawMessage(names), "revisionId": revisionID, "modSiteId": siteID, "iconPath": iconPath})
	}
	return items, rows.Err()
}

func (s *Server) decorateCatalysts(ctx context.Context, raw []byte, revisionID string) ([]map[string]any, error) {
	var catalysts []map[string]any
	_ = json.Unmarshal(raw, &catalysts)
	entityIDs := make([]string, 0, len(catalysts))
	for _, catalyst := range catalysts {
		if entityID, _ := catalyst["entityId"].(string); strings.TrimSpace(entityID) != "" {
			entityIDs = append(entityIDs, entityID)
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
			entityID, _ := catalyst["entityId"].(string)
			if canonicalID := canonicalByEntity[entityID]; canonicalID != "" {
				catalyst["item"] = canonicalID
				catalyst["resource_location"] = canonicalID
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
		source, exists := resolved[exportResourceKey{RevisionID: revisionID, ResourceID: itemID, Kind: "item"}]
		if !exists {
			continue
		}
		catalyst["entityId"] = source.EntityID
		catalyst["publicId"] = source.PublicID
		catalyst["revisionId"] = source.RevisionID
		catalyst["modSiteId"] = source.ModSiteID
		catalyst["versionPublicId"] = source.VersionPublicID
		catalyst["detailUrl"] = canonicalResourceDetailURL(source)
		catalyst["iconPath"] = source.IconPath
	}
	return catalysts, nil
}

func publishGlobalCatalogSnapshotTx(ctx context.Context, tx pgx.Tx, revisionID int64, aggregateType string, raw []byte, actorID int64) error {
	switch aggregateType {
	case "catalog_tag":
		var snapshot globalTagSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		var entityID int64
		if err := tx.QueryRow(ctx, `select id from catalog_entities where public_id=$1 and entity_type='tag'`, snapshot.EntityID).Scan(&entityID); err != nil {
			return err
		}
		if err := publishKnowledgePageTx(ctx, tx, entityID, snapshot.Locale, snapshot.ContentMarkdown, actorID, revisionID); err != nil {
			return err
		}
		var resourceIDs []int64
		if err := tx.QueryRow(ctx, `select coalesce(array_agg(resource.entity_id order by requested.ordinal) filter(where resource.entity_id is not null),'{}'::bigint[])
			from unnest($1::text[]) with ordinality requested(canonical_id,ordinal)
			left join lateral (select resource.entity_id from game_resource_aliases alias
				join game_resources resource on resource.entity_id=alias.resource_id
				where alias.alias_id=requested.canonical_id order by (resource.kind_code='minecraft.item') desc limit 1) resource on true`, snapshot.MemberIDs).Scan(&resourceIDs); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into tag_member_overrides(tag_id,resource_ids,updated_by,published_revision_id)
			values($1,$2,$3,$4) on conflict(tag_id) do update set resource_ids=excluded.resource_ids,
			updated_by=excluded.updated_by,published_revision_id=excluded.published_revision_id,updated_at=now()`,
			entityID, resourceIDs, nullableActorID(actorID), revisionID)
		return err
	case "catalog_recipe_type":
		var snapshot globalRecipeTypeSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		var entityID int64
		if err := tx.QueryRow(ctx, `select id from catalog_entities where public_id=$1 and entity_type='recipe_type'`, snapshot.EntityID).Scan(&entityID); err != nil {
			return err
		}
		if err := publishKnowledgePageTx(ctx, tx, entityID, snapshot.Locale, snapshot.ContentMarkdown, actorID, revisionID); err != nil {
			return err
		}
		canonicalIDs := make([]string, 0, len(snapshot.Catalysts))
		for _, catalyst := range snapshot.Catalysts {
			if id := recipeAlternativeItemID(catalyst); id != "" {
				canonicalIDs = append(canonicalIDs, id)
			}
		}
		var resourceIDs []int64
		if err := tx.QueryRow(ctx, `select coalesce(array_agg(resource.entity_id order by requested.ordinal) filter(where resource.entity_id is not null),'{}'::bigint[])
			from unnest($1::text[]) with ordinality requested(canonical_id,ordinal)
			left join game_resource_aliases alias on alias.alias_id=requested.canonical_id and alias.kind_code='minecraft.item'
			left join game_resources resource on resource.entity_id=alias.resource_id`, canonicalIDs).Scan(&resourceIDs); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into recipe_type_catalyst_overrides(recipe_type_id,catalyst_resource_ids,updated_by,published_revision_id)
			values($1,$2,$3,$4) on conflict(recipe_type_id) do update set catalyst_resource_ids=excluded.catalyst_resource_ids,
			updated_by=excluded.updated_by,published_revision_id=excluded.published_revision_id,updated_at=now()`,
			entityID, resourceIDs, nullableActorID(actorID), revisionID)
		return err
	case "catalog_recipe":
		var snapshot globalRecipeSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		var override any
		if len(snapshot.LayoutOverride) > 0 {
			encoded, _ := json.Marshal(snapshot.LayoutOverride)
			override = string(encoded)
		}
		var entityID int64
		if err := tx.QueryRow(ctx, `select id from catalog_entities where public_id=$1 and entity_type='recipe'`, snapshot.EntityID).Scan(&entityID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into recipe_content_overrides(recipe_id,note,layout_override,updated_by,published_revision_id)
			values($1,$2,$3::jsonb,$4,$5) on conflict(recipe_id) do update set note=excluded.note,
			layout_override=excluded.layout_override,updated_by=excluded.updated_by,
			published_revision_id=excluded.published_revision_id,updated_at=now()`,
			entityID, snapshot.Note, override, nullableActorID(actorID), revisionID)
		return err
	default:
		return errors.New("unsupported global catalog revision")
	}
}

func publishKnowledgePageTx(ctx context.Context, tx pgx.Tx, entityID int64, locale, markdown string, actorID, revisionID int64) error {
	_, err := tx.Exec(ctx, `insert into knowledge_pages(entity_id,locale,content_markdown,updated_by,published_revision_id)
		values($1,$2,$3,$4,$5) on conflict(entity_id,locale) do update set content_markdown=excluded.content_markdown,
		updated_by=excluded.updated_by,published_revision_id=excluded.published_revision_id,updated_at=now()`,
		entityID, locale, markdown, nullableActorID(actorID), revisionID)
	return err
}
