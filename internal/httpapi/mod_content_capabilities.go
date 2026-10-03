package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

// Child content does not make an unpublished parent project public. Keep
// modIdentity for internal editing and resolve the publication boundary here
// in the same query as the public identity, without adding a per-read query.
func (s *Server) readableModIdentity(ctx context.Context, siteID string, claims security.Claims, reviewPermission string) (modIdentityRecord, error) {
	var identity modIdentityRecord
	var approved bool
	err := s.db.QueryRow(ctx, `select id,project_code,slug,submitted_by,review_status='approved'
		from mods where slug=$1`, normalizeModSiteID(siteID)).Scan(
		&identity.ID, &identity.UniqueID, &identity.SiteID, &identity.SubmittedByID, &approved,
	)
	if err != nil {
		return identity, err
	}
	if !approved && !canEditMod(claims, identity) && !claimsAllow(claims, reviewPermission) {
		return modIdentityRecord{}, pgx.ErrNoRows
	}
	return identity, nil
}

type modContentCapabilities struct {
	ManageLayout   bool `json:"manageLayout"`
	CreateResource bool `json:"createResource"`
	EditResource   bool `json:"editResource"`
}

func modContentCapabilitiesFor(claims security.Claims, identity modIdentityRecord) modContentCapabilities {
	editable := canEditMod(claims, identity)
	return modContentCapabilities{
		ManageLayout:   editable,
		CreateResource: editable,
		EditResource:   editable,
	}
}

func markModContentCapabilityResponse(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Authorization")
	w.Header().Add("Vary", "Cookie")
}
