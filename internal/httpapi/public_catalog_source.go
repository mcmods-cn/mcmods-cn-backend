package httpapi

import (
	"context"
	"net/http"

	"mcmods-cn-backend/internal/catalogvisibility"
	"mcmods-cn-backend/internal/security"
)

// publicCatalogContext prevents an editor's private project preview from being
// reused by a public catalog response or its shared cache.
func publicCatalogContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, claimsContextKey, security.Claims{})
}

// Keep the HTTP call sites explicit while search projections share the same
// source policy through catalogvisibility.
func publicCatalogEntitySQL(alias, entityType string) string {
	return catalogvisibility.EntitySQL(alias, entityType)
}

func (s *Server) catalogEntityIsPublic(ctx context.Context, entityID int64, entityType string) (bool, error) {
	var visible bool
	err := s.db.QueryRow(ctx, `select exists(select 1 from catalog_entities public_entity
	 where public_entity.id=$1 and public_entity.status='active' and public_entity.archived_at is null
	 and `+publicCatalogEntitySQL("public_entity", entityType)+`)`, entityID).Scan(&visible)
	return visible, err
}

func (s *Server) requirePublicCatalogEntity(w http.ResponseWriter, r *http.Request, entity catalogEditorEntity) bool {
	visible, err := s.catalogEntityIsPublic(r.Context(), entity.ID, entity.EntityType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read catalog publication state")
		return false
	}
	if !visible {
		writeError(w, http.StatusNotFound, "catalog entry not found")
		return false
	}
	return true
}
