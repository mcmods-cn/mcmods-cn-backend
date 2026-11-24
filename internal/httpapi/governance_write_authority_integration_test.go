package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
	"mcmods-cn-backend/tools/testing/postgrestarget"
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

// Keep the fault-injection tables local to one session while authorization uses the installed schema.
func governanceReviewTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	base, err := postgrestarget.ParseSingleLoopback(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	db := newTEST018IsolatedDatabaseWithConfig(t, ctx, base)
	pc := db.Config()
	pc.MaxConns, pc.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func governanceReviewTestActor(t *testing.T, ctx context.Context, db *pgxpool.Pool, label string, codes ...string) security.Claims {
	t.Helper()
	cfg := config.Load()
	cfg.JWTSecret = "governance-fixture-only-session-signing"
	id, _, token := createTEST044User(t, ctx, db, cfg, label, "UTC")
	grantTEST044Permissions(t, ctx, db, id, codes...)
	claims, err := security.ParseToken(cfg.JWTSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	claims.Subject = id
	access, err := resolveUsersRootPermissionsQuery(ctx, db, []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	claims.PermissionRules = access[id].Permissions
	return claims
}

func TestBanCreationWaitsForAnOngoingReleaseBeforeLockingAccountsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	actorID, subjectID := f.userIDs[f.reviewer], f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, actorID, "ban.create")
	var subject string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, subjectID).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	body := map[string]string{"userId": subject, "reasonCode": "harassment"}
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/bans", body, http.StatusCreated)
	release, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release.Rollback(f.ctx)
	var oldKey string
	if err = release.QueryRow(f.ctx, `update ban_records set status='revoked',revoked_at=now(),revoked_by=$2,revoke_reason='reviewed'
		where user_id=$1 and status='active' returning id::text`, subjectID, actorID).Scan(&oldKey); err != nil {
		t.Fatal(err)
	}
	next := auditCompletionAsyncJSON(f, f.reviewer, http.MethodPost, "/api/v1/admin/bans", body)
	auditCompletionAwaitLocks(t, f, 1)
	if _, err = release.Exec(f.ctx, `select id from users where id=$1 for no key update nowait`, subjectID); err != nil {
		t.Fatalf("creation acquired the account needed to finish the ongoing release: %v", err)
	}
	if _, err = release.Exec(f.ctx, `delete from user_role_bindings where user_id=$1 and source='governance_ban' and source_key=$2`, subjectID, oldKey); err != nil {
		t.Fatal(err)
	}
	if err = release.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	got := <-next
	if got.err != nil || got.status != http.StatusCreated {
		t.Fatalf("second ban returned HTTP %d %s, %v", got.status, got.raw, got.err)
	}
	var active, revoked, bindings int
	err = f.db.QueryRow(f.ctx, `select count(*) filter(where ban.status='active'),count(*) filter(where ban.status='revoked'),
		(select count(*) from user_role_bindings grant_row join ban_records current_ban on current_ban.id::text=grant_row.source_key
		where grant_row.source='governance_ban' and grant_row.user_id=$1 and current_ban.status='active')
		from ban_records ban where ban.user_id=$1`, subjectID).Scan(&active, &revoked, &bindings)
	if err != nil || active != 1 || revoked != 1 || bindings != 1 {
		t.Fatalf("active=%d revoked=%d active bindings=%d: %v", active, revoked, bindings, err)
	}
}

func TestBanReasonsAvailableForEachSanctionWorkflowIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "report.review", "report.action.ban")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "ban.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "report.review")
	const path = "/api/v1/admin/ban-reasons?locale=en-US"
	for label, token := range map[string]string{"report_reviewer": f.reviewer, "direct_moderator": f.editor} {
		t.Run(label, func(t *testing.T) {
			dictionary := decodeTEST022Data[struct {
				Items []struct{ Code, Label string }
			}](t, f.require(t, token, http.MethodGet, path, nil, http.StatusOK))
			for _, reason := range dictionary.Items {
				if reason.Code == "harassment" && reason.Label != "" {
					return
				}
			}
			t.Fatal("active harassment reason missing from the sanction dictionary")
		})
	}
	f.require(t, f.otherEditor, http.MethodGet, path, nil, http.StatusForbidden)
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/bans", nil, http.StatusForbidden)
}
