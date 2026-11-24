package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReportResolutionUsesCurrentActionGrantAfterAssignmentLockIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "report.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "report.review")
	report := decodeTEST022Data[struct{ ID string }](t, f.require(t, f.editor, http.MethodPost, "/api/v1/reports",
		map[string]string{"targetType": "mod", "targetId": f.modCode, "reasonCode": "copyright_theft"}, http.StatusCreated))
	path := "/api/v1/admin/reports/" + report.ID
	f.require(t, f.reviewer, http.MethodPost, path+"/claim", nil, http.StatusOK)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	var holder int
	if err = tx.QueryRow(ctx, `select pg_backend_pid() from reports where public_id=$1 for update`, report.ID).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	type response struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan response, 1)
	go func() {
		status, body, err := test048HTTPRequest(ctx, f.origin.Client(), f.origin.URL, f.reviewer, http.MethodPost, path+"/resolve",
			`{"conclusion":"valid","deleteTarget":true,"note":"reviewed","idempotencyKey":"governance-grant-review"}`)
		done <- response{status, body, err}
	}()
	for {
		var waiting bool
		if err = f.db.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where datname=current_database()
			and wait_event_type='Lock' and $1=any(pg_blocking_pids(pid)))`, holder).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case reply := <-done:
			t.Fatalf("review finished before reading the assigned report: HTTP %d %s, %v", reply.status, reply.body, reply.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "report.action.delete")
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	reply := <-done
	if reply.err != nil || reply.status != http.StatusOK {
		t.Fatalf("review HTTP %d %s, %v", reply.status, reply.body, reply.err)
	}
	var status, review string
	var actions int
	err = f.db.QueryRow(ctx, `select m.review_status,r.status,(select count(*) from moderation_actions where report_id=r.id)
	  from reports r join mods m on m.project_code=r.target_public_id where r.public_id=$1`, report.ID).Scan(&status, &review, &actions)
	if err != nil || status != "rejected" || review != "resolved_valid" || actions != 1 {
		t.Fatalf("project=%s report=%s actions=%d: %v", status, review, actions, err)
	}
}

func TestBanStorageFailureIsRecoverableAndLeavesNoSanctionIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "ban.create")
	var target string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table ban_records rename column custom_reason to fixture_unavailable_reason`); err != nil {
		t.Fatal(err)
	}
	status, body, err := test048HTTPRequest(f.ctx, f.origin.Client(), f.origin.URL, f.reviewer, http.MethodPost,
		"/api/v1/admin/bans", `{"userId":"`+target+`","reasonCode":"harassment"}`)
	if err != nil || status != http.StatusInternalServerError || strings.Contains(string(body), "custom_reason") {
		t.Errorf("unexpected storage failure response HTTP %d %s, %v", status, body, err)
	}
	var count int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from ban_records)+(select count(*) from user_role_bindings where source='governance_ban')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed sanction persisted %d rows: %v", count, err)
	}
}
