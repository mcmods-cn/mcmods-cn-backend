package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

var errProjectModerationHidden = errors.New("project is hidden by moderation")

// Check the governing record after locking the project row. A query begun
// before the governing transaction commits may see the new locked row but
// still have a snapshot that predates its moderation action.
func checkProjectRevisionModerationTx(ctx context.Context, tx pgx.Tx, aggregateType string, projectID int64) error {
	targetType := aggregateType
	var publicID, reviewStatus string
	var err error
	switch aggregateType {
	case "mod":
		err = tx.QueryRow(ctx, `select project_code,review_status from mods where id=$1 for update`, projectID).
			Scan(&publicID, &reviewStatus)
	case modpackAggregate:
		err = tx.QueryRow(ctx, `select public_id,review_status from modpacks where id=$1 for update`, projectID).
			Scan(&publicID, &reviewStatus)
	case simpleProjectAggregate:
		err = tx.QueryRow(ctx, `select public_id,review_status,project_type from simple_projects where id=$1 for update`, projectID).
			Scan(&publicID, &reviewStatus, &targetType)
	default:
		return errors.New("unsupported project revision aggregate")
	}
	if err != nil {
		return err
	}
	if reviewStatus != "rejected" {
		return nil
	}
	if targetType == "shader_pack" {
		targetType = "shader"
	}

	var hidden bool
	err = tx.QueryRow(ctx, `select exists(select 1 from moderation_actions
		where action_type='delete' and target_type=$1 and target_public_id=$2
		  and after_summary->>'visibility'='hidden_by_moderation')`, targetType, publicID).Scan(&hidden)
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
