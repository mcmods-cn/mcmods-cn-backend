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
	ContentMarkdown string `json:"contentMarkdown"`
	ContentLocale   string `json:"contentLocale"`
	ModelAvailable  bool   `json:"modelAvailable"`
	Recipes         []any  `json:"recipes"`
	Uses            []any  `json:"uses"`
}

func (s *Server) modExportEntryDetail(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	registry := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("registry")))
	objectID := strings.TrimSpace(r.URL.Query().Get("objectId"))
	locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	if !isExportRegistryName(registry) || objectID == "" {
		writeError(w, http.StatusBadRequest, "registry and objectId are required")
		return
	}
	var modID int64
	var namespace, objectPath string
	var err error
	if assetPath, entriesSQL, ok := exportCustomDocumentSource(registry); ok {
		err = s.db.QueryRow(r.Context(), entriesSQL, revisionID, assetPath, objectID).Scan(&modID)
	} else if assetPath, arrayKey, ok := exportDocumentSource(registry); ok {
		query := `select revision.mod_id from mod_export_revisions revision
			join mod_export_text_assets asset on asset.revision_id=revision.id and asset.asset_path=$2
			cross join lateral jsonb_array_elements(asset.json_content->'` + arrayKey + `') document
			where revision.id=$1 and document->>'id'=$3 limit 1`
		err = s.db.QueryRow(r.Context(), query, revisionID, assetPath, objectID).Scan(&modID)
	} else {
		err = s.db.QueryRow(r.Context(), `
			select revision.mod_id,entry.namespace,entry.object_path
			from mod_export_registry_entries entry
			join mod_export_revisions revision on revision.id=entry.revision_id
			where entry.revision_id=$1 and entry.registry=$2 and entry.object_id=$3`,
			revisionID, registry, objectID).Scan(&modID, &namespace, &objectPath)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read entry")
		return
	}
	response := modExportEntryDetailResponse{Recipes: []any{}, Uses: []any{}}
	_ = s.db.QueryRow(r.Context(), `
		select locale,content_markdown from mod_export_entry_contents
		where mod_id=$1 and registry=$2 and object_id=$3 and locale=any($4::text[])
		order by case locale when $5 then 0 when 'zh_cn' then 1 when 'en_us' then 2 else 3 end limit 1`,
		modID, registry, objectID, []string{locale, "zh_cn", "en_us"}, locale).Scan(&response.ContentLocale, &response.ContentMarkdown)
	if registry == "blocks" {
		modelPath := "assets/" + namespace + "/blockstates/" + objectPath + ".json"
		_ = s.db.QueryRow(r.Context(), `select exists(select 1 from mod_export_text_assets where revision_id=$1 and asset_path=$2)`, revisionID, modelPath).Scan(&response.ModelAvailable)
	}
	if registry == "items" || registry == "blocks" {
		response.Recipes, response.Uses, err = s.modExportRecipesForObject(r.Context(), revisionID, objectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read entry recipes")
			return
		}
	}
	writeJSON(w, http.StatusOK, response)
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
	request.ObjectID = strings.TrimSpace(request.ObjectID)
	request.Locale = normalizeExportContentLocale(request.Locale)
	if !isExportRegistryName(request.Registry) || request.ObjectID == "" || len(request.ContentMarkdown) > maxModExportEntryMarkdownBytes {
		writeError(w, http.StatusBadRequest, "invalid entry content")
		return
	}
	var exists bool
	if assetPath, entriesSQL, ok := exportCustomDocumentSource(request.Registry); ok {
		query := `select exists(` + strings.Replace(entriesSQL, "revision.id=$1", "revision.mod_id=$1", 1) + `)`
		err = s.db.QueryRow(r.Context(), query, identity.ID, assetPath, request.ObjectID).Scan(&exists)
	} else if assetPath, arrayKey, ok := exportDocumentSource(request.Registry); ok {
		query := `select exists(select 1 from mod_export_revisions revision
			join mod_export_text_assets asset on asset.revision_id=revision.id and asset.asset_path=$2
			cross join lateral jsonb_array_elements(asset.json_content->'` + arrayKey + `') document
			where revision.mod_id=$1 and document->>'id'=$3)`
		err = s.db.QueryRow(r.Context(), query, identity.ID, assetPath, request.ObjectID).Scan(&exists)
	} else {
		err = s.db.QueryRow(r.Context(), `
			select exists(select 1 from mod_export_registry_entries entry join mod_export_revisions revision on revision.id=entry.revision_id
			where revision.mod_id=$1 and entry.registry=$2 and entry.object_id=$3)`, identity.ID, request.Registry, request.ObjectID).Scan(&exists)
	}
	if err != nil || !exists {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}
	claims := currentClaims(r)
	aggregateKey := entryContentAggregateKey(identity.ID, request.Registry, request.ObjectID, request.Locale)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start entry revision")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, "mod_export_entry:"+aggregateKey); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock entry content")
		return
	}
	var baseRevisionID *int64
	err = tx.QueryRow(r.Context(), `select published_revision_id from mod_export_entry_contents
		where mod_id=$1 and registry=$2 and object_id=$3 and locale=$4 for update`,
		identity.ID, request.Registry, request.ObjectID, request.Locale).Scan(&baseRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		baseRevisionID = nil
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load entry revision")
		return
	}
	snapshot, err := json.Marshal(map[string]any{
		"modId": identity.ID, "registry": request.Registry, "objectId": request.ObjectID,
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
		AggregateType: "mod_export_entry", AggregateKey: aggregateKey, BaseRevision: baseRevisionID,
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
		if err = publishModExportEntryContentTx(r.Context(), tx, created.RevisionID, identity.ID, request.Registry, request.ObjectID, request.Locale, request.ContentMarkdown, claims.Subject); err != nil {
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

func entryContentAggregateKey(modID int64, registry, objectID, locale string) string {
	encoded, _ := json.Marshal([]any{modID, registry, objectID, locale})
	return string(encoded)
}

func publishModExportEntryContentTx(ctx context.Context, tx pgx.Tx, revisionID, modID int64, registry, objectID, locale, markdown string, actorID int64) error {
	_, err := tx.Exec(ctx, `
		insert into mod_export_entry_contents(mod_id,registry,object_id,locale,content_markdown,updated_by,published_revision_id)
		values($1,$2,$3,$4,$5,$6,$7)
		on conflict(mod_id,registry,object_id,locale) do update set
		content_markdown=excluded.content_markdown,updated_by=excluded.updated_by,
		published_revision_id=excluded.published_revision_id,updated_at=now()`,
		modID, registry, objectID, locale, markdown, nullableActorID(actorID), revisionID)
	return err
}

func (s *Server) modExportRecipesForObject(ctx context.Context, revisionID, objectID string) ([]any, []any, error) {
	produces := make([]any, 0, 8)
	uses := make([]any, 0, 8)
	decorations := make([]map[string]any, 0, 16)
	rows, err := s.db.Query(ctx, `with matches as (
		select recipe_key,bool_or(role='output') produces,bool_or(role in ('input','catalyst')) uses
		from mod_export_recipe_items where revision_id=$1 and item_id=$2 and ingredient_kind='item'
		group by recipe_key limit 200)
		select layout.recipe_id,layout.recipe_type_id,layout.compact_layout,matches.produces,matches.uses
		from matches join lateral (
			select recipe_id,recipe_type_id,compact_layout from mod_export_recipe_layouts
			where revision_id=$1 and recipe_key=matches.recipe_key order by layout_path limit 1
		) layout on true order by layout.recipe_id`, revisionID, objectID)
	if err != nil {
		return produces, uses, err
	}
	defer rows.Close()
	for rows.Next() {
		var recipeID, recipeTypeID string
		var raw json.RawMessage
		var isProduced, isUsed bool
		var layout map[string]any
		if err = rows.Scan(&recipeID, &recipeTypeID, &raw, &isProduced, &isUsed); err != nil {
			return produces, uses, err
		}
		if err = json.Unmarshal(raw, &layout); err != nil {
			return produces, uses, err
		}
		recipe := map[string]any{"id": recipeID, "type": recipeTypeID, "jeiLayout": layout}
		decorations = append(decorations, map[string]any{"revisionId": revisionID, "layout": layout})
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
	if err = s.decorateGlobalRecipeTags(ctx, decorations); err != nil {
		return produces, uses, err
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

func exportDocumentSource(registry string) (assetPath, arrayKey string, ok bool) {
	switch registry {
	case "advancements":
		return "advancements/advancements.json", "advancements", true
	case "key_mappings":
		return "registries/key_mappings.json", "entries", true
	case "biomes":
		return "worldgen/biomes.json", "biomes", true
	case "dimensions":
		return "worldgen/dimensions.json", "dimensions", true
	case "world_structures":
		return "worldgen/structures.json", "structures", true
	case "loot_tables":
		return "worldgen/loot_tables.json", "loot_tables", true
	case "worldgen_data":
		return "worldgen/data_files.json", "files", true
	default:
		return "", "", false
	}
}

func exportCustomDocumentSource(registry string) (assetPath, entriesSQL string, ok bool) {
	switch registry {
	case "natural_generation":
		return "worldgen/natural_generation.json", `select revision.mod_id from mod_export_revisions revision
			join mod_export_text_assets asset on asset.revision_id=revision.id and asset.asset_path=$2
			cross join lateral jsonb_array_elements(coalesce(asset.json_content->'categories','[]'::jsonb)) category(value)
			cross join lateral jsonb_array_elements(coalesce(category.value->'entries','[]'::jsonb)) entry(value)
			where revision.id=$1 and entry.value->>'id'=$3 limit 1`, true
	case "ingredients":
		return "ingredients/ingredients.json", `select revision.mod_id from mod_export_revisions revision
			join mod_export_text_assets asset on asset.revision_id=revision.id and asset.asset_path=$2
			cross join lateral jsonb_array_elements(coalesce(asset.json_content->'types','[]'::jsonb)) ingredient_type(value)
			cross join lateral jsonb_array_elements(coalesce(ingredient_type.value->'entries','[]'::jsonb)) entry(value)
			where revision.id=$1 and coalesce(entry.value->>'resource_location',entry.value->>'unique_id')=$3 limit 1`, true
	default:
		return "", "", false
	}
}
