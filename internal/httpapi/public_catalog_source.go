package httpapi

import (
	"context"
	"net/http"

	"mcmods-cn-backend/internal/catalogvisibility"
	"mcmods-cn-backend/internal/security"
)

// publicImportRevisionSQL checks the exact fallback source, aliased as revision.
// Callers allow independently authorized previews of the requested revision separately.
const publicImportRevisionSQL = `revision.is_active and revision.status in ('ready','partial')
	and exists(select 1 from mods source_mod
		join mod_content_versions source_version on source_version.id=revision.target_version_id
			and source_version.mod_id=source_mod.id and source_version.status='active'
		where source_mod.id=revision.mod_id and source_mod.review_status='approved')`

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

func catalogEntityIsPublic(ctx context.Context, query databaseQuery, entityID int64, entityType string) (bool, error) {
	var visible bool
	err := query.QueryRow(ctx, `select exists(select 1 from catalog_entities public_entity
	 where public_entity.id=$1 and public_entity.status='active' and public_entity.archived_at is null
	 and `+publicCatalogEntitySQL("public_entity", entityType)+`)`, entityID).Scan(&visible)
	return visible, err
}

func (s *Server) requirePublicCatalogEntity(w http.ResponseWriter, r *http.Request, entity catalogEditorEntity) bool {
	visible, err := catalogEntityIsPublic(r.Context(), s.db, entity.ID, entity.EntityType)
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
