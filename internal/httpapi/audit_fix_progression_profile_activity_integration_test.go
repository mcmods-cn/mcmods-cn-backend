package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func auditFixProgressionProfileFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditFixGovernanceActivityFacts(t, f)
	for _, table := range []string{"system_settings", "level_system_config", "level_recalculation_jobs", "task_definitions", "user_task_progress", "user_timezone_changes", "permission_role_tracks", "permission_role_track_roles"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}

func TestAuditFixCMP003ProgressionAndProfileActivitySharesBusinessTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "task.write", "task.read", "level.write", "permission.write", "admin.config.write")
	recover := func(actor, method, path string, body any, want int, action, object int16, message string) []byte {
		t.Helper()
		prior := auditFixActivityIntentCount(t, f, actor, action, object)
		auditFixInjectActivityFailure(t, f, f.userIDs[actor], true)
		before := auditFixProgressionProfileFacts(t, f)
		raw := f.require(t, actor, method, path, body, 500)
		auditFixRequireSpecificActivityError(t, raw, message)
		auditFixRequireProjectFactsUnchanged(t, before, auditFixProgressionProfileFacts(t, f))
		auditFixInjectActivityFailure(t, f, f.userIDs[actor], false)
		result := f.require(t, actor, method, path, body, want)
		if got := auditFixActivityIntentCount(t, f, actor, action, object); got != prior+1 {
			t.Fatalf("%s %s activity=%d prior=%d", method, path, got, prior)
		}
		return result
	}
	task := taskPayload{Code: "audit_atomic_task", Name: "Atomic task", Translations: map[string]any{}, RefreshPeriod: "never", Condition: map[string]any{"action": "create", "objectType": "mod", "metric": "count", "target": 1}, Rewards: map[string]any{"experience": 1}, Status: "active"}
	created := decodeTEST022Data[taskPayload](t, recover(f.reviewer, http.MethodPost, "/api/v1/admin/tasks", task, 201, activity.ActionCreate, activity.ObjectTask, "failed to persist progression activity"))
	task.Name = "Updated atomic task"
	recover(f.reviewer, http.MethodPut, "/api/v1/admin/tasks/"+created.PublicID, task, 200, activity.ActionEdit, activity.ObjectTask, "failed to persist progression activity")
	recover(f.reviewer, http.MethodDelete, "/api/v1/admin/tasks/"+created.PublicID, nil, 200, activity.ActionDelete, activity.ObjectTask, "failed to persist progression activity")
	recover(f.reviewer, http.MethodPut, "/api/v1/admin/levels/config", levelConfigPayload{LevelThresholds: []int64{}}, 200, activity.ActionEdit, activity.ObjectTask, "failed to persist progression activity")
	var jobs int
	if err := f.db.QueryRow(f.ctx, `select count(*) from level_recalculation_jobs where config_version=(select version from level_system_config where singleton) and status='queued'`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("current recalculation jobs=%d err=%v", jobs, err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into roles(code,name) values('audit_track_low','Low'),('audit_track_high','High');
 insert into permission_role_tracks(code,name) values('audit_atomic_track','Atomic track');
 insert into permission_role_track_roles(track_code,role_id,position) select 'audit_atomic_track',id,case code when 'audit_track_low' then 0 else 1 end from roles where code in ('audit_track_low','audit_track_high')`); err != nil {
		t.Fatal(err)
	}
	var target string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&target); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/users/" + target + "/role-tracks/audit_atomic_track/"
	// With no manual member in this track there is no action to roll back.
	// An unavailable activity sink must neither turn this into a failure nor
	// manufacture a progression event. Actual transitions are tested below.
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], true)
	before := auditFixProgressionProfileFacts(t, f)
	prior := auditFixActivityIntentCount(t, f, f.reviewer, activity.ActionCreate, activity.ObjectUser)
	unchanged := decodeTEST022Data[struct {
		Changed *bool    `json:"changed"`
		Roles   []string `json:"roles"`
	}](t, f.require(t, f.reviewer, http.MethodPost, path+"upgrade", nil, http.StatusOK))
	if unchanged.Changed == nil || *unchanged.Changed || unchanged.Roles == nil || len(unchanged.Roles) != 0 {
		t.Fatalf("unused role track returned %#v", unchanged)
	}
	if got := auditFixActivityIntentCount(t, f, f.reviewer, activity.ActionCreate, activity.ObjectUser); got != prior {
		t.Fatalf("unused role track wrote activity count=%d prior=%d", got, prior)
	}
	auditFixRequireProjectFactsUnchanged(t, before, auditFixProgressionProfileFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], false)
	if _, err := f.db.Exec(f.ctx, `insert into user_role_bindings(user_id,role_id,source,source_key) select $1,id,'manual','' from roles where code='audit_track_low'`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"upgrade", "downgrade"} {
		recover(f.reviewer, http.MethodPost, path+direction, nil, 200, activity.ActionCreate, activity.ObjectUser, "failed to persist role track activity")
		var role string
		if err := f.db.QueryRow(f.ctx, `select role.code from user_role_bindings binding join roles role on role.id=binding.role_id where binding.user_id=$1 and binding.source='manual' and role.code like 'audit_track_%'`, f.userIDs[f.editor]).Scan(&role); err != nil {
			t.Fatal(err)
		}
		want := "audit_track_high"
		if direction == "downgrade" {
			want = "audit_track_low"
		}
		if role != want {
			t.Fatalf("%s role=%s want=%s", direction, role, want)
		}
	}
	recover(f.reviewer, http.MethodPut, "/api/v1/admin/config/profile", profileConfigPayload{SignatureMaxBytes: 512}, 200, activity.ActionEdit, activity.ObjectUser, "failed to persist profile activity")
	body := map[string]any{"signature": "Synthetic atomic signature", "timezone": "Europe/London", "messageReceive": false, "showOnlineStatus": false, "publicCardStatSlots": []string{"comment_count", "", "edit_count", "", "map_count", ""}}
	raw := recover(f.editor, http.MethodPut, "/api/v1/users/me/profile-settings", body, 200, activity.ActionCreate, activity.ObjectUser, "failed to persist profile activity")
	if !json.Valid(raw) {
		t.Fatal("profile result is not JSON")
	}
	var signature, timezone string
	if err := f.db.QueryRow(f.ctx, `select signature,timezone from users where id=$1`, f.userIDs[f.editor]).Scan(&signature, &timezone); err != nil || signature != body["signature"] || timezone != "Europe/London" {
		t.Fatalf("profile signature=%q timezone=%q err=%v", signature, timezone, err)
	}
	body["signature"] = "Second synthetic atomic signature"
	recover(f.editor, http.MethodPut, "/api/v1/users/me/profile-settings", body, 200, activity.ActionEdit, activity.ObjectUser, "failed to persist profile activity")
	recover(f.editor, http.MethodPut, "/api/v1/users/me/profile-settings", map[string]any{}, 200, activity.ActionEdit, activity.ObjectUser, "failed to persist profile activity")
}
