package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

var errCatalogImportPolicyCancel = errors.New("catalog import cancelled by current execution policy")

type catalogImportPolicyCancellation struct{ Code string }

func (e catalogImportPolicyCancellation) Error() string { return e.Code }
func (e catalogImportPolicyCancellation) Is(target error) bool {
	return target == errCatalogImportPolicyCancel
}

func catalogImportRequestedMode(claims security.Claims, projectID string) string {
	if automationScopedPermission(claims, "project.no-review", projectID) || claimsAllow(claims, "content.no-review") {
		return "automatic"
	}
	return "review"
}

// Queue requirements are recorded, never granted. Current account and RBAC
// are resolved on this transaction's connection before execution/publication.
func catalogImportEligibilityTx(ctx context.Context, tx pgx.Tx, jobID, runToken string) (bool, error) {
	var creatorID, targetVersionID int64
	var projectID, mode string
	err := tx.QueryRow(ctx, `select coalesce(job.created_by,0),mod.project_code,job.requested_publication_mode,version.id
 from catalog_import_jobs job join mods mod on mod.id=job.mod_id
 join mod_content_versions version on version.id=job.target_version_id and version.mod_id=job.mod_id
 where job.id=$1 and job.run_token=$2 and job.status in ('validating','importing')`, jobID, runToken).Scan(&creatorID, &projectID, &mode, &targetVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errModExportLeaseLost
	}
	if err != nil {
		return false, err
	}
	// Use the same advisory -> target-row order as review/publication. Taking
	// a share lock here and upgrading it after the advisory lock can deadlock
	// concurrent automatic imports or an independent review of this version.
	if _, err = lockImportedContentVersionTx(ctx, tx, targetVersionID); errors.Is(err, pgx.ErrNoRows) {
		return false, catalogImportPolicyCancellation{"target_archived_reupload_required"}
	} else if err != nil {
		return false, err
	}
	if mode != "review" && mode != "automatic" {
		return false, catalogImportPolicyCancellation{"queue_mode_invalid_reupload_required"}
	}
	var status string
	err = tx.QueryRow(ctx, `select status from users where id=$1 for share`, creatorID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, catalogImportPolicyCancellation{"creator_unavailable_reupload_required"}
	}
	if err != nil {
		return false, err
	}
	if status != "active" {
		return false, catalogImportPolicyCancellation{"creator_unavailable_reupload_required"}
	}
	resolved, err := resolveUsersRootPermissionsQuery(ctx, tx, []int64{creatorID})
	if err != nil {
		return false, err
	}
	actor := security.Claims{Subject: creatorID, PermissionRules: resolved[creatorID].Permissions}
	if claimsExplicitlyAllow(actor, "account.banned") || !automationScopedPermission(actor, "project.edit", projectID) {
		return false, catalogImportPolicyCancellation{"creator_permission_revoked_reupload_required"}
	}
	canActivate := mode == "automatic"
	if canActivate && catalogImportRequestedMode(actor, projectID) != "automatic" {
		return false, catalogImportPolicyCancellation{"automatic_permission_revoked_reupload_required"}
	}
	return canActivate, nil
}

func (s *Server) enforceCatalogImportExecutionPolicy(ctx context.Context, jobID, runToken string) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	_, err = catalogImportEligibilityTx(ctx, tx, jobID, runToken)
	if err == nil {
		return false, tx.Commit(ctx)
	}
	if !errors.Is(err, errCatalogImportPolicyCancel) {
		return false, err
	}
	cause := err
	if err = tx.Rollback(ctx); err != nil {
		return false, err
	}
	if err = s.cancelCatalogImportForPolicy(ctx, jobID, runToken, cause); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) cancelCatalogImportForPolicy(ctx context.Context, jobID, runToken string, cause error) error {
	var policy catalogImportPolicyCancellation
	if !errors.As(cause, &policy) {
		return fmt.Errorf("invalid cancellation cause: %w", cause)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockCatalogImportAttemptTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	if err = s.compensateCatalogImportArtifactsTx(ctx, tx, jobID, runToken, "catalog-import-policy-cancelled"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `delete from catalog_import_revisions where job_id=$1 and status='staging' and import_run_token=$2`, jobID, runToken); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_job_logs(job_id,level,stage,message) values($1,'warn','cancelled',$2)`, jobID, policy.Code); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into audit_events(aggregate_type,aggregate_key,action,metadata) values('catalog_import_job',$1,'execution_cancelled',jsonb_build_object('reason',$2::text,'source','current_execution_policy'))`, jobID, policy.Code); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update catalog_import_jobs set status='cancelled',current_stage='cancelled',progress=least(progress,99),error_code=$3,error_detail=jsonb_build_object('message','Import cancelled by current permissions or target state; upload again.'),finished_at=now(),heartbeat_at=now(),updated_at=now(),run_token='' where id=$1 and run_token=$2`, jobID, runToken, policy.Code); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
