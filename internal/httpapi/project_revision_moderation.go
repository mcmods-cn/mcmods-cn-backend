package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

var errProjectModerationHidden = errors.New("project is hidden by moderation")

type projectRevisionTarget struct {
	publicID            string
	projectType         string
	reviewStatus        string
	publishedRevisionID *int64
}

func lockProjectRevisionTargetTx(ctx context.Context, tx pgx.Tx, aggregateType string, projectID int64) (projectRevisionTarget, error) {
	target := projectRevisionTarget{projectType: aggregateType}
	var err error
	switch aggregateType {
	case "mod":
		err = tx.QueryRow(ctx, `select project_code,review_status,published_revision_id from mods where id=$1 for update`, projectID).
			Scan(&target.publicID, &target.reviewStatus, &target.publishedRevisionID)
	case modpackAggregate:
		err = tx.QueryRow(ctx, `select public_id,review_status,published_revision_id from modpacks where id=$1 for update`, projectID).
			Scan(&target.publicID, &target.reviewStatus, &target.publishedRevisionID)
	case simpleProjectAggregate:
		err = tx.QueryRow(ctx, `select public_id,review_status,published_revision_id,project_type from simple_projects where id=$1 for update`, projectID).
			Scan(&target.publicID, &target.reviewStatus, &target.publishedRevisionID, &target.projectType)
	default:
		return projectRevisionTarget{}, errors.New("unsupported project revision aggregate")
	}
	return target, err
}

// Check the governing record after locking the project row. A query begun
// before the governing transaction commits may see the new locked row but
// still have a snapshot that predates its moderation action.
func checkProjectRevisionModerationTx(ctx context.Context, tx pgx.Tx, aggregateType string, projectID int64) error {
	target, err := lockProjectRevisionTargetTx(ctx, tx, aggregateType, projectID)
	if err != nil {
		return err
	}
	if target.reviewStatus != "rejected" {
		return nil
	}
	reportTargetType := target.projectType
	if reportTargetType == "shader_pack" {
		reportTargetType = "shader"
	}

	var hidden bool
	err = tx.QueryRow(ctx, `select exists(select 1 from moderation_actions
		where action_type='delete' and target_type=$1 and target_public_id=$2
		  and after_summary->>'visibility'='hidden_by_moderation')`, reportTargetType, target.publicID).Scan(&hidden)
	if err != nil {
		return err
	}
	if hidden {
		return errProjectModerationHidden
	}
	return nil
}

func writeProjectModerationConflict(w http.ResponseWriter, r *http.Request, tx pgx.Tx, requestID, actorID int64) {
	if err := markChangeRequestConflictedTx(r.Context(), tx, requestID, actorID, errProjectModerationHidden.Error(), r); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record project review conflict")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project review conflict")
		return
	}
	writeError(w, http.StatusConflict, errProjectModerationHidden.Error())
}

func allowProjectRevisionRejection(w http.ResponseWriter, r *http.Request, tx pgx.Tx, aggregateType string, projectID, requestID, actorID int64) bool {
	err := checkProjectRevisionModerationTx(r.Context(), tx, aggregateType, projectID)
	switch {
	case errors.Is(err, errProjectModerationHidden):
		writeProjectModerationConflict(w, r, tx, requestID, actorID)
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to read project moderation state")
	default:
		return true
	}
	return false
}
