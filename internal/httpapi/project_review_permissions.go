package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

func projectMutationBypassesReview(claims security.Claims) bool {
	return claimsAllow(claims, "admin.*") || claimsAllow(claims, "project.no-review") || claimsAllow(claims, "content.no-review")
}

func canEditProjectRevision(claims security.Claims, projectType, publicID string) bool {
	if projectType == "mod" {
		return claimsAllow(claims, "project.edit") || claimsAllow(claims, "project.edit."+publicID)
	}
	return claims.Subject > 0 && (claimsAllow(claims, "project.edit."+publicID) || claimsAllow(claims, "admin.*"))
}

func projectEditReviewRequired(config reviewConfig, claims security.Claims, projectType, publicID string) bool {
	switch projectType {
	case "mod":
		return config.ModEdit && !canSkipProjectReview(claims, modIdentityRecord{UniqueID: publicID})
	case "modpack":
		return config.ModpackEdit && !claimsAllow(claims, "content.no-review") && !claimsAllow(claims, "admin.*")
	default:
		return simpleProjectReviewRequired(config, projectType, "edit") && !projectMutationBypassesReview(claims)
	}
}

type manualProjectEdit struct {
	createdContentRevision
	actor  security.Claims
	status string
}

// The caller owns the transaction and applies an approved snapshot with the
// same actor that authorized the revision after both target locks were held.
func createManualProjectEdit(w http.ResponseWriter, r *http.Request, tx pgx.Tx, params createContentRevisionParams) (manualProjectEdit, bool) {
	var edit manualProjectEdit
	if err := lockContentRevisionTx(r.Context(), tx, &params); err != nil {
		if errors.Is(err, errProjectRevisionBaseChanged) {
			writeError(w, http.StatusConflict, errProjectRevisionBaseChanged.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "failed to lock project revision")
		}
		return edit, false
	}
	actor, ok := requireCurrentWriteActor(w, r, tx)
	if !ok {
		return edit, false
	}
	if !canEditProjectRevision(actor, params.EntityType, params.AggregateKey) {
		writeError(w, http.StatusForbidden, "permission denied")
		return edit, false
	}
	params.ActorID = actor.Subject
	params.Status = "approved"
	if antiAbuseModerationRequired(r) || projectEditReviewRequired(loadReviewConfig(r.Context(), tx), actor, params.EntityType, params.AggregateKey) {
		params.Status = "pending"
	}
	created, err := insertLockedContentRevisionTx(r.Context(), tx, params)
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "failed to create project revision")
		}
		return edit, false
	}
	return manualProjectEdit{createdContentRevision: created, actor: actor, status: params.Status}, true
}
