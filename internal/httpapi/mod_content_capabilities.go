package httpapi

import (
	"net/http"

	"mcmods-cn-backend/internal/security"
)

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
