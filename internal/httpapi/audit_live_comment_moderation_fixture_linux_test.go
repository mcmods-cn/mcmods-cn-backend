package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/antiabuse"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

type auditCMP004Scenario struct {
	f                              auditFixGRFixture
	authorID, currencyID, taskID   int64
	authorToken, authorPublic      string
	postID, pendingID, commentPath string
	body, idempotency              string
}

func auditCMP004CommentFacts(t *testing.T, a auditCMP004Scenario, id string) string {
	t.Helper()
	var facts string
	if err := a.f.db.QueryRow(a.f.ctx, `select jsonb_build_object(
 'comment',(select to_jsonb(comment) from comments comment where public_id=$1),
 'reviews',(select coalesce(jsonb_agg(to_jsonb(event) order by event.id),'[]'::jsonb) from audit_events event where aggregate_type='comment_moderation' and aggregate_key=$1),
 'activity',(select coalesce(jsonb_agg(to_jsonb(event) order by event.id),'[]'::jsonb) from activity_event_outbox event where object_public_id=$1),
 'delivery',(select coalesce(jsonb_agg(to_jsonb(event) order by event.id),'[]'::jsonb) from nats_outbox event where payload->'data'->>'commentId'=$1))::text`, id).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

func auditCMP004HiddenReviewAfterQuota(t *testing.T, a auditCMP004Scenario, path string) []byte {
	t.Helper()
	f := a.f
	request := func() (int, http.Header, []byte) {
		payload, err := json.Marshal(map[string]string{"status": "approved"})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPatch, f.origin.URL+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+f.reviewer)
		req.Header.Set("Content-Type", "application/json")
		res, err := f.origin.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header.Clone(), raw
	}
	status, headers, raw := request()
	if status == http.StatusTooManyRequests {
		var problem struct {
			Code string `json:"code"`
		}
		seconds, err := strconv.Atoi(headers.Get("Retry-After"))
		if err != nil || seconds <= 0 || seconds > 120 || json.Unmarshal(raw, &problem) != nil || problem.Code != "rate_limited" {
			t.Fatal("default moderation quota did not provide its bounded recovery contract")
		}
		t.Logf("CMP004 hidden-target counterexample reached legitimate reviewquota429; one explicit same-actor retry after actualRetryAfter=%ds, without changing actor/policy", seconds)
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-f.ctx.Done():
			t.Fatal("fixture expired during real moderation-quota recovery")
		}
		status, _, raw = request()
	}
	if status != http.StatusNotFound || !bytes.Contains(raw, []byte("comment target unavailable")) {
		t.Fatalf("hidden target expected exact404 after available quota, gotHTTP%d", status)
	}
	return raw
}

func TestAuditCMP004DefaultPolicyRejectRestoreRevocationAndHiddenTargetHTTPPostgresIntegration(t *testing.T) {
	a := auditCMP004DefaultPolicyScenario(t, 3*time.Minute)
	f := a.f
	reviewPath := "/api/v1/comments/" + a.pendingID + "/review"
	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "rejected"}, 200)
	hidden := f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending?status=hidden", nil, 200)
	if !bytes.Contains(hidden, []byte(a.pendingID)) || !bytes.Contains(hidden, []byte(a.body)) {
		t.Fatal("actual rejected default-policy comment is not in the recovery queue")
	}
	before := auditCMP004CommentFacts(t, a, a.pendingID)
	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "approved"}, 409)
	if after := auditCMP004CommentFacts(t, a, a.pendingID); after != before {
		t.Fatal("rejected comment direct approval changed persisted moderation facts")
	}
	if f.number(t, `select count(*) from activity_event_outbox where object_public_id=$1`, a.pendingID) != 0 || f.number(t, `select count(*) from user_task_progress where user_id=$1 and task_id=$2`, a.authorID, a.taskID) != 0 {
		t.Fatal("reject already rewarded a nonpublic comment")
	}
	// Revoke and restore the real permission through its configured governance
	// API. The moderator keeps the same session, so stale cached authority cannot
	// satisfy this negative case. No administrator replaces the comment author.
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "permission.write")
	var reviewerPublic string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.reviewer]).Scan(&reviewerPublic); err != nil {
		t.Fatal(err)
	}
	permissionsPath := "/api/v1/admin/users/" + reviewerPublic + "/permissions"
	f.require(t, f.otherEditor, http.MethodPut, permissionsPath, updateUserPermissionsRequest{Permissions: []userPermissionEntry{{Code: "content.review", Allow: true}, {Code: "comment.moderate", Allow: false}}}, 200)
	before = auditCMP004CommentFacts(t, a, a.pendingID)
	f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending?status=hidden", nil, 403)
	denied := f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "restore"}, 403)
	if !bytes.Contains(denied, []byte("comment moderator required")) || auditCMP004CommentFacts(t, a, a.pendingID) != before {
		t.Fatal("revoked moderator restored a comment or was rejected for an unrelated reason")
	}
	f.require(t, f.otherEditor, http.MethodPut, permissionsPath, updateUserPermissionsRequest{Permissions: []userPermissionEntry{{Code: "content.review", Allow: true}, {Code: "comment.moderate", Allow: true}}}, 200)
	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "restore"}, 200)
	before = auditCMP004CommentFacts(t, a, a.pendingID)
	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "restore"}, 409)
	if auditCMP004CommentFacts(t, a, a.pendingID) != before {
		t.Fatal("duplicate restore changed moderation facts")
	}
	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "approved"}, 200)
	if f.number(t, `select count(*) from comments where public_id=$1 and status='published'`, a.pendingID) != 1 || f.number(t, `select count(*) from activity_event_outbox where object_public_id=$1 and user_id=$2`, a.pendingID, a.authorID) != 1 || f.number(t, `select count(*) from audit_events where aggregate_type='comment_moderation' and aggregate_key=$1`, a.pendingID) != 3 {
		t.Fatal("reject/restore/approve did not leave exactly three decisions and one author intent")
	}
	// A separate ordinary HTTP comment targets the existing owned public Mod.
	// Its later unavailable state is a controlled fixture transition, unrelated
	// to the still-held question bounty and its undecided cancellation policy.
	modPath := "/api/v1/comment-targets/mod/" + f.modCode + "/comments"
	second := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, a.authorToken, http.MethodPost, modPath, map[string]string{"body": "Hidden target counterexample " + randomCatalogPublicID(), "idempotencyKey": "cmp004-hidden-" + randomCatalogPublicID()}, 202))
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='rejected' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/mods/"+f.modCode, nil, 404)
	before = auditCMP004CommentFacts(t, a, second.ID)
	denied = auditCMP004HiddenReviewAfterQuota(t, a, "/api/v1/comments/"+second.ID+"/review")
	if !bytes.Contains(denied, []byte("comment target unavailable")) || auditCMP004CommentFacts(t, a, second.ID) != before {
		t.Fatal("hidden parent review was not exact404 with zero comment/activity/audit/delivery writes")
	}
	queued := f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending", nil, 200)
	if bytes.Contains(queued, []byte(second.ID)) {
		t.Fatal("current hidden target leaked a moderation queue card")
	}
	t.Log("CMP004 unchanged default-policy pending→real reject/recovery queue→real RBAC revoke403/zeroeffects→restore→duplicate409/zeroeffects→approve once; separate current hiddenMod404/zeroeffects; no held-bounty policy decision")
}

func TestAuditCMP004DefaultPolicyTwoIndependentModeratorsOneApprovalHTTPPostgresIntegration(t *testing.T) {
	a := auditCMP004DefaultPolicyScenario(t, 3*time.Minute)
	f := a.f
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "comment.moderate")
	cfg := f.db.Config()
	cfg.MaxConns, cfg.MinConns = 4, 1
	const application = "mcmods-cmp004-independent-review-race"
	cfg.ConnConfig.RuntimeParams["application_name"] = application
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	api := NewServer(f.ctx, f.server.cfg, pool, nil, nil, nil, nil)
	origin := httptest.NewServer(api)
	t.Cleanup(func() {
		origin.Close()
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := api.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		api.cache.Close()
	})
	actorFixture := f.test013Fixture
	actorFixture.origin = origin
	for _, token := range []string{f.reviewer, f.denied} {
		actorFixture.require(t, token, http.MethodGet, "/api/v1/auth/me", nil, 200)
	}
	// The holder and observer use the original independent pool. No request or
	// observer tries to borrow the holder's single occupied connection.
	holder, err := pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	var holderPID int
	if err = holder.QueryRow(f.ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err = holder.Exec(f.ctx, `select id from comments where public_id=$1 for update`, a.pendingID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/comments/" + a.pendingID + "/review"
	first := auditCompletionAsyncJSON(actorFixture, f.reviewer, http.MethodPatch, path, map[string]string{"status": "approved"})
	second := auditCompletionAsyncJSON(actorFixture, f.denied, http.MethodPatch, path, map[string]string{"status": "approved"})
	wait, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var n int
		if err = f.db.QueryRow(wait, `with recursive blocked(pid) as (select $2::int union select actor.pid from pg_stat_activity actor join blocked parent on parent.pid=any(pg_blocking_pids(actor.pid)) where actor.datname=current_database()) select count(*) from pg_stat_activity actor join blocked using(pid) where actor.application_name=$1 and actor.pid<>$2 and actor.wait_event_type='Lock' and actor.query like 'select comment.id,comment.author_id,%'`, application, holderPID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 2 {
			break
		}
		select {
		case <-ticker.C:
		case <-wait.Done():
			t.Fatal("two actual independent reviewer requests never reached the exact held comment row")
		}
	}
	if err = holder.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	one, two := <-first, <-second
	if one.err != nil || two.err != nil || !((one.status == 200 && two.status == 409) || (one.status == 409 && two.status == 200)) {
		t.Fatalf("two real reviewer outcomes=%d/%d transport=%v/%v", one.status, two.status, one.err, two.err)
	}
	if f.number(t, `select count(*) from comments where public_id=$1 and status='published'`, a.pendingID) != 1 || f.number(t, `select count(*) from audit_events where aggregate_type='comment_moderation' and aggregate_key=$1 and action='comment.review.approved'`, a.pendingID) != 1 || f.number(t, `select count(*) from activity_event_outbox where object_public_id=$1 and user_id=$2`, a.pendingID, a.authorID) != 1 {
		t.Fatal("moderator race left duplicate publication/audit/author activity")
	}
	_ = auditFixActivityMonitor(t, f.test013Fixture)
	auditFixWaitActivity(t, func() bool {
		return f.number(t, `select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null and progress=1`, a.authorID, a.taskID) == 1
	})
	if f.number(t, `select count(*) from experience_transactions where user_id=$1 and reason='task_reward' and reference_key like $2 and amount_delta=25`, a.authorID, strconv.FormatInt(a.taskID, 10)+":%") != 1 {
		t.Fatal("actual race consumer did not award exactly once")
	}
	t.Log("CMP004 actual defaultpolicy pending; two distinct moderators observed blocked on exact same comment row, outcomes200+409; one publication/audit/authorintent and actual rewardconsumer once")
}

func auditCMP004ResolvedAnswerConflict(t *testing.T, a auditCMP004Scenario, path string) {
	t.Helper()
	f := a.f
	request := func() (int, http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+f.editor)
		req.Header.Set("Content-Type", "application/json")
		response, err := f.origin.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, response.Header.Clone(), raw
	}
	status, headers, raw := request()
	if status == http.StatusTooManyRequests {
		var problem struct {
			Code string `json:"code"`
		}
		seconds, err := strconv.Atoi(headers.Get("Retry-After"))
		if err != nil || seconds <= 0 || seconds > 120 || json.Unmarshal(raw, &problem) != nil || problem.Code != "rate_limited" {
			t.Fatal("quota rejection did not carry the actual bounded retry contract")
		}
		if n := f.number(t, `select count(*) from currency_transactions where reference_key=$1 and transaction_type='question_bounty_award'`, a.postID); n != 1 {
			t.Fatal("quota-rejected duplicate changed the existing award ledger")
		}
		t.Logf("CMP004 legitimate defaultquota429/zeroadditionalaward; one explicit duplicate attempt after actualRetryAfter=%ds; no quota override or automatic product write replay", seconds)
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-f.ctx.Done():
			t.Fatal("fixture did not have the required real quota recovery window")
		}
		status, _, raw = request()
	}
	if status != http.StatusConflict || !bytes.Contains(raw, []byte("question has already been resolved")) {
		t.Fatalf("settled duplicate did not reach exact business409: HTTP%d", status)
	}
}

// Only identities, a currency and a real configured reward task are fixture
// data. Every question/revision/comment below is produced through ordinary
// HTTP. No SQL pending row, injected moderation context or policy change.
func auditCMP004DefaultPolicyScenario(t *testing.T, duration time.Duration) auditCMP004Scenario {
	t.Helper()
	f := newAuditFixGRFixture(t, duration)
	settings, err := f.server.antiAbuse.Settings(f.ctx)
	if err != nil || !f.server.cfg.AntiAbuse.Enabled || !reflect.DeepEqual(settings, antiabuse.DefaultSettings()) {
		t.Fatalf("current default anti-abuse policy was changed or disabled: %v", err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "community.discussion.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "comment.moderate")
	a := auditCMP004Scenario{f: f}
	// This helper creates an ordinary active account with the schema's real
	// new-account and unverified-email defaults; no elevated role is installed.
	a.authorID, a.authorPublic, a.authorToken = createTEST044User(t, f.ctx, f.db, f.server.cfg, "cmp004-"+randomCatalogPublicID(), "UTC")
	f.userIDs[a.authorToken] = a.authorID
	grantTEST044Permissions(t, f.ctx, f.db, a.authorID, "comment.create")
	if err = f.db.QueryRow(f.ctx, `insert into currencies(code,name,transfer_tax_bps) values('cmp004','Owned moderation test currency',1000) returning id`).Scan(&a.currencyID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = changeCurrencyBalanceByIDTx(f.ctx, tx, f.userIDs[f.editor], a.currencyID, 1000, "cmp004_fixture_seed", nil, "fixture", "owned", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `insert into task_definitions(code,name,condition,rewards,status)
 values('cmp004_first_published_comment','Owned first published comment','{"action":"create","objectType":"comment","metric":"count","target":1}'::jsonb,'{"experience":25,"currencies":{"cmp004":3}}'::jsonb,'active') returning id`).Scan(&a.taskID); err != nil {
		t.Fatal(err)
	}
	snapshot := communityPostSnapshot{Kind: "discussion", Title: "Owned moderation bounty " + f.modCode, SourceLocale: "en-US", BodyMarkdown: "Synthetic public question for comment recovery.", MinecraftVersions: []string{}, Projects: []communityPostReference{}, Resources: []communityPostReference{}, BountyCurrency: "cmp004", BountyAmount: 100}
	post := decodeTEST022Data[test042Mutation](t, f.require(t, f.editor, http.MethodPost, "/api/v1/community/posts", communityPostMutationRequest{communityPostSnapshot: snapshot}, 201))
	if post.ReviewStatus == "pending" {
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+post.RevisionID, map[string]string{"status": "approved", "note": "Independent owned question prerequisite"}, 200)
	}
	a.postID = post.ID
	f.require(t, "", http.MethodGet, "/api/v1/community/posts/"+a.postID, nil, 200)
	a.commentPath = "/api/v1/comment-targets/community_post/" + a.postID + "/comments"
	a.body = "Copper solution " + randomCatalogPublicID() + randomCatalogPublicID()
	a.idempotency = "cmp004-natural-" + randomCatalogPublicID()
	created := decodeTEST022Data[struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		Moderation bool   `json:"moderation"`
	}](t, f.require(t, a.authorToken, http.MethodPost, a.commentPath, map[string]string{"body": a.body, "idempotencyKey": a.idempotency}, 202))
	if created.ID == "" || created.Status != "pending" || !created.Moderation {
		t.Fatal("ordinary default-policy HTTP request did not create a pending comment")
	}
	a.pendingID = created.ID
	var rules []string
	var risk int
	auditFixWaitActivity(t, func() bool {
		return f.db.QueryRow(f.ctx, `select risk_score,rule_codes from anti_abuse_events where user_id=$1 and action='comment.create' and outcome='moderation' order by id desc limit 1`, a.authorID).Scan(&risk, &rules) == nil
	})
	if risk < settings.ModerationThreshold || risk >= settings.ChallengeThreshold {
		t.Fatalf("natural moderation score=%d outside unchanged default moderation band", risk)
	}
	for _, want := range []string{"new_account", "email_unverified", "suspicious_client", "missing_form_token"} {
		found := false
		for _, got := range rules {
			found = found || got == want
		}
		if !found {
			t.Fatalf("default HTTP client moderation missing actual rule %s: %v", want, rules)
		}
	}
	t.Logf("CMP004_DEFAULT actor=%s policy=unchanged score=%d rules=%v client=ordinaryGoHTTP noSQLpending/noinjecteddecision", a.authorPublic, risk, rules)
	return a
}

func TestAuditCMP004DefaultPolicyPendingCanBecomeBountyAnswerHTTPPostgresIntegration(t *testing.T) {
	a := auditCMP004DefaultPolicyScenario(t, 3*time.Minute)
	f := a.f
	visible := func(want int) {
		t.Helper()
		raw := f.require(t, "", http.MethodGet, a.commentPath, nil, 200)
		var response struct {
			Data struct {
				Items []commentResponse `json:"items"`
				Total int               `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil || response.Data.Total != want || len(response.Data.Items) != want {
			t.Fatalf("anonymous comment count/items=%d/%d want%d err=%v", response.Data.Total, len(response.Data.Items), want, err)
		}
		if (want > 0) != bytes.Contains(raw, []byte(a.body)) {
			t.Fatal("pending/published body visibility disagrees with public count")
		}
	}
	visible(0)
	if n := f.number(t, `select count(*) from activity_event_outbox where user_id=$1`, a.authorID); n != 0 {
		t.Fatal("natural pending comment already emitted author activity")
	}
	answerPath := "/api/v1/community/posts/" + a.postID + "/answers/" + a.pendingID
	denied := f.require(t, f.editor, http.MethodPost, answerPath, nil, 404)
	if !bytes.Contains(denied, []byte("answer was not found")) {
		t.Fatal("unrelated failure satisfied pending answer denial")
	}
	reviewPath := "/api/v1/comments/" + a.pendingID + "/review"
	f.require(t, a.authorToken, http.MethodPatch, reviewPath, map[string]string{"status": "approved"}, 403)
	queued := f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/comments/pending", nil, 200)
	if !bytes.Contains(queued, []byte(a.pendingID)) || !bytes.Contains(queued, []byte(a.body)) {
		t.Fatal("independent moderator cannot find actual default-policy comment")
	}
	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "approved"}, 200)
	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "approved"}, 409)
	visible(1)
	monitor := auditFixActivityMonitor(t, f.test013Fixture)
	auditFixWaitActivity(t, func() bool {
		return f.number(t, `select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null and progress=1`, a.authorID, a.taskID) == 1
	})
	closed, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := monitor.Close(closed); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": "approved"}, 409)
	_ = auditFixActivityMonitor(t, f.test013Fixture)
	f.require(t, f.editor, http.MethodPost, answerPath, nil, 200)
	auditCMP004ResolvedAnswerConflict(t, a, answerPath)
	publicQuestion := decodeTEST022Data[communityPostResponse](t, f.require(t, "", http.MethodGet, "/api/v1/community/posts/"+a.postID, nil, 200))
	if publicQuestion.ResolutionStatus != "answered" || publicQuestion.AcceptedCommentID != a.pendingID || publicQuestion.Bounty == nil || publicQuestion.Bounty.Status != "awarded" || publicQuestion.Bounty.NetAmount != 90 || publicQuestion.Bounty.TaxAmount != 10 {
		t.Fatal("public question did not retain the exact recovered answer and existing bounty settlement contract")
	}
	var balance, awards, rewardXP, rewardCoin int64
	if err := f.db.QueryRow(f.ctx, `select
 (select balance from user_currency_balances where user_id=$1 and currency_id=$2),
 (select count(*) from currency_transactions where user_id=$1 and transaction_type='question_bounty_award' and reference_key=$3),
 (select count(*) from experience_transactions where user_id=$1 and reason='task_reward' and reference_key like $4 and amount_delta=25),
 (select count(*) from currency_transactions where user_id=$1 and transaction_type='task_reward' and reference_key like $4 and amount_delta=3)`, a.authorID, a.currencyID, a.postID, strconv.FormatInt(a.taskID, 10)+":%").Scan(&balance, &awards, &rewardXP, &rewardCoin); err != nil {
		t.Fatal(err)
	}
	if balance != 93 || awards != 1 || rewardXP != 1 || rewardCoin != 1 {
		t.Fatalf("actual restored answer balance/award/taskXP/taskCoin=%d/%d/%d/%d want93/1/1/1", balance, awards, rewardXP, rewardCoin)
	}
	var eventID string
	var notification []byte
	if err := f.db.QueryRow(f.ctx, `select event_id,payload from nats_outbox where payload->>'templateKey'='question_answer_accepted' and payload->'data'->>'communityPostId'=$1`, a.postID).Scan(&eventID, &notification); err != nil {
		t.Fatal(err)
	}
	worker := &NotificationWorker{db: f.db, cache: f.server.cache}
	for range 2 {
		if err := worker.handleEvent(queue.WithEventID(f.ctx, eventID), notification); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.number(t, `select count(*) from notifications where source_event_id=$1 and recipient_id=$2`, eventID, a.authorID); n != 1 {
		t.Fatal("actual duplicate notification consumer did not preserve exactly one accepted-answer notification")
	}
	t.Log("CMP004 actualdefaultpolicy→pending→independentHTTPmoderator→visiblebody/count1→legalbountyaward90/tax10+configuredtask25XP/3coin once; actualmonitorrestart and duplicateNotificationWorker; noNATSTransport/nativeUIclaim")
}

// The server below never invents a moderation decision. The external native
// browser must use the actual moderator UI and ordinary session login. Only the
// schema setup/drop use the shared DDL lock; the live service does not hold it.
func TestAuditLiveDefaultPolicyCommentModerationFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_COMMENT_MODERATION") != "1" {
		t.Skip("opt-in owned actual default-policy comment moderation native fixture")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	frontend := os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
	if frontend == "" {
		t.Fatal("declare exact clean frontend checkout")
	}
	revisions := map[string]any{}
	for repo, checkout := range map[string]string{"mcmods-cn-backend": strings.TrimSpace(string(backend)), "mcmods-cn-frontend": frontend, "mcmods_exporter": "/workspace/mcmods_exporter"} {
		revision := auditJourneyRevision(checkout)
		if revision["error"] != nil || revision["dirty"] != false {
			t.Fatalf("clean exact source required: %s", repo)
		}
		revisions[repo] = revision
	}
	directory := os.Getenv("MCMODS_AUDIT_COMMENT_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("new absolute owned evidence directory required")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-default-comment-moderation pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	const frontendOrigin = "http://127.0.0.1:3391"
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	unlock := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlock() })
	// The actor/service TTL starts after the shared setup window is acquired.
	a := auditCMP004DefaultPolicyScenario(t, 45*time.Minute)
	unlock()
	t.Cleanup(func() { unlock = auditGlobalCatalogPGLock(t) })
	f := a.f
	// Explicitly authorized moderation-console user; no wildcard, no immunity,
	// no permissions for the ordinary comment author, no production policy edit.
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "admin.access")
	var account string
	if err = f.db.QueryRow(f.ctx, `select username from users where id=$1`, f.userIDs[f.reviewer]).Scan(&account); err != nil {
		t.Fatal(err)
	}
	password := "Audit-private-comment-" + randomCatalogPublicID() + "-2026!"
	hash, err := security.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update users set password_hash=$2 where id=$1`, f.userIDs[f.reviewer], hash); err != nil {
		t.Fatal(err)
	}
	// Password setup invalidated the helper token. A normal HTTP login obtains
	// its replacement; the browser later performs its own real native login.
	payload, err := json.Marshal(loginRequest{Account: account, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+"/api/v1/auth/login", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", frontendOrigin)
	res, err := f.origin.Client().Do(req)
	if err != nil {
		t.Fatal("owned normal moderator login failed")
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(res.Body, 4<<20))
	res.Body.Close()
	if res.StatusCode != 200 || readErr != nil {
		t.Fatalf("owned normal moderator login status=%d", res.StatusCode)
	}
	var token string
	for _, cookie := range res.Cookies() {
		if cookie.Name == authSessionCookieName {
			token = cookie.Value
		}
	}
	if token == "" {
		t.Fatal("normal moderator login omitted session cookie")
	}
	f.userIDs[token] = f.userIDs[f.reviewer]
	f.reviewer = token
	a.f = f
	me := f.require(t, f.reviewer, http.MethodGet, "/api/v1/auth/me", nil, 200)
	if !bytes.Contains(me, []byte("admin.access")) || !bytes.Contains(me, []byte("comment.moderate")) {
		t.Fatal("ordinary configured moderator lacks exact UI permissions")
	}
	authFile := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authFile, map[string]any{"moderatorAccount": account, "moderatorPassword": password, "moderatorToken": token, "authorToken": a.authorToken, "ordinaryToken": f.denied})
	_ = auditFixActivityMonitor(t, f.test013Fixture)
	var schema string
	if err = f.db.QueryRow(f.ctx, `select current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	runtime := map[string]any{"owner": "mcmods-live-default-comment-moderation", "state": "READY", "pid": os.Getpid(), "apiOrigin": f.origin.URL, "frontendOrigin": frontendOrigin, "repoRevisions": revisions, "authFile": authFile, "ownedSchema": schema, "postId": a.postID, "postPath": communityPostPath("discussion", a.postID), "commentId": a.pendingID, "body": a.body, "authorPublicId": a.authorPublic, "authorInternalId": a.authorID, "moderatorInternalId": f.userIDs[token], "taskId": a.taskID, "defaultPolicyRisk": 47, "defaultPolicyChanged": false, "stopPath": filepath.Join(directory, "STOP"), "createdAt": time.Now().UTC().Format(time.RFC3339Nano), "layer": "Actual default antiabuse HTTP pending prerequisite and actual PostgreSQL/service; external native moderator UI required; actual local activity consumer, no NATS transport claim; synthetic actors/currency only"}
	update := func(state string) {
		runtime["state"] = state
		runtime["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
	}
	update("READY")
	t.Logf("DEFAULT_COMMENT_MODERATION_READY runtime=%s; credentials private; no native acceptance inferred", filepath.Join(directory, "runtime.json"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			update("EXPIRED")
			t.Fatal("owned comment moderation fixture expired before normal STOP")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(directory, "STOP")); err == nil {
				public := f.require(t, "", http.MethodGet, a.commentPath, nil, 200)
				var response struct {
					Data struct {
						Total int               `json:"total"`
						Items []commentResponse `json:"items"`
					} `json:"data"`
				}
				if json.Unmarshal(public, &response) != nil || response.Data.Total != 1 || len(response.Data.Items) != 1 || response.Data.Items[0].ID != a.pendingID || !bytes.Contains(public, []byte(a.body)) {
					t.Fatal("native STOP did not retain exact anonymous published body/count")
				}
				auditFixWaitActivity(t, func() bool {
					return f.number(t, `select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null and progress=1`, a.authorID, a.taskID) == 1
				})
				if f.number(t, `select count(*) from audit_events where aggregate_type='comment_moderation' and aggregate_key=$1`, a.pendingID) != 3 || f.number(t, `select count(*) from experience_transactions where user_id=$1 and reason='task_reward' and reference_key like $2 and amount_delta=25`, a.authorID, strconv.FormatInt(a.taskID, 10)+":%") != 1 {
					t.Fatal("native reject/restore/approve audit or actual reward consumer is not exactly once")
				}
				var seal string
				if err = f.db.QueryRow(f.ctx, `select jsonb_build_object('comment',(select to_jsonb(c) from comments c where public_id=$1),'decisions',(select jsonb_agg(jsonb_build_object('action',action,'actorId',actor_id) order by id) from audit_events where aggregate_type='comment_moderation' and aggregate_key=$1),'taskProgress',(select to_jsonb(p) from user_task_progress p where user_id=$2 and task_id=$3),'xpRewardRows',(select count(*) from experience_transactions where user_id=$2 and reason='task_reward' and reference_key like $4 and amount_delta=25))::text`, a.pendingID, a.authorID, a.taskID, strconv.FormatInt(a.taskID, 10)+":%").Scan(&seal); err != nil {
					t.Fatal(err)
				}
				auditJourneyWriteJSON(t, filepath.Join(directory, "final-postconditions.json"), json.RawMessage(seal))
				update("STOPPED")
				t.Log("DEFAULT_COMMENT_MODERATION_STOP actual public/body/count +3 independent moderation decisions +actual consumer reward once, normal owned cleanup follows")
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			update("READY")
		}
	}
}
