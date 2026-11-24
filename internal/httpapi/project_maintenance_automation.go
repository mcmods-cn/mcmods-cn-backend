package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	automatedMaintenanceLowFrequency = "lowFrequency"
	automatedMaintenanceDiscontinued = "discontinued"
)

type projectMaintenanceResult struct {
	PreviousStatus      string    `json:"previousStatus"`
	CurrentStatus       string    `json:"currentStatus"`
	LastProjectChangeAt time.Time `json:"lastProjectChangeAt"`
	Changed             bool      `json:"changed"`
	Restored            bool      `json:"restored"`
	ManualOverride      bool      `json:"manualOverride"`
}

func desiredProjectMaintenanceStatus(lastProjectChange, now time.Time) string {
	if lastProjectChange.IsZero() {
		return ""
	}
	if lastProjectChange.Before(now.AddDate(-1, 0, 0)) {
		return automatedMaintenanceDiscontinued
	}
	if lastProjectChange.Before(now.AddDate(0, -6, 0)) {
		return automatedMaintenanceLowFrequency
	}
	return ""
}

func latestProviderFileActivity(files []providerProjectFile) time.Time {
	var latest time.Time
	for _, file := range files {
		if file.PublishedAt.After(latest) {
			latest = file.PublishedAt
		}
	}
	return latest
}

func latestProviderReleaseActivity(releases []projectAutomationRelease) time.Time {
	var latest time.Time
	for _, release := range releases {
		if release.PublishedAt.After(latest) {
			latest = release.PublishedAt
		}
	}
	return latest
}

// applyProjectMaintenancePolicy records the latest upstream activity and owns
// only status values that it previously set. A later manual status change
// suspends automation until the next genuinely newer upstream event.
func (worker *ProjectAutomationWorker) applyProjectMaintenancePolicy(
	ctx context.Context,
	job projectAutomationJob,
	observedExternalChange time.Time,
	now time.Time,
) (projectMaintenanceResult, error) {
	result := projectMaintenanceResult{}
	if job.ActorID <= 0 {
		return result, errors.New("automation actor is required for maintenance status updates")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if observedExternalChange.After(now) {
		observedExternalChange = now
	}

	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = lockProjectAutomationRunTx(ctx, tx, job); err != nil {
		return result, err
	}

	var currentStatus, reviewStatus, publicID string
	var createdAt time.Time
	var publishedRevisionID *int64
	if job.ProjectType == "mod" {
		err = tx.QueryRow(ctx, `select official_status,review_status,project_code,created_at,published_revision_id from mods where id=$1 for update`, job.InternalID).
			Scan(&currentStatus, &reviewStatus, &publicID, &createdAt, &publishedRevisionID)
	} else {
		err = tx.QueryRow(ctx, `select official_status,review_status,public_id,created_at,published_revision_id from simple_projects
			where id=$1 and project_type=$2 for update`, job.InternalID, job.ProjectType).
			Scan(&currentStatus, &reviewStatus, &publicID, &createdAt, &publishedRevisionID)
	}
	if err != nil {
		return result, fmt.Errorf("load project maintenance status: %w", err)
	}
	if publicID != job.ProjectPublicID {
		return result, errProjectAutomationParentUnavailable
	}
	result.PreviousStatus = currentStatus
	result.CurrentStatus = currentStatus
	if reviewStatus != "approved" {
		return result, nil
	}
	if _, err = authorizeAutomationProject(ctx, tx, job.ActorID, publicID, false, true); err != nil {
		return result, err
	}

	lastProjectChange := createdAt.UTC()
	var automatedStatus *string
	var statusBeforeAutomation string
	var manualOverride bool
	var hasExternalActivity bool
	rowExists := true
	var persistedProjectChange time.Time
	err = tx.QueryRow(ctx, `select last_project_change_at,has_external_activity,automated_status,status_before_automation,manual_status_override
		from project_automation_activity where project_route_id=$1 for update`, job.RouteID).Scan(
		&persistedProjectChange, &hasExternalActivity, &automatedStatus, &statusBeforeAutomation, &manualOverride,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		rowExists = false
		err = nil
	}
	if err != nil {
		return result, err
	}
	if rowExists {
		lastProjectChange = persistedProjectChange
	}

	if automatedStatus != nil && currentStatus != *automatedStatus {
		// Someone changed the status after automation. Preserve that decision
		// rather than immediately fighting it on every worker tick.
		automatedStatus = nil
		statusBeforeAutomation = ""
		manualOverride = true
	}
	newProjectActivity := false
	if !observedExternalChange.IsZero() && (!hasExternalActivity || observedExternalChange.After(lastProjectChange)) {
		lastProjectChange = observedExternalChange.UTC()
		hasExternalActivity = true
		newProjectActivity = true
	}
	if newProjectActivity {
		manualOverride = false
	}
	if lastProjectChange.After(now) {
		lastProjectChange = now
	}
	if currentStatus == "archived" {
		manualOverride = true
		automatedStatus = nil
		statusBeforeAutomation = ""
	}

	desired := desiredProjectMaintenanceStatus(lastProjectChange, now)
	if !manualOverride {
		switch {
		case desired != "" && automatedStatus != nil:
			if currentStatus != desired {
				if err = publishAutomatedProjectMaintenanceStatusTx(ctx, tx, job, publishedRevisionID, currentStatus, desired); err != nil {
					return result, err
				}
				result.Changed = true
				currentStatus = desired
			}
			value := desired
			automatedStatus = &value
		case desired != "" && currentStatus != desired:
			statusBeforeAutomation = currentStatus
			if err = publishAutomatedProjectMaintenanceStatusTx(ctx, tx, job, publishedRevisionID, currentStatus, desired); err != nil {
				return result, err
			}
			value := desired
			automatedStatus = &value
			currentStatus = desired
			result.Changed = true
		case desired == "" && automatedStatus != nil:
			restore := statusBeforeAutomation
			if !validProjectOfficialStatus(restore) {
				restore = "active"
			}
			if currentStatus == *automatedStatus && currentStatus != restore {
				if err = publishAutomatedProjectMaintenanceStatusTx(ctx, tx, job, publishedRevisionID, currentStatus, restore); err != nil {
					return result, err
				}
				currentStatus = restore
				result.Changed = true
				result.Restored = true
			}
			automatedStatus = nil
			statusBeforeAutomation = ""
		}
	}

	if rowExists {
		_, err = tx.Exec(ctx, `update project_automation_activity set
			last_project_change_at=$2,has_external_activity=$3,last_checked_at=$4,automated_status=$5,status_before_automation=$6,
			manual_status_override=$7,last_source_type=$8,last_update_kind=$9,changed_by=$10,updated_at=now()
			where project_route_id=$1`, job.RouteID, lastProjectChange, hasExternalActivity, now, automatedStatus, statusBeforeAutomation,
			manualOverride, job.SourceType, job.Kind, job.ActorID)
	} else {
		_, err = tx.Exec(ctx, `insert into project_automation_activity(
			project_route_id,last_project_change_at,has_external_activity,last_checked_at,automated_status,status_before_automation,
			manual_status_override,last_source_type,last_update_kind,changed_by)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, job.RouteID, lastProjectChange, hasExternalActivity, now, automatedStatus,
			statusBeforeAutomation, manualOverride, job.SourceType, job.Kind, job.ActorID)
	}
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	result.CurrentStatus = currentStatus
	result.LastProjectChangeAt = lastProjectChange
	result.ManualOverride = manualOverride
	return result, nil
}

func publishAutomatedProjectMaintenanceStatusTx(ctx context.Context, tx pgx.Tx, job projectAutomationJob,
	baseRevisionID *int64, previousStatus, status string) error {
	if !validProjectOfficialStatus(status) || previousStatus == status {
		return fmt.Errorf("invalid automated project status transition %q -> %q", previousStatus, status)
	}
	if baseRevisionID == nil {
		return errors.New("automated project maintenance requires a published revision")
	}
	aggregateType := simpleProjectAggregate
	if job.ProjectType == "mod" {
		aggregateType = "mod"
	} else if normalizeSimpleProjectType(job.ProjectType) == "" {
		return fmt.Errorf("unsupported automated maintenance project type %q", job.ProjectType)
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `select snapshot from content_revisions
		where id=$1 and entity_type=$2 and entity_id=$3 and aggregate_type=$4 and aggregate_key=$5`,
		*baseRevisionID, job.ProjectType, job.InternalID, aggregateType, job.ProjectPublicID).Scan(&raw); err != nil {
		return fmt.Errorf("load published project revision for automated maintenance: %w", err)
	}
	// Compatibility discovery commits independently of maintenance publication.
	// Carry those live facts into the revision without reapplying old project content.
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot == nil {
		return errors.New("invalid published snapshot for automated maintenance")
	}
	var compat map[string]json.RawMessage
	if job.ProjectType == "mod" {
		err := tx.QueryRow(ctx, `select jsonb_build_object('compatibilities',coalesce(jsonb_agg(to_jsonb(c) order by loader),'[]'::jsonb))
			from (select loader,array_agg(minecraft_version order by minecraft_version desc) versions
				from mod_loader_compatibilities where mod_id=$1 group by loader) c`, job.InternalID).Scan(&compat)
		if err != nil {
			return fmt.Errorf("read mod compatibility for maintenance revision: %w", err)
		}
	} else {
		err := tx.QueryRow(ctx, `select jsonb_build_object('minecraftVersions',minecraft_versions,'loaders',loaders)
			from simple_projects where id=$1 and project_type=$2`, job.InternalID, job.ProjectType).Scan(&compat)
		if err != nil {
			return fmt.Errorf("read project compatibility for maintenance revision: %w", err)
		}
	}
	for field, value := range compat {
		previous := snapshot[field]
		if bytes.Equal(value, []byte("[]")) && (previous == nil || bytes.Equal(previous, []byte("null"))) {
			continue
		}
		snapshot[field] = value
	}
	snapshot["officialStatus"], _ = json.Marshal(status)
	nextRaw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode automated project maintenance revision: %w", err)
	}
	reason := fmt.Sprintf("Automated maintenance status: %s -> %s", previousStatus, status)
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType: job.ProjectType, EntityID: job.InternalID, AggregateType: aggregateType, AggregateKey: job.ProjectPublicID,
		BaseRevision: baseRevisionID, Snapshot: nextRaw, Reason: reason, ActorID: job.ActorID, Source: "auto_update", Status: "approved",
		Metadata: map[string]any{"automation": "project_maintenance", "previousStatus": previousStatus, "status": status,
			"sourceType": job.SourceType, "updateKind": job.Kind},
	})
	if err != nil {
		return fmt.Errorf("create automated project maintenance revision: %w", err)
	}
	update := `update mods set official_status=$2,published_revision_id=$3,updated_at=now()
		where id=$1 and published_revision_id=$4 and official_status=$5 and review_status='approved'`
	if job.ProjectType != "mod" {
		update = `update simple_projects set official_status=$2,published_revision_id=$3,updated_at=now()
			where id=$1 and published_revision_id=$4 and official_status=$5 and review_status='approved'`
	}
	tag, err := tx.Exec(ctx, update, job.InternalID, status, created.RevisionID, *baseRevisionID, previousStatus)
	if err != nil {
		return fmt.Errorf("apply automated maintenance status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errProjectAutomationParentUnavailable
	}
	if err = appendReviewResolutionTx(ctx, tx, created.ChangeRequestID, "approved", job.ActorID, reason, nil); err != nil {
		return fmt.Errorf("approve automated project maintenance revision: %w", err)
	}
	if err = appendCatalogPublishedReviewEventTx(ctx, tx, created.ChangeRequestID, job.ActorID, reason, nil); err != nil {
		return fmt.Errorf("publish automated project maintenance revision: %w", err)
	}
	return nil
}

func validProjectOfficialStatus(status string) bool {
	switch status {
	case "active", "lowFrequency", "discontinued", "archived", "development":
		return true
	default:
		return false
	}
}
