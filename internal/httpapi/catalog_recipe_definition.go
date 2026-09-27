package httpapi

import (
	"context"
	"encoding/json"
)

func reconcileCatalogRecipeDefinition(requested, stored map[string]any, editing bool) (map[string]any, error) {
	if !editing {
		if len(requested) != 0 {
			return nil, errCatalogEditorInvalid
		}
		return map[string]any{}, nil
	}
	stored = nonNilJSONObject(stored)
	// Missing is a backwards-compatible preserve operation. If the client does
	// echo this read-only field, it must echo the complete current value so an
	// old editor cannot turn an opaque definition into an empty object.
	if requested != nil && string(catalogJSON(requested)) != string(catalogJSON(stored)) {
		return nil, errCatalogEditorConflict
	}
	return stored, nil
}

func (s *Server) catalogRecipeDefinitionForMutation(ctx context.Context, recipeID int64, requested map[string]any) (map[string]any, error) {
	if recipeID <= 0 {
		return reconcileCatalogRecipeDefinition(requested, nil, false)
	}
	var raw []byte
	if err := s.db.QueryRow(ctx, `select coalesce((select definition from recipe_definitions where recipe_id=$1),'{}'::jsonb)`, recipeID).Scan(&raw); err != nil {
		return nil, err
	}
	stored := map[string]any{}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	return reconcileCatalogRecipeDefinition(requested, stored, true)
}
