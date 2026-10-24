package httpapi

import (
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func auditFixProfilePreferencesFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditFixUserActivityFacts(t, f)
	for _, table := range []string{"users", "user_notification_settings"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func TestAuditFixCMP003UnfollowAndNotificationPreferencesShareActivityTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "user.follow.create")
	var target string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.otherEditor]).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_follows(follower_id,followed_id) values($1,$2)`, f.userIDs[f.editor], f.userIDs[f.otherEditor]); err != nil {
		t.Fatal(err)
	}
	recover := func(method, path string, body any, action int16) {
		t.Helper()
		prior := auditFixActivityIntentCount(t, f, f.editor, action, activity.ObjectUser)
		auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
		before := auditFixProfilePreferencesFacts(t, f)
		raw := f.require(t, f.editor, method, path, body, 500)
		auditFixRequireSpecificActivityError(t, raw, "failed to persist user preference activity")
		auditFixRequireProjectFactsUnchanged(t, before, auditFixProfilePreferencesFacts(t, f))
		auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
		f.require(t, f.editor, method, path, body, 200)
		if count := auditFixActivityIntentCount(t, f, f.editor, action, activity.ObjectUser); count != prior+1 {
			t.Fatalf("preference activity=%d prior=%d", count, prior)
		}
	}
	for range 2 {
		recover(http.MethodDelete, "/api/v1/users/"+target+"/follow", nil, activity.ActionDelete)
	}
	var follows int
	if err := f.db.QueryRow(f.ctx, `select count(*) from user_follows where follower_id=$1`, f.userIDs[f.editor]).Scan(&follows); err != nil || follows != 0 {
		t.Fatalf("actual unfollow rows=%d err=%v", follows, err)
	}
	path := "/api/v1/users/me/notification-settings"
	recover(http.MethodPut, path, map[string]bool{"emailEnabled": true, "projectUpdatesEnabled": false}, activity.ActionEdit)
	recover(http.MethodPut, path, map[string]bool{"emailEnabled": false}, activity.ActionEdit)
	self := decodeTEST022Data[map[string]bool](t, f.require(t, f.editor, http.MethodGet, path, nil, 200))
	other := decodeTEST022Data[map[string]bool](t, f.require(t, f.otherEditor, http.MethodGet, path, nil, 200))
	if self["emailEnabled"] || self["projectUpdatesEnabled"] || other["emailEnabled"] || !other["projectUpdatesEnabled"] {
		t.Fatalf("actual preferences self=%v other=%v", self, other)
	}
}
