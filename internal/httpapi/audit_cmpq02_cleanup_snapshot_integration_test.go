package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAuditCMPQ02CleanupDeletesOnlyPreviewedCommittedMembershipHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "log.write")
	delayed, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer delayed.Rollback(f.ctx)
	var lowLateID, visibleID, highLateID int64
	if err = delayed.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,$2,$3,now()-interval '3 days') returning id`, f.userIDs[f.otherEditor], activityActionIDs["view"], activityObjectTypeIDs["user"]).Scan(&lowLateID); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,$2,$3,now()-interval '2 days') returning id`, f.userIDs[f.editor], activityActionIDs["view"], activityObjectTypeIDs["user"]).Scan(&visibleID); err != nil {
		t.Fatal(err)
	}
	if lowLateID >= visibleID {
		t.Fatalf("delayed lower sequence barrier invalid: %d >= %d", lowLateID, visibleID)
	}
	var preview struct {
		Data struct {
			PreviewID    string `json:"previewId"`
			Token        string `json:"confirmationToken"`
			Confirmation string `json:"confirmationText"`
			Matched      int64  `json:"matchedCount"`
		} `json:"data"`
	}
	if err = json.Unmarshal(f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/preview", map[string]any{"actions": []string{"view"}, "objectTypes": []string{"user"}, "to": "2030-01-01T00:00:00Z"}, 200), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Data.Matched != 1 || preview.Data.Confirmation != "DELETE 1" {
		t.Fatalf("preview=%d want=1", preview.Data.Matched)
	}
	// The explicit commit is the deterministic barrier: this lower sequence row
	// existed in an uncommitted transaction during preview and becomes visible now.
	if err = delayed.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,$2,$3,now()-interval '4 days') returning id`, f.userIDs[f.denied], activityActionIDs["view"], activityObjectTypeIDs["user"]).Scan(&highLateID); err != nil {
		t.Fatal(err)
	}
	request := activityCleanupExecuteRequest{PreviewID: preview.Data.PreviewID, ConfirmationToken: preview.Data.Token, Confirmation: preview.Data.Confirmation}
	// Explicit JSON names avoid relying on permissive field-name decoding.
	var result struct {
		Data struct {
			Matched int64  `json:"matchedCount"`
			Deleted int64  `json:"deletedCount"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	if err = json.Unmarshal(f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/execute", request, 200), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Matched != 1 || result.Data.Deleted != 1 || result.Data.Status != "completed" {
		t.Errorf("preview membership violated: matched=%d deleted=%d status=%s", result.Data.Matched, result.Data.Deleted, result.Data.Status)
	}
	var lateRemaining, visibleRemaining int
	if err = f.db.QueryRow(f.ctx, `select count(*) filter(where id=any($1::bigint[])),count(*) filter(where id=$2) from user_activity_events where id=any($3::bigint[])`, []int64{lowLateID, highLateID}, visibleID, []int64{lowLateID, visibleID, highLateID}).Scan(&lateRemaining, &visibleRemaining); err != nil {
		t.Fatal(err)
	}
	if lateRemaining != 2 || visibleRemaining != 0 {
		t.Errorf("actual rows late=%d want=2 visible=%d want=0", lateRemaining, visibleRemaining)
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/execute", request, 409)
	var auditMatched, auditDeleted int64
	if err = f.db.QueryRow(f.ctx, `select matched_count,deleted_count from activity_cleanup_runs where public_id=$1`, preview.Data.PreviewID).Scan(&auditMatched, &auditDeleted); err != nil {
		t.Fatal(err)
	}
	if auditMatched != 1 || auditDeleted != 1 {
		t.Errorf("audit matched=%d deleted=%d want=1/1", auditMatched, auditDeleted)
	}
}

func TestAuditCMPQ02LegacyPreviewAndRecoveryFailClosedHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "log.write")
	var eventID int64
	if err := f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,3,7,now()-interval '2 days') returning id`, f.userIDs[f.editor]).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	filter := normalizedActivityCleanupFilter{UserIDs: []int64{f.userIDs[f.editor]}, ActionIDs: []int16{3}, SnapshotBefore: time.Now().UTC(), BatchSize: 1000, Summary: activityCleanupFilterRequest{Actions: []string{"view"}}}
	raw, err := json.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	token, hash, err := newCleanupConfirmationToken()
	if err != nil {
		t.Fatal(err)
	}
	var previewID, runningID string
	if err = f.db.QueryRow(f.ctx, `insert into activity_cleanup_runs(source,initiated_by,status,filters,confirmation_hash,matched_count,expires_at) values('manual',$1,'preview',$2::jsonb,$3,1,now()+interval '15 minutes') returning public_id`, f.userIDs[f.editor], raw, hash).Scan(&previewID); err != nil {
		t.Fatal(err)
	}
	response := f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/execute", activityCleanupExecuteRequest{PreviewID: previewID, ConfirmationToken: token, Confirmation: "DELETE 1"}, 409)
	if !strings.Contains(string(response), "ACTIVITY_CLEANUP_PREVIEW_REFRESH_REQUIRED") {
		t.Fatal("legacy preview did not request refresh")
	}
	if err = f.db.QueryRow(f.ctx, `insert into activity_cleanup_runs(source,initiated_by,status,filters,matched_count,started_at) values('manual',$1,'running',$2::jsonb,1,now()-interval '2 minutes') returning public_id`, f.userIDs[f.editor], raw).Scan(&runningID); err != nil {
		t.Fatal(err)
	}
	repairActivityCleanupRuns(f.ctx, f.db)
	var present bool
	var previewStatus, runningStatus string
	var deleted int64
	if err = f.db.QueryRow(f.ctx, `select exists(select 1 from user_activity_events where id=$1),(select status from activity_cleanup_runs where public_id=$2),(select status from activity_cleanup_runs where public_id=$3),(select deleted_count from activity_cleanup_runs where public_id=$3)`, eventID, previewID, runningID).Scan(&present, &previewStatus, &runningStatus, &deleted); err != nil {
		t.Fatal(err)
	}
	if !present || previewStatus != "preview" || runningStatus != "failed" || deleted != 0 {
		t.Fatalf("legacy cleanup expanded scope: present=%v preview=%s running=%s deleted=%d", present, previewStatus, runningStatus, deleted)
	}
}

func TestAuditCMPQ02EmptyPreviewDoesNotAdoptLateRowsHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "log.write")
	var preview struct {
		Data struct {
			ID           string `json:"previewId"`
			Token        string `json:"confirmationToken"`
			Confirmation string `json:"confirmationText"`
			Matched      int64  `json:"matchedCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/preview", map[string]any{"actions": []string{"view"}, "objectTypes": []string{"user"}, "to": "2030-01-01T00:00:00Z"}, 200), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Data.Matched != 0 || preview.Data.Confirmation != "DELETE 0" {
		t.Fatalf("empty preview count=%d", preview.Data.Matched)
	}
	var id int64
	if err := f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,3,7,now()-interval '3 days') returning id`, f.userIDs[f.editor]).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Data struct {
			Deleted int64 `json:"deletedCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/execute", activityCleanupExecuteRequest{PreviewID: preview.Data.ID, ConfirmationToken: preview.Data.Token, Confirmation: preview.Data.Confirmation}, 200), &result); err != nil {
		t.Fatal(err)
	}
	var present bool
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from user_activity_events where id=$1)`, id).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if result.Data.Deleted != 0 || !present {
		t.Fatalf("empty snapshot adopted late row: deleted=%d present=%v", result.Data.Deleted, present)
	}
}
