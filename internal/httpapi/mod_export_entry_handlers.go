package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

const maxModExportEntryMarkdownBytes = 256 * 1024

type modExportEntryDetailResponse struct {
	EntityID         string                           `json:"entityId"`
	PublicID         string                           `json:"publicId"`
	ContentMarkdown  string                           `json:"contentMarkdown"`
	ContentLocale    string                           `json:"contentLocale"`
	ModelAvailable   bool                             `json:"modelAvailable"`
	ModelAssetPaths  []string                         `json:"modelAssetPaths"`
	BlockEntityModel *modExportBlockEntityModelDetail `json:"blockEntityModel,omitempty"`
	Recipes          []any                            `json:"recipes"`
	Uses             []any                            `json:"uses"`
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
	entityID := strings.TrimSpace(r.URL.Query().Get("entityId"))
	locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	if !isExportRegistryName(registry) || (objectID == "" && entityID == "") {
		writeError(w, http.StatusBadRequest, "registry and entityId or objectId are required")
		return
	}
	var modID int64
	var publicID string
	err := s.db.QueryRow(r.Context(), `select resource.entity_id,entity.public_id,resource.canonical_id,revision.mod_id
		from game_resource_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join catalog_entities entity on entity.id=resource.entity_id
		join mod_export_revisions revision on revision.id=snapshot.revision_id
		where snapshot.revision_id=$1 and snapshot.registry=$2
		and (($3<>'' and resource.entity_id=$3) or ($3='' and resource.canonical_id=$4))`,
		revisionID, registry, entityID, objectID).Scan(&entityID, &publicID, &objectID, &modID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read entry")
		return
	}
	response := modExportEntryDetailResponse{EntityID: entityID, PublicID: publicID, ModelAssetPaths: []string{}, Recipes: []any{}, Uses: []any{}}
	recipeResourceID := entityID
	_ = s.db.QueryRow(r.Context(), `
		select locale,content_markdown from knowledge_pages
		where entity_id=$1 and locale=any($2::text[])
		order by case locale when $3 then 0 when 'zh_cn' then 1 when 'en_us' then 2 else 3 end limit 1`,
		entityID, []string{locale, "zh_cn", "en_us"}, locale).Scan(&response.ContentLocale, &response.ContentMarkdown)
	if registry == "blocks" {
		var itemResourceID, blockstatePath, itemModelPath string
		var modelPaths, texturePaths []string
		bindingErr := s.db.QueryRow(r.Context(), `
			select coalesce(binding.item_resource_id,''),binding.blockstate_path,binding.item_model_path,binding.model_paths,binding.texture_paths
			from game_resource_asset_bindings binding
			join game_resource_snapshots snapshot on snapshot.id=binding.snapshot_id
			where snapshot.revision_id=$1 and binding.block_resource_id=$2`,
			revisionID, entityID).Scan(&itemResourceID, &blockstatePath, &itemModelPath, &modelPaths, &texturePaths)
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
		response.BlockEntityModel, err = s.loadModExportBlockEntityModel(r.Context(), revisionID, entityID)
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
		if itemResourceID != "" {
			recipeResourceID = itemResourceID
		}
	}
	if registry == "items" || registry == "blocks" {
		response.Recipes, response.Uses, err = s.modExportRecipesForObject(r.Context(), revisionID, recipeResourceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read entry recipes")
			return
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) loadModExportBlockEntityModel(ctx context.Context, revisionID, resourceID string) (*modExportBlockEntityModelDetail, error) {
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

func (s *Server) updateModExportEntryContent(w http.ResponseWriter, r *http.Request) {
	identity, err := s.modIdentity(r.Context(), r.PathValue("siteId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod")
		return
	}
	if !canEditMod(currentClaims(r), identity) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	var request struct {
		EntityID        string `json:"entityId"`
		Registry        string `json:"registry"`
		ObjectID        string `json:"objectId"`
		Locale          string `json:"locale"`
		ContentMarkdown string `json:"contentMarkdown"`
	}
	if err = decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.Registry = strings.ToLower(strings.TrimSpace(request.Registry))
	request.EntityID = strings.TrimSpace(request.EntityID)
	request.ObjectID = strings.TrimSpace(request.ObjectID)
	request.Locale = normalizeExportContentLocale(request.Locale)
	if !isExportRegistryName(request.Registry) || (request.EntityID == "" && request.ObjectID == "") || len(request.ContentMarkdown) > maxModExportEntryMarkdownBytes {
		writeError(w, http.StatusBadRequest, "invalid entry content")
		return
	}
	err = s.db.QueryRow(r.Context(), `select resource.entity_id,resource.canonical_id
		from game_resource_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join mod_export_revisions revision on revision.id=snapshot.revision_id
		where revision.mod_id=$1 and snapshot.registry=$2
		and (($3<>'' and resource.entity_id=$3) or ($3='' and resource.canonical_id=$4))
		order by revision.is_active desc,revision.revision_no desc limit 1`,
		identity.ID, request.Registry, request.EntityID, request.ObjectID).Scan(&request.EntityID, &request.ObjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve entry")
		return
	}
	claims := currentClaims(r)
	aggregateKey := entryContentAggregateKey(request.EntityID, request.Locale)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start entry revision")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, "catalog_resource:"+aggregateKey); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock entry content")
		return
	}
	var baseRevisionID *int64
	err = tx.QueryRow(r.Context(), `select published_revision_id from knowledge_pages
		where entity_id=$1 and locale=$2 for update`, request.EntityID, request.Locale).Scan(&baseRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		baseRevisionID = nil
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load entry revision")
		return
	}
	snapshot, err := json.Marshal(map[string]any{
		"entityId": request.EntityID, "modId": identity.ID, "registry": request.Registry, "objectId": request.ObjectID,
		"locale": request.Locale, "contentMarkdown": request.ContentMarkdown,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode entry revision")
		return
	}
	status := "pending"
	if canSkipProjectReview(claims, identity) {
		status = "approved"
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityID: request.EntityID, AggregateType: "catalog_resource", AggregateKey: aggregateKey, BaseRevision: baseRevisionID,
		Snapshot: snapshot, Reason: "Update exported entry introduction", ActorID: claims.Subject,
		Source: "user", Status: status,
		Metadata: map[string]any{"modId": identity.ID, "siteId": identity.SiteID, "registry": request.Registry, "objectId": request.ObjectID, "locale": request.Locale},
		Request:  r,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create entry revision")
		return
	}
	if status == "approved" {
		if err = publishModExportEntryContentTx(r.Context(), tx, created.RevisionID, request.EntityID, request.Locale, request.ContentMarkdown, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish entry content")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record entry approval")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit entry revision")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"locale": request.Locale, "contentMarkdown": request.ContentMarkdown,
		"status": status, "revisionId": created.RevisionID, "changeRequestId": created.ChangeRequestID,
	})
}

func entryContentAggregateKey(entityID, locale string) string {
	encoded, _ := json.Marshal([]any{entityID, locale})
	return string(encoded)
}

func publishModExportEntryContentTx(ctx context.Context, tx pgx.Tx, revisionID int64, entityID, locale, markdown string, actorID int64) error {
	_, err := tx.Exec(ctx, `
		insert into knowledge_pages(entity_id,locale,content_markdown,updated_by,published_revision_id)
		values($1,$2,$3,$4,$5)
		on conflict(entity_id,locale) do update set
		content_markdown=excluded.content_markdown,updated_by=excluded.updated_by,
		published_revision_id=excluded.published_revision_id,updated_at=now()`,
		entityID, locale, markdown, nullableActorID(actorID), revisionID)
	return err
}

func (s *Server) modExportRecipesForObject(ctx context.Context, revisionID, resourceID string) ([]any, []any, error) {
	produces := make([]any, 0, 8)
	uses := make([]any, 0, 8)
	decorations := make([]map[string]any, 0, 16)
	rows, err := s.db.Query(ctx, `with preferred as (
		select minecraft_version,loader from mod_export_revisions where id=$1
	), ranked as (
		select binding.recipe_snapshot_id,bool_or(slot.role='output') produces,
			bool_or(slot.role in ('input','catalyst')) uses,
			row_number() over(partition by snapshot.recipe_id order by (snapshot.revision_id=$1) desc,
			(revision.minecraft_version=preferred.minecraft_version) desc,
			(revision.loader=preferred.loader) desc,coalesce(revision.activated_at,revision.created_at) desc) rank
		from recipe_binding_alternatives alternative
		join recipe_bindings binding on binding.id=alternative.binding_id
		join recipe_template_slots slot on slot.id=binding.template_slot_id
		join recipe_snapshots snapshot on snapshot.id=binding.recipe_snapshot_id
		join mod_export_revisions revision on revision.id=snapshot.revision_id
		cross join preferred
		where alternative.resource_id=$2 and binding.ingredient_present and (snapshot.revision_id=$1 or revision.is_active)
		group by binding.recipe_snapshot_id,snapshot.recipe_id,snapshot.revision_id,
			revision.minecraft_version,revision.loader,preferred.minecraft_version,preferred.loader,
			revision.activated_at,revision.created_at
	)
	select recipe.entity_id,recipe_type.canonical_id,snapshot.id,ranked.produces,ranked.uses,snapshot.revision_id
	from ranked join recipe_snapshots snapshot on snapshot.id=ranked.recipe_snapshot_id
	join recipes recipe on recipe.entity_id=snapshot.recipe_id
	join recipe_types recipe_type on recipe_type.entity_id=recipe.recipe_type_id
	where ranked.rank=1 order by recipe.entity_id limit 200`, revisionID, resourceID)
	if err != nil {
		return produces, uses, err
	}
	defer rows.Close()
	for rows.Next() {
		var recipeID, recipeTypeID, snapshotID, sourceRevisionID string
		var isProduced, isUsed bool
		if err = rows.Scan(&recipeID, &recipeTypeID, &snapshotID, &isProduced, &isUsed, &sourceRevisionID); err != nil {
			return produces, uses, err
		}
		recipe := map[string]any{"entityId": recipeID, "id": recipeID, "type": recipeTypeID,
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
	if err = s.decorateRecipeResources(ctx, decorations); err != nil {
		return produces, uses, err
	}
	return produces, uses, nil
}

func normalizeExportContentLocale(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
	if value == "" || len(value) > 32 {
		return "zh_cn"
	}
	return value
}
