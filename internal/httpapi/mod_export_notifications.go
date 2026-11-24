package httpapi

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// Terminal job facts and their notification intent share the same transaction.
// The fenced state transition calls this once; event redelivery retains the
// committed event ID and the existing NotificationWorker deduplication.
func enqueueModExportResultTx(ctx context.Context, tx pgx.Tx, jobID, status, failureMessage string) error {
	var recipientID int64
	var siteID, modName string
	var skipped int
	err := tx.QueryRow(ctx,
		`select coalesce(j.created_by,0),m.slug,m.primary_name,coalesce((j.error_detail->>'translationValuesSkipped')::int,0)
		 from catalog_import_jobs j join mods m on m.id=j.mod_id where j.id=$1`, jobID,
	).Scan(&recipientID, &siteID, &modName, &skipped)
	if err != nil {
		return fmt.Errorf("load import notification recipient: %w", err)
	}
	// A deleted account has no recipient; preserve the existing policy.
	if recipientID <= 0 {
		return nil
	}
	code := "mod_import_success"
	values := map[string]string{"name": modName, "skipped": fmt.Sprintf("%d", skipped), "error": ""}
	if status == "partial" {
		code = "mod_import_partial"
	}
	if status == "failed" {
		code = "mod_import_failure"
		values["error"] = "未知错误"
		if failureMessage != "" {
			values["error"] = failureMessage
		}
	}
	return enqueueTemplatedNotificationTx(ctx, tx, "notification.direct", recipientID, 0, code, values, map[string]any{
		"type": "mod_export_import", "jobId": jobID, "modSiteId": siteID, "status": status,
		"translationValuesSkipped": skipped, "targetLabel": modName, "url": "/mods/" + siteID,
	}, "")
}
