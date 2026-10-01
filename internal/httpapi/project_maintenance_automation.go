package httpapi

import (
	"context"
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
	if err = lockAutomationJobTx(ctx, tx, job); err != nil {
		return result, err
	}

	var currentStatus string
	var createdAt time.Time
	if job.ProjectType == "mod" {
		err = tx.QueryRow(ctx, `select official_status,created_at from mods where id=$1 for update`, job.InternalID).Scan(&currentStatus, &createdAt)
	} else {
		err = tx.QueryRow(ctx, `select official_status,created_at from simple_projects where id=$1 and project_type=$2 for update`, job.InternalID, job.ProjectType).Scan(&currentStatus, &createdAt)
	}
	if err != nil {
		return result, fmt.Errorf("load project maintenance status: %w", err)
	}
	result.PreviousStatus = currentStatus
	result.CurrentStatus = currentStatus

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
				if err = updateAutomatedProjectStatus(ctx, tx, job, desired); err != nil {
					return result, err
				}
				result.Changed = true
				currentStatus = desired
			}
			value := desired
			automatedStatus = &value
		case desired != "" && currentStatus != desired:
			statusBeforeAutomation = currentStatus
			if err = updateAutomatedProjectStatus(ctx, tx, job, desired); err != nil {
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
				if err = updateAutomatedProjectStatus(ctx, tx, job, restore); err != nil {
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

func updateAutomatedProjectStatus(ctx context.Context, tx pgx.Tx, job projectAutomationJob, status string) error {
	if !validProjectOfficialStatus(status) {
		return fmt.Errorf("invalid automated project status %q", status)
	}
	var err error
	if job.ProjectType == "mod" {
		_, err = tx.Exec(ctx, `update mods set official_status=$2,updated_at=now() where id=$1`, job.InternalID, status)
	} else {
		_, err = tx.Exec(ctx, `update simple_projects set official_status=$3,updated_at=now() where id=$1 and project_type=$2`, job.InternalID, job.ProjectType, status)
	}
	return err
}

func validProjectOfficialStatus(status string) bool {
	switch status {
	case "active", "lowFrequency", "discontinued", "archived", "development":
		return true
	default:
		return false
	}
}
