package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

const maxModExportEntryMarkdownBytes = 256 * 1024

type modExportEntryDetailResponse struct {
	EntityID          string                           `json:"entityId"`
	PublicID          string                           `json:"publicId"`
	Data              map[string]any                   `json:"data"`
	Name              string                           `json:"name"`
	Summary           string                           `json:"summary"`
	ContentMarkdown   string                           `json:"contentMarkdown"`
	ContentLocale     string                           `json:"contentLocale"`
	ContentProvenance string                           `json:"contentProvenance"`
	ModelAvailable    bool                             `json:"modelAvailable"`
	ModelAssetPaths   []string                         `json:"modelAssetPaths"`
	BlockEntityModel  *modExportBlockEntityModelDetail `json:"blockEntityModel,omitempty"`
	Recipes           []any                            `json:"recipes"`
	Uses              []any                            `json:"uses"`
	Versions          []map[string]any                 `json:"versions"`
}

type modExportBlockEntityModelDetail struct {
	BlockID         string                              `json:"blockId"`
	BlockEntityType string                              `json:"blockEntityTypeId"`
	ModelSource     string                              `json:"modelSource"`
	ModelAvailable  bool                                `json:"modelAvailable"`
	Variants        []modExportBlockEntityVariantDetail `json:"variants"`
}

type modExportBlockEntityVariantDetail struct {
	VariantID       string           `json:"variantId"`
	OBJPath         string           `json:"objPath"`
	MeshPath        string           `json:"meshPath"`
	VertexCount     int              `json:"vertexCount"`
	QuadCount       int              `json:"quadCount"`
	CoordinateSpace string           `json:"coordinateSpace"`
	UVSpace         string           `json:"uvSpace"`
	UVOrigin        string           `json:"uvOrigin"`
	UVComplete      bool             `json:"uvComplete"`
	Textures        []map[string]any `json:"textures"`
	Mesh            map[string]any   `json:"mesh"`
}

func (s *Server) modExportEntryDetail(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	registry := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("registry")))
	objectID := strings.TrimSpace(r.URL.Query().Get("objectId"))
	entityPublicID := strings.TrimSpace(r.URL.Query().Get("entityId"))
	locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	if !isExportRegistryName(registry) || (objectID == "" && entityPublicID == "") {
		writeError(w, http.StatusBadRequest, "registry and entityId or objectId are required")
		return
	}
	var resourceID, modID int64
	var publicID string
	var snapshotData []byte
	err := s.db.QueryRow(r.Context(), `select resource.entity_id,entity.public_id,resource.canonical_id,revision.mod_id,snapshot.data
		from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join catalog_entities entity on entity.id=resource.entity_id
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		where snapshot.revision_id=$1 and snapshot.registry=$2
		and (($3<>'' and entity.public_id=$3) or ($3='' and resource.canonical_id=$4))`,
		revisionID, registry, entityPublicID, objectID).Scan(&resourceID, &publicID, &objectID, &modID, &snapshotData)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read entry")
		return
	}
	data, err := decodeModExportEntryData(snapshotData)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode entry data")
		return
	}
	response := modExportEntryDetailResponse{
		EntityID: publicID, PublicID: publicID, Data: data,
		ModelAssetPaths: []string{}, Recipes: []any{}, Uses: []any{}, Versions: []map[string]any{},
	}
	recipeResourceID := resourceID
	primary, secondary := s.requestContentLocales(r)
	if requested := normalizeContentLocale(locale); requested != "" {
		primary = requested
	}
	if requested := normalizeContentLocale(r.URL.Query().Get("secondaryLocale")); requested != "" {
		secondary = requested
	}
	if registry == "loot_tables" {
		items := []map[string]any{{"data": response.Data}}
		if err = s.decorateLootTableResources(r.Context(), revisionID, items, primary, secondary); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve loot table resources")
			return
		}
	}
	localized, localizationErr := s.loadCatalogEntityLocalizations(r.Context(), publicID)
	if localizationErr != nil && !errors.Is(localizationErr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to resolve entry localization")
		return
	}
	if localizationErr == nil {
		available := make([]string, 0, len(localized.Localizations))
		for availableLocale := range localized.Localizations {
			available = append(available, availableLocale)
		}
		resolution := resolveContentLocale(primary, secondary, localized.DefaultLocale, available)
		if content, ok := localized.Localizations[resolution.ResolvedLocale]; ok {
			response.Name = content.Name
			response.Summary = content.Summary
			response.ContentMarkdown = content.ContentMarkdown
			response.ContentLocale = content.Locale
			response.ContentProvenance = content.Provenance
		}
	}
	if response.Name == "" {
		var translationKey string
		var importedNames []byte
		nameErr := s.db.QueryRow(r.Context(), `select translation_key,names
			from resource_import_snapshots where revision_id=$1 and resource_id=$2`,
			revisionID, resourceID).Scan(&translationKey, &importedNames)
		if nameErr != nil && !errors.Is(nameErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to resolve imported entry name")
			return
		}
		if nameErr == nil {
			names := map[string]any{}
			_ = json.Unmarshal(importedNames, &names)
			item := map[string]any{"translationKey": translationKey, "names": names}
			if nameErr = s.decorateExportTranslationNames(r.Context(), revisionID, primary, []map[string]any{item}); nameErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to resolve imported entry translation")
				return
			}
			names, _ = item["names"].(map[string]any)
			available := make([]string, 0, len(names))
			for availableLocale := range names {
				available = append(available, availableLocale)
			}
			resolution := resolveContentLocale(primary, secondary, "en-US", available)
			response.Name, _ = names[resolution.ResolvedLocale].(string)
			if response.Name != "" {
				response.ContentLocale = resolution.ResolvedLocale
				response.ContentProvenance = "import"
			}
		}
	}
	if response.ContentMarkdown == "" {
		var pageLocale, pageMarkdown string
		pageErr := s.db.QueryRow(r.Context(), `
			select locale,content_markdown from knowledge_pages
			where entity_id=$1 and locale=any($2::text[])
			order by case locale when $3 then 0 when 'zh-CN' then 1 when 'zh-TW' then 2 when 'en-US' then 3 else 4 end limit 1`,
			resourceID, []string{locale, "zh-CN", "zh-TW", "en-US"}, locale).Scan(&pageLocale, &pageMarkdown)
		if pageErr == nil && pageMarkdown != "" {
			response.ContentMarkdown = pageMarkdown
			if response.ContentLocale == "" {
				response.ContentLocale = pageLocale
			}
		}
	}
	versionCarrier := map[string]any{"entityId": publicID, "versions": []map[string]any{}}
	if err = s.decorateResourceVersionRows(r.Context(), []map[string]any{versionCarrier}, primary, secondary); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve entry versions")
		return
	}
	response.Versions, _ = versionCarrier["versions"].([]map[string]any)
	if registry == "blocks" {
		var itemResourceID sql.NullInt64
		var blockstatePath, itemModelPath string
		var modelPaths, texturePaths []string
		bindingErr := s.db.QueryRow(r.Context(), `
			select binding.item_resource_id,binding.blockstate_path,binding.item_model_path,binding.model_paths,binding.texture_paths
			from game_resource_asset_bindings binding
			join resource_import_snapshots snapshot on snapshot.id=binding.snapshot_id
			where snapshot.revision_id=$1 and binding.block_resource_id=$2`,
			revisionID, resourceID).Scan(&itemResourceID, &blockstatePath, &itemModelPath, &modelPaths, &texturePaths)
		if bindingErr != nil && !errors.Is(bindingErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to read block resource binding")
			return
		}
		paths := make(map[string]struct{}, 2+len(modelPaths)+len(texturePaths))
		for _, assetPath := range append([]string{blockstatePath, itemModelPath}, append(modelPaths, texturePaths...)...) {
			if assetPath != "" {
				paths[assetPath] = struct{}{}
			}
		}
		response.BlockEntityModel, err = s.loadModExportBlockEntityModel(r.Context(), revisionID, resourceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read block entity model")
			return
		}
		if response.BlockEntityModel != nil {
			response.ModelAvailable = response.BlockEntityModel.ModelAvailable
			for _, variant := range response.BlockEntityModel.Variants {
				paths[variant.OBJPath] = struct{}{}
				paths[variant.MeshPath] = struct{}{}
				for _, texture := range variant.Textures {
					if texturePath, ok := texture["path"].(string); ok && texturePath != "" {
						paths[texturePath] = struct{}{}
					}
				}
			}
		}
		response.ModelAssetPaths = sortedExportPaths(paths)
		response.ModelAvailable = response.ModelAvailable || blockstatePath != ""
		if itemResourceID.Valid {
			recipeResourceID = itemResourceID.Int64
		}
	}
	if registry == "items" || registry == "blocks" {
		response.Recipes, response.Uses, err = s.modExportRecipesForObject(r.Context(), revisionID, recipeResourceID, locale)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read entry recipes")
			return
		}
	}
	if response.ContentLocale != "" {
		w.Header().Set("Content-Language", response.ContentLocale)
	}
	writeJSON(w, http.StatusOK, response)
}

func decodeModExportEntryData(raw []byte) (map[string]any, error) {
	data := make(map[string]any)
	if len(raw) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data == nil {
		data = make(map[string]any)
	}
	return data, nil
}

func (s *Server) loadModExportBlockEntityModel(ctx context.Context, revisionID string, resourceID int64) (*modExportBlockEntityModelDetail, error) {
	var modelID string
	model := &modExportBlockEntityModelDetail{Variants: []modExportBlockEntityVariantDetail{}}
	err := s.db.QueryRow(ctx, `select id,block_id,block_entity_type_id,model_source,model_available
		from block_entity_model_snapshots where revision_id=$1 and block_resource_id=$2`, revisionID, resourceID).
		Scan(&modelID, &model.BlockID, &model.BlockEntityType, &model.ModelSource, &model.ModelAvailable)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `select variant_id,obj_path,mesh_path,vertex_count,quad_count,coordinate_space,
		uv_space,uv_origin,uv_complete,textures,mesh_data from block_entity_model_variants
		where model_snapshot_id=$1 order by case variant_id when 'single' then 0 when 'default' then 1 else 2 end,variant_id`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var variant modExportBlockEntityVariantDetail
		var texturesRaw, meshRaw []byte
		if err = rows.Scan(&variant.VariantID, &variant.OBJPath, &variant.MeshPath, &variant.VertexCount,
			&variant.QuadCount, &variant.CoordinateSpace, &variant.UVSpace, &variant.UVOrigin,
			&variant.UVComplete, &texturesRaw, &meshRaw); err != nil {
			return nil, err
		}
		variant.Textures = []map[string]any{}
		variant.Mesh = map[string]any{}
		if len(texturesRaw) > 0 {
			_ = json.Unmarshal(texturesRaw, &variant.Textures)
		}
		if len(meshRaw) > 0 {
			_ = json.Unmarshal(meshRaw, &variant.Mesh)
		}
		model.Variants = append(model.Variants, variant)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return model, nil
}

func publishModExportEntryContentTx(ctx context.Context, tx pgx.Tx, revisionID, entityID int64, locale, markdown string, actorID int64) error {
	_, err := tx.Exec(ctx, `
		insert into knowledge_pages(entity_id,locale,content_markdown,updated_by,published_revision_id)
		values($1,$2,$3,$4,$5)
		on conflict(entity_id,locale) do update set
		content_markdown=excluded.content_markdown,updated_by=excluded.updated_by,
		published_revision_id=excluded.published_revision_id,updated_at=now()`,
		entityID, locale, markdown, nullableActorID(actorID), revisionID)
	return err
}

func (s *Server) modExportRecipesForObject(ctx context.Context, revisionID string, resourceID int64, locale string) ([]any, []any, error) {
	produces := make([]any, 0, 8)
	uses := make([]any, 0, 8)
	decorations := make([]map[string]any, 0, 16)
	rows, err := s.db.Query(ctx, `with preferred as (
		select minecraft_version,loader from catalog_import_revisions where id=$1
	), ranked as (
		select binding.recipe_snapshot_id,bool_or(slot.role='output') produces,
			bool_or(slot.role in ('input','catalyst')) uses,
			row_number() over(partition by snapshot.recipe_id order by (snapshot.revision_id=$1) desc,
			(revision.minecraft_version=preferred.minecraft_version) desc,
			(revision.loader=preferred.loader) desc,coalesce(revision.activated_at,revision.created_at) desc) rank
		from recipe_import_binding_candidates alternative
		join recipe_import_bindings binding on binding.id=alternative.binding_id
		join recipe_template_import_slots slot on slot.id=binding.template_slot_id
		join recipe_import_snapshots snapshot on snapshot.id=binding.recipe_snapshot_id
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		cross join preferred
		where alternative.resource_id=$2 and binding.ingredient_present and (snapshot.revision_id=$1 or revision.is_active)
		group by binding.recipe_snapshot_id,snapshot.recipe_id,snapshot.revision_id,
			revision.minecraft_version,revision.loader,preferred.minecraft_version,preferred.loader,
			revision.activated_at,revision.created_at
	)
	select recipe_entity.public_id,recipe_type.canonical_id,snapshot.id,ranked.produces,ranked.uses,snapshot.revision_id,
		coalesce(recipe.canonical_source_id,snapshot.source_recipe_id)
	from ranked join recipe_import_snapshots snapshot on snapshot.id=ranked.recipe_snapshot_id
	join recipes recipe on recipe.entity_id=snapshot.recipe_id
	join catalog_entities recipe_entity on recipe_entity.id=recipe.entity_id
	join recipe_types recipe_type on recipe_type.entity_id=recipe.recipe_type_id
	where ranked.rank=1 order by recipe.entity_id limit 200`, revisionID, resourceID)
	if err != nil {
		return produces, uses, err
	}
	defer rows.Close()
	for rows.Next() {
		var recipePublicID, recipeTypeID, snapshotID, sourceRevisionID, sourceRecipeID string
		var isProduced, isUsed bool
		if err = rows.Scan(&recipePublicID, &recipeTypeID, &snapshotID, &isProduced, &isUsed, &sourceRevisionID, &sourceRecipeID); err != nil {
			return produces, uses, err
		}
		recipe := map[string]any{"entityId": recipePublicID, "id": recipePublicID, "recipeId": sourceRecipeID, "type": recipeTypeID,
			"recipeSnapshotId": snapshotID, "revisionId": sourceRevisionID}
		decorations = append(decorations, recipe)
		if isProduced && len(produces) < 100 {
			produces = append(produces, recipe)
		}
		if isUsed && len(uses) < 100 {
			uses = append(uses, recipe)
		}
	}
	if err = rows.Err(); err != nil {
		return produces, uses, err
	}
	if err = s.hydrateRecipeRenderLayouts(ctx, decorations); err != nil {
		return produces, uses, err
	}
	for _, recipe := range decorations {
		recipe["jeiLayout"] = recipe["layout"]
	}
	if err = s.decorateRecipeResources(ctx, decorations, locale); err != nil {
		return produces, uses, err
	}
	return produces, uses, nil
}

func normalizeExportContentLocale(value string) string {
	value = normalizeContentLocale(value)
	if value == "" || len(value) > 32 || !validContentLocaleTag(value) {
		return "zh-CN"
	}
	return value
}
