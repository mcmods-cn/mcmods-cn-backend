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
	if assetPath, arrayKey, ok := exportDocumentSource(registry); ok {
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
	if registry != "advancements" && registry != "key_mappings" {
		response.Recipes, response.Uses = s.modExportRecipesForObject(r.Context(), revisionID, objectID)
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
	if assetPath, arrayKey, ok := exportDocumentSource(request.Registry); ok {
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
	_, err = s.db.Exec(r.Context(), `
		insert into mod_export_entry_contents(mod_id,registry,object_id,locale,content_markdown,updated_by)
		values($1,$2,$3,$4,$5,$6)
		on conflict(mod_id,registry,object_id,locale) do update
		set content_markdown=excluded.content_markdown,updated_by=excluded.updated_by,updated_at=now()`,
		identity.ID, request.Registry, request.ObjectID, request.Locale, request.ContentMarkdown, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save entry content")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locale": request.Locale, "contentMarkdown": request.ContentMarkdown})
}

func (s *Server) modExportRecipesForObject(ctx context.Context, revisionID, objectID string) ([]any, []any) {
	var raw []byte
	if err := s.db.QueryRow(ctx, `select json_content from mod_export_text_assets where revision_id=$1 and asset_path='recipes/recipes.json'`, revisionID).Scan(&raw); err != nil {
		return []any{}, []any{}
	}
	var document struct {
		Recipes []json.RawMessage `json:"recipes"`
	}
	if json.Unmarshal(raw, &document) != nil {
		return []any{}, []any{}
	}
	produces := make([]any, 0, 8)
	uses := make([]any, 0, 8)
	for _, recipeRaw := range document.Recipes {
		var recipe map[string]any
		if json.Unmarshal(recipeRaw, &recipe) != nil {
			continue
		}
		if exportRecipeProduces(recipe, objectID) && len(produces) < 100 {
			produces = append(produces, recipe)
		}
		if exportRecipeUses(recipe, objectID) && len(uses) < 100 {
			uses = append(uses, recipe)
		}
	}
	s.decorateModExportRecipeLayouts(ctx, revisionID, produces, uses)
	return produces, uses
}

func (s *Server) decorateModExportRecipeLayouts(ctx context.Context, revisionID string, groups ...[]any) {
	layoutPaths := make([]string, 0, 32)
	recipeIDsByPath := make(map[string]string)
	for _, group := range groups {
		for _, value := range group {
			recipe, _ := value.(map[string]any)
			id, _ := recipe["id"].(string)
			recipeType, _ := recipe["type"].(string)
			if layoutPath := exportRecipeLayoutPath(recipeType, id); layoutPath != "" {
				if _, exists := recipeIDsByPath[layoutPath]; !exists {
					layoutPaths = append(layoutPaths, layoutPath)
					recipeIDsByPath[layoutPath] = id
				}
			}
		}
	}
	if len(layoutPaths) == 0 {
		return
	}
	rows, err := s.db.Query(ctx, `select asset_path,json_content from mod_export_text_assets
		where revision_id=$1 and asset_path=any($2::text[])`, revisionID, layoutPaths)
	if err != nil {
		return
	}
	defer rows.Close()
	layouts := make(map[string]map[string]any, len(layoutPaths))
	for rows.Next() {
		var assetPath string
		var raw []byte
		var layout map[string]any
		if rows.Scan(&assetPath, &raw) != nil || json.Unmarshal(raw, &layout) != nil {
			continue
		}
		id, _ := layout["recipe_id"].(string)
		if id == "" {
			id = recipeIDsByPath[assetPath]
		}
		if id != "" {
			layouts[id] = layout
		}
	}
	for _, group := range groups {
		for _, value := range group {
			recipe, _ := value.(map[string]any)
			id, _ := recipe["id"].(string)
			if layout := layouts[id]; layout != nil {
				recipe["jeiLayout"] = layout
			}
		}
	}
}

func exportRecipeLayoutPath(recipeType, recipeID string) string {
	if recipeID == "" {
		return ""
	}
	category := strings.TrimSpace(recipeType)
	switch category {
	case "minecraft:smelting":
		category = "minecraft:furnace"
	case "minecraft:campfire_cooking":
		category = "minecraft:campfire"
	}
	parts := strings.SplitN(category, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	filePath := strings.Replace(recipeID, ":", "__", 1) + ".json"
	return "recipes/jei/layouts/" + parts[0] + "/" + parts[1] + "/" + filePath
}

func exportRecipeProduces(recipe map[string]any, objectID string) bool {
	result, _ := recipe["result"].(map[string]any)
	item, _ := result["item"].(string)
	return item == objectID
}

func exportRecipeUses(recipe map[string]any, objectID string) bool {
	ingredients, _ := recipe["ingredients"].([]any)
	for _, ingredientValue := range ingredients {
		ingredient, _ := ingredientValue.(map[string]any)
		alternatives, _ := ingredient["alternatives"].([]any)
		for _, alternativeValue := range alternatives {
			alternative, _ := alternativeValue.(map[string]any)
			if item, _ := alternative["item"].(string); item == objectID {
				return true
			}
		}
	}
	return false
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
	default:
		return "", "", false
	}
}
