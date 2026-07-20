package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) ownedSkinContent(w http.ResponseWriter, r *http.Request) {
	publicID := normalizedSkinPublicID(r.PathValue("publicId"))
	claims := currentClaims(r)
	var ownerID int64
	err := s.db.QueryRow(r.Context(), `select owner_id from skin_assets where public_id=$1 and status='active'`, publicID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skin asset")
		return
	}
	if claims.Subject != ownerID && !isSkinAdmin(claims) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), contentVisibilityBypassKey{}, true))
	if r.Method == http.MethodGet {
		s.catalogEntityContent(w, r)
		return
	}
	s.updateCatalogEntityContent(w, r)
}

func (s *Server) ownedBlueprintContent(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	claims := currentClaims(r)
	var ownerID int64
	err := s.db.QueryRow(r.Context(), `select owner_id from blueprints where public_id=$1 and status<>'deleted'`, publicID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "blueprint was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load blueprint")
		return
	}
	if claims.Subject != ownerID && !hasPermission(claims.Permissions, "admin.*") {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), contentVisibilityBypassKey{}, true))
	if r.Method == http.MethodGet {
		s.catalogEntityContent(w, r)
		return
	}
	s.updateCatalogEntityContent(w, r)
}
