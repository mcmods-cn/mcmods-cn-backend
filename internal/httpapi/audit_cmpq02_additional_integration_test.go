package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

type auditCMPQ02Preview struct {
	ID           string `json:"previewId"`
	Token        string `json:"confirmationToken"`
	Confirmation string `json:"confirmationText"`
	Matched      int64  `json:"matchedCount"`
}
type auditCMPQ02Executed struct {
	Matched int64  `json:"matchedCount"`
	Deleted int64  `json:"deletedCount"`
	Status  string `json:"status"`
}

func auditCMPQ02MakePreview(t *testing.T, f test013Fixture, filter any) auditCMPQ02Preview {
	t.Helper()
	return decodeTEST022Data[auditCMPQ02Preview](t, f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/preview", filter, 200))
}
func auditCMPQ02Confirm(t *testing.T, f test013Fixture, preview auditCMPQ02Preview, want int) []byte {
	t.Helper()
	return f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/execute", activityCleanupExecuteRequest{PreviewID: preview.ID, ConfirmationToken: preview.Token, Confirmation: preview.Confirmation}, want)
}
func auditCMPQ02UserPublicID(t *testing.T, f test013Fixture) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAuditCMPQ02OverlappingCleanupDoesNotReplaceRemovedMembersHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "log.write")
	var firstID, secondID, lateID, currentID int64
	for _, row := range []struct {
		owner int64
		out   *int64
	}{{f.userIDs[f.editor], &firstID}, {f.userIDs[f.otherEditor], &secondID}} {
		if err := f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,$2,$3,now()-interval '2 days') returning id`, row.owner, activityActionIDs["view"], activityObjectTypeIDs["user"]).Scan(row.out); err != nil {
			t.Fatal(err)
		}
	}
	broad := auditCMPQ02MakePreview(t, f, map[string]any{"actions": []string{"view"}, "objectTypes": []string{"user"}, "to": "2030-01-01T00:00:00Z"})
	narrow := auditCMPQ02MakePreview(t, f, map[string]any{"actions": []string{"view"}, "users": []string{auditCMPQ02UserPublicID(t, f)}})
	if broad.Matched != 2 || narrow.Matched != 1 {
		t.Fatalf("overlapping preview counts=%d/%d", broad.Matched, narrow.Matched)
	}
	other := decodeTEST022Data[auditCMPQ02Executed](t, auditCMPQ02Confirm(t, f, narrow, 200))
	if other.Deleted != 1 {
		t.Fatalf("first cleanup deleted=%d", other.Deleted)
	}
	if err := f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,$2,$3,now()-interval '4 days') returning id`, f.userIDs[f.denied], activityActionIDs["view"], activityObjectTypeIDs["user"]).Scan(&lateID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,$2,$3,clock_timestamp()) returning id`, f.userIDs[f.denied], activityActionIDs["view"], activityObjectTypeIDs["user"]).Scan(&currentID); err != nil {
		t.Fatal(err)
	}
	result := decodeTEST022Data[auditCMPQ02Executed](t, auditCMPQ02Confirm(t, f, broad, 200))
	if result.Matched != 2 || result.Deleted != 1 || result.Status != "completed" {
		t.Fatalf("overlapping cleanup=%+v", result)
	}
	var originalRemaining, newRemaining int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where id=any($1::bigint[])),count(*) filter(where id=any($2::bigint[])) from user_activity_events`, []int64{firstID, secondID}, []int64{lateID, currentID}).Scan(&originalRemaining, &newRemaining); err != nil || originalRemaining != 0 || newRemaining != 2 {
		t.Fatalf("original=%d replacement=%d err=%v", originalRemaining, newRemaining, err)
	}
	var matched, deleted int64
	if err := f.db.QueryRow(f.ctx, `select matched_count,deleted_count from activity_cleanup_runs where public_id=$1`, broad.ID).Scan(&matched, &deleted); err != nil || matched != 2 || deleted != 1 {
		t.Fatalf("audit matched/deleted=%d/%d err=%v", matched, deleted, err)
	}
	auditCMPQ02Confirm(t, f, broad, 409)
}

func TestAuditCMPQ02ExpiredConfirmationDeletesNothingHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "log.write")
	var eventID int64
	if err := f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,$2,$3,now()-interval '2 days') returning id`, f.userIDs[f.editor], activityActionIDs["view"], activityObjectTypeIDs["user"]).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	preview := auditCMPQ02MakePreview(t, f, map[string]any{"actions": []string{"view"}, "users": []string{auditCMPQ02UserPublicID(t, f)}})
	if preview.Matched != 1 {
		t.Fatalf("preview matched=%d", preview.Matched)
	}
	// Set only this owned fixture's expiry, avoiding wall-clock sleeps or a wider
	// time budget. The request still presents the otherwise correct token/phrase.
	if _, err := f.db.Exec(f.ctx, `update activity_cleanup_runs set expires_at=now()-interval '1 second' where public_id=$1`, preview.ID); err != nil {
		t.Fatal(err)
	}
	raw := auditCMPQ02Confirm(t, f, preview, 404)
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Code != "HTTP_404" {
		t.Fatalf("expiry contract=%s err=%v", response.Code, err)
	}
	var present bool
	var status string
	var deleted int64
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from user_activity_events where id=$1),status,deleted_count from activity_cleanup_runs where public_id=$2`, eventID, preview.ID).Scan(&present, &status, &deleted); err != nil || !present || status != "preview" || deleted != 0 {
		t.Fatalf("expired confirmation changed facts: present=%v status=%s deleted=%d err=%v", present, status, deleted, err)
	}
}

func TestAuditCMPQ02CleanupPreservesPermanentStatisticsHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "log.write")
	if _, err := f.db.Exec(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,markdown_added_bytes,markdown_deleted_bytes,occurred_at) values
  ($1,$3,$5,11,3,date_trunc('day',now())-interval '2 days'+interval '4 hours'),
  ($1,$4,$5,17,5,date_trunc('day',now())-interval '2 days'+interval '5 hours'),
  ($2,$3,$5,19,7,date_trunc('day',now())-interval '2 days'+interval '6 hours')`, f.userIDs[f.editor], f.userIDs[f.otherEditor], activityActionIDs["view"], activityActionIDs["edit"], activityObjectTypeIDs["user"]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_experience(user_id,experience,level) values($1,100,0) on conflict(user_id) do update set experience=100,level=0`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_currency_balances(user_id,currency_id,balance) select $1,id,1000 from currencies where code='diamond' on conflict(user_id,currency_id) do update set balance=1000`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var raw string
		if err := f.db.QueryRow(f.ctx, `select jsonb_build_object(
   'totals',(select coalesce(jsonb_agg(to_jsonb(row) order by row.user_id),'[]') from user_statistics_totals row where user_id=any($1::bigint[])),
   'daily',(select coalesce(jsonb_agg(to_jsonb(row) order by row.user_id,row.stat_date),'[]') from user_statistics_daily row where user_id=any($1::bigint[])),
   'experience',(select coalesce(jsonb_agg(to_jsonb(row)),'[]') from user_experience row where user_id=$2),
   'balances',(select coalesce(jsonb_agg(to_jsonb(row) order by row.currency_id),'[]') from user_currency_balances row where user_id=$2),
   'ledger',(select coalesce(jsonb_agg(to_jsonb(row) order by row.id),'[]') from currency_transactions row where user_id=$2)
  )::text`, []int64{f.userIDs[f.editor], f.userIDs[f.otherEditor]}, f.userIDs[f.editor]).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	preview := auditCMPQ02MakePreview(t, f, map[string]any{"actions": []string{"view"}, "users": []string{auditCMPQ02UserPublicID(t, f)}})
	result := decodeTEST022Data[auditCMPQ02Executed](t, auditCMPQ02Confirm(t, f, preview, 200))
	if preview.Matched != 1 || result.Deleted != 1 {
		t.Fatalf("cleanup matched=%d deleted=%d", preview.Matched, result.Deleted)
	}
	if after := snapshot(); after != before {
		t.Fatal("activity retention changed persistent daily/totals statistics, experience, balances or ledger")
	}
	var retained, rawCount, otherCount, retainedAdded, retainedDeleted int64
	if err := f.db.QueryRow(f.ctx, `select
  (select coalesce(sum(event_count),0) from user_statistics_retained_actions where user_id=$1),
  (select count(*) from user_activity_events where user_id=$1),
  (select count(*) from user_activity_events where user_id=$2),
  (select coalesce(sum(markdown_added_bytes),0) from user_statistics_retained_actions where user_id=$1),
  (select coalesce(sum(markdown_deleted_bytes),0) from user_statistics_retained_actions where user_id=$1)`, f.userIDs[f.editor], f.userIDs[f.otherEditor]).Scan(&retained, &rawCount, &otherCount, &retainedAdded, &retainedDeleted); err != nil || retained != 1 || rawCount != 1 || otherCount != 1 || retainedAdded != 11 || retainedDeleted != 3 {
		t.Fatalf("retained=%d raw=%d other=%d bytes=%d/%d err=%v", retained, rawCount, otherCount, retainedAdded, retainedDeleted, err)
	}
}
