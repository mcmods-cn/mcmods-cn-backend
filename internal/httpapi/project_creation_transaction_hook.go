package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

type projectCreationTransactionResult struct {
	ProjectType      string
	ProjectID        int64
	ProjectPublicID  string
	SiteID           string
	ProjectTitle     string
	TargetURL        string
	ReviewStatus     string
	ChangeRequestID  int64
	ChangeRequestUID string
}

// The crawler has a service identity and a lease. Its callbacks run on the
// creation transaction so a project and its external source commit together.
type projectCreationTransactionHook struct {
	actor  func(context.Context, pgx.Tx) (security.Claims, error)
	finish func(context.Context, pgx.Tx, projectCreationTransactionResult) error
}

type projectCreationTransactionHookContextKey struct{}

func withProjectCreationTransactionHook(ctx context.Context, hook projectCreationTransactionHook) context.Context {
	return context.WithValue(ctx, projectCreationTransactionHookContextKey{}, hook)
}

func runProjectCreationTransactionHook(ctx context.Context, tx pgx.Tx, result projectCreationTransactionResult) error {
	hook, ok := ctx.Value(projectCreationTransactionHookContextKey{}).(projectCreationTransactionHook)
	if !ok {
		return nil
	}
	return hook.finish(ctx, tx, result)
}

// Resolve the actor after asset preparation and final file validation, just
// before creating the project. Each kind keeps its own creation and review policy.
func currentProjectCreation(w http.ResponseWriter, r *http.Request, tx pgx.Tx, projectType string) (security.Claims, string, bool) {
	var actor security.Claims
	var err error
	if hook, ok := r.Context().Value(projectCreationTransactionHookContextKey{}).(projectCreationTransactionHook); ok {
		actor, err = hook.actor(r.Context(), tx)
		if err != nil {
			if errors.Is(err, errAutomationPermissionDenied) {
				writeError(w, http.StatusForbidden, "project creation permission is required")
			} else {
				writeError(w, http.StatusServiceUnavailable, "project creation authorization is unavailable")
			}
			return actor, "", false
		}
	} else {
		actor, err = resolveCurrentWriteActorTx(r.Context(), tx, currentClaims(r))
		if err != nil {
			writeCurrentWriteActorFailure(w, err)
			return actor, "", false
		}
	}
	config := loadReviewConfig(r.Context(), tx)
	var allowed, review bool
	switch projectType {
	case "mod":
		allowed = claimsAllow(actor, "project.create")
		review = config.ModCreate && !projectMutationBypassesReview(actor)
	case "modpack":
		allowed = claimsAllow(actor, "modpack.create")
		review = config.ModpackCreate && !claimsAllow(actor, "content.no-review") && !claimsAllow(actor, "admin.*")
	default:
		allowed = claimsAllow(actor, "project.create."+projectType) || claimsAllow(actor, "admin.*")
		review = simpleProjectReviewRequired(config, projectType, "create") && !projectMutationBypassesReview(actor)
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "project creation permission is required")
		return actor, "", false
	}
	if review || antiAbuseModerationRequired(r) {
		return actor, "pending", true
	}
	return actor, "approved", true
}
