package httpapi

import (
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"net/url"
	"testing"
)

func mustAuditActivityURL(path string) *url.URL { return &url.URL{Path: path} }
func auditFixUserActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"user_follows", "user_blocks", "yggdrasil_accounts", "yggdrasil_tokens", "yggdrasil_join_sessions"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func auditFixUserActivityFailureRecovery(t *testing.T, f test013Fixture, actor, method, path string, body any) {
	t.Helper()
	auditFixInjectActivityFailure(t, f, f.userIDs[actor], true)
	before := auditFixUserActivityFacts(t, f)
	raw := f.require(t, actor, method, path, body, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist user activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixUserActivityFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[actor], false)
	action := inferredActivityAction(&http.Request{Method: method, URL: mustAuditActivityURL(path)})
	prior := auditFixActivityIntentCount(t, f, actor, action, activity.ObjectUser)
	f.require(t, actor, method, path, body, 200)
	if count := auditFixActivityIntentCount(t, f, actor, action, activity.ObjectUser); count != prior+1 {
		t.Fatalf("successful user activity=%d prior=%d", count, prior)
	}
}
func TestAuditFixCMP003LauncherCredentialsAndSessionActivityShareRevocationTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], yggdrasilLoginPermission)
	credential := "/api/v1/users/me/launcher-credential"
	// The historical no-account success records the same durable DELETE event.
	auditFixUserActivityFailureRecovery(t, f, f.editor, http.MethodDelete, credential, nil)
	auditFixUserActivityFailureRecovery(t, f, f.editor, http.MethodPut, credential, map[string]string{"password": "audit-synthetic-launcher-only-password"})
	var session string
	if err := f.db.QueryRow(f.ctx, `insert into yggdrasil_tokens(access_token_hash,user_id,client_token,status,expires_at) values($1,$2,'audit-synthetic-client','active',now()+interval '1 hour') returning public_id`, hashYggdrasilToken("audit-synthetic-session-token"), f.userIDs[f.editor]).Scan(&session); err != nil {
		t.Fatal(err)
	}
	auditFixUserActivityFailureRecovery(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/launcher-sessions/"+session, nil)
	var tokenState string
	if err := f.db.QueryRow(f.ctx, `select status from yggdrasil_tokens where public_id=$1`, session).Scan(&tokenState); err != nil || tokenState != "revoked" {
		t.Fatalf("actual session state=%s err=%v", tokenState, err)
	}
	auditFixUserActivityFailureRecovery(t, f, f.editor, http.MethodDelete, credential, nil)
	var enabled bool
	if err := f.db.QueryRow(f.ctx, `select enabled from yggdrasil_accounts where user_id=$1`, f.userIDs[f.editor]).Scan(&enabled); err != nil || enabled {
		t.Fatalf("actual account enabled=%v err=%v", enabled, err)
	}
}
func TestAuditFixCMP003FollowAndBlockActivityShareRelationshipTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "user.follow.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "user.follow.receive")
	var target string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.otherEditor]).Scan(&target); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/users/" + target
	auditFixUserActivityFailureRecovery(t, f, f.editor, http.MethodPost, path+"/follow", nil)
	if _, err := f.db.Exec(f.ctx, `insert into user_follows(follower_id,followed_id) values($1,$2)`, f.userIDs[f.otherEditor], f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	auditFixUserActivityFailureRecovery(t, f, f.editor, http.MethodPut, path+"/block", nil)
	var follows int
	if err := f.db.QueryRow(f.ctx, `select count(*) from user_follows`).Scan(&follows); err != nil || follows != 0 {
		t.Fatalf("block follows=%d err=%v", follows, err)
	}
	auditFixUserActivityFailureRecovery(t, f, f.editor, http.MethodDelete, path+"/block", nil)
	var blocks int
	if err := f.db.QueryRow(f.ctx, `select count(*) from user_blocks`).Scan(&blocks); err != nil || blocks != 0 {
		t.Fatalf("unblock rows=%d err=%v", blocks, err)
	}
}
func TestAuditFixCMP003UserFileDeletionActivitySharesTombstoneAndQuotaTransaction(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("Synthetic exact bytes for user deletion transaction")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "atomic-delete.zip", "application/octet-stream", "test037-atomic", raw)
	f.put(t, ticket, raw)
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
	body := map[string]string{"objectKey": ticket.ObjectKey}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := f.facts(t)
	response := f.require(t, f.editor, http.MethodDelete, "/api/v1/users/me/files", body, 500)
	auditFixRequireSpecificActivityError(t, response, "failed to persist user activity")
	f.unchanged(t, before)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	f.require(t, f.editor, http.MethodDelete, "/api/v1/users/me/files", body, 200)
	var status string
	var intents int
	if err := f.db.QueryRow(f.ctx, `select status,(select count(*) from oss_object_deletion_outbox where object_key=$1) from oss_files where object_key=$1`, ticket.ObjectKey).Scan(&status, &intents); err != nil || status != "deleted" || intents != 1 {
		t.Fatalf("actual file status=%s deleteIntents=%d err=%v", status, intents, err)
	}
	if count := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionDelete, activity.ObjectFile); count != 1 {
		t.Fatalf("file deletion activity=%d want1", count)
	}
}
