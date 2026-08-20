package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) modGalleryImage(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if siteID == "" || !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "invalid mod gallery image")
		return
	}
	claims := currentClaims(r)
	canReview := claimsAllow(claims, "project.review") || claimsAllow(claims, "admin.*")
	var objectKey, contentType string
	err := s.db.QueryRow(r.Context(), `select file.object_key,file.content_type
		from mod_gallery_images gallery
		join mods mod on mod.id=gallery.mod_id and mod.slug=$1
			and (mod.review_status='approved' or mod.submitted_by=$3 or $4)
		join oss_files file on file.id=gallery.oss_file_id and file.status='active'
		where gallery.public_id=$2`, siteID, publicID, claims.Subject, canReview).Scan(&objectKey, &contentType)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod gallery image not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load mod gallery image")
		return
	}
	s.redirectCatalogOSSAsset(w, r, objectKey, contentType)
}
