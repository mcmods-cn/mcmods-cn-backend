package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/progression"
)

func auditFixActivityMonitor(t *testing.T, f test013Fixture) *activity.Monitor {
	t.Helper()
	s := f.origin.Config.Handler.(*Server)
	m := activity.NewMonitor(f.db, progression.NewService(f.db, s.cache), activity.Options{BatchSize: 16, QueueCapacity: 32, FlushInterval: 20 * time.Millisecond, RetryMinDelay: 20 * time.Millisecond, RetryMaxDelay: 50 * time.Millisecond, WriteTimeout: time.Second, DurableEnqueueTimeout: time.Second})
	// Attach the initial real monitor so the pre-fix fault exercises the actual
	// post-response middleware path as well. Restarting only the consumer keeps
	// this pointer stable; fixed writes enqueue in their own business transaction.
	if s.activity == nil {
		s.activity = m
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}
func auditFixActivityTask(t *testing.T, f test013Fixture) int64 {
	t.Helper()
	if _, err := f.db.Exec(f.ctx, `update task_definitions set status='disabled'`); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.db.QueryRow(f.ctx, `insert into task_definitions(code,name,condition,rewards,status) values('audit_http_comment_reward','Audit HTTP comment reward','{"action":"create","objectType":"comment","metric":"count","target":1}'::jsonb,'{"experience":25,"currencies":{"diamond":3}}'::jsonb,'active') returning id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func auditFixActivityFacts(t *testing.T, f test013Fixture, user, task int64) (comments, events, progress, rewarded, currency, xp, ledgers int64) {
	t.Helper()
	err := f.db.QueryRow(f.ctx, `select (select count(*) from comments where author_id=$1),
 (select count(*) from user_activity_events where user_id=$1 and action_id=$3 and object_type_id=$4),
 coalesce((select sum(progress) from user_task_progress where user_id=$1 and task_id=$2),0),
 coalesce((select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null),0),
 coalesce((select b.balance from user_currency_balances b join currencies c on c.id=b.currency_id where b.user_id=$1 and c.code='diamond'),0),
 coalesce((select experience from user_experience where user_id=$1),0),(select count(*) from currency_transactions where user_id=$1)`, user, task, activity.ActionCreate, activity.ObjectComment).Scan(&comments, &events, &progress, &rewarded, &currency, &xp, &ledgers)
	if err != nil {
		t.Fatal(err)
	}
	return
}
func auditFixWaitActivity(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("bounded activity drain did not settle")
}
func auditFixRequireRewardFacts(t *testing.T, f test013Fixture, user, task int64) {
	t.Helper()
	c, e, p, r, b, x, l := auditFixActivityFacts(t, f, user, task)
	if c != 1 || e != 1 || p != 1 || r != 1 || b != 3 || x != 25 || l != 1 {
		t.Fatalf("comments/events/progress/rewarded/currency/xp/ledger=%d/%d/%d/%d/%d/%d/%d", c, e, p, r, b, x, l)
	}
}

func TestAuditFixCMP003CommentConcurrentIdempotencyRewardExactlyOnce(t *testing.T) {
	fixture := newTEST038Fixture(t)
	f := fixture.test013Fixture
	task := auditFixActivityTask(t, f)
	auditFixActivityMonitor(t, f)
	start := make(chan struct{})
	out := make(chan auditCompletionHTTPResult, 8)
	body := map[string]any{"body": "Actual HTTP exactly once comment", "idempotencyKey": "audit-cmp-atomic-unique"}
	for range 8 {
		go func() {
			<-start
			out <- <-auditCompletionAsyncJSON(f, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body)
		}()
	}
	close(start)
	created, replayed := 0, 0
	for range 8 {
		result := <-out
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.status == 201 {
			created++
		} else if result.status == 200 {
			replayed++
		} else {
			t.Fatalf("idempotency HTTP%d %s", result.status, result.raw)
		}
	}
	if created != 1 || replayed != 7 {
		t.Fatalf("created/replayed=%d/%d", created, replayed)
	}
	auditFixWaitActivity(t, func() bool {
		_, e, _, _, b, _, _ := auditFixActivityFacts(t, f, f.userIDs[f.editor], task)
		return e == 1 && b == 3
	})
	auditFixRequireRewardFacts(t, f, f.userIDs[f.editor], task)
}

func TestAuditFixCMP003CommentOutboxFailureIsAtomicAndRecoversAfterMonitorRestart(t *testing.T) {
	fixture := newTEST038Fixture(t)
	f := fixture.test013Fixture
	task := auditFixActivityTask(t, f)
	m := auditFixActivityMonitor(t, f)
	if _, err := f.db.Exec(f.ctx, fmt.Sprintf(`alter table activity_event_outbox add constraint audit_reject_activity_enqueue check(user_id<>%d) not valid`, f.userIDs[f.editor])); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"body": "Actual HTTP failure zero write", "idempotencyKey": "audit-cmp-atomic-recovery"}
	raw := f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body, 500)
	if !bytes.Contains(raw, []byte("保存评论活动失败")) {
		t.Fatalf("unrelated SQL failure satisfied fault test: %s", raw)
	}
	c, e, p, r, b, x, l := auditFixActivityFacts(t, f, f.userIDs[f.editor], task)
	if c+e+p+r+b+x+l != 0 {
		t.Fatalf("failed business request left partial side effects %d/%d/%d/%d/%d/%d/%d", c, e, p, r, b, x, l)
	}
	if _, err := f.db.Exec(f.ctx, `alter table activity_event_outbox drop constraint audit_reject_activity_enqueue`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	auditFixActivityMonitor(t, f)
	f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body, 201)
	f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body, 200)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", map[string]any{"body": "Healthy recovery control", "idempotencyKey": "audit-cmp-atomic-control"}, 201)
	auditFixWaitActivity(t, func() bool {
		_, _, _, _, a, _, _ := auditFixActivityFacts(t, f, f.userIDs[f.editor], task)
		_, _, _, _, b, _, _ := auditFixActivityFacts(t, f, f.userIDs[f.otherEditor], task)
		return a == 3 && b == 3
	})
	auditFixRequireRewardFacts(t, f, f.userIDs[f.editor], task)
	auditFixRequireRewardFacts(t, f, f.userIDs[f.otherEditor], task)
}

func TestAuditFixCMP003CommentEditDeleteActivityFailureRollsBackBodyAndBindings(t *testing.T) {
	fixture := newTEST038Fixture(t)
	f := fixture.test013Fixture
	item := fixture.comment(t, f.editor, f.modCode, "audit-edit-delete", "Original atomic comment", "")
	inject := func(on bool) {
		statement := `alter table activity_event_outbox drop constraint audit_reject_activity_enqueue`
		if on {
			statement = fmt.Sprintf(`alter table activity_event_outbox add constraint audit_reject_activity_enqueue check(user_id<>%d) not valid`, f.userIDs[f.editor])
		}
		if _, err := f.db.Exec(f.ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/comments/" + item.ID
	replacement := "Edited atomic comment with added words"
	update := updateCommentRequest{Body: replacement, BaseUpdatedAt: item.UpdatedAt}
	inject(true)
	raw := f.require(t, f.editor, http.MethodPatch, path, update, 500)
	if !bytes.Contains(raw, []byte("保存评论活动失败")) {
		t.Fatalf("wrong update failure: %s", raw)
	}
	var body, status string
	if err := f.db.QueryRow(f.ctx, `select body,status from comments where public_id=$1`, item.ID).Scan(&body, &status); err != nil || body != item.Body || status != "published" {
		t.Fatalf("failed edit left body=%q status=%q err=%v", body, status, err)
	}
	inject(false)
	f.require(t, f.editor, http.MethodPatch, path, update, 200)
	inject(true)
	raw = f.require(t, f.editor, http.MethodDelete, path, nil, 500)
	if !bytes.Contains(raw, []byte("保存评论活动失败")) {
		t.Fatalf("wrong delete failure: %s", raw)
	}
	if err := f.db.QueryRow(f.ctx, `select body,status from comments where public_id=$1`, item.ID).Scan(&body, &status); err != nil || body != replacement || status != "published" {
		t.Fatalf("failed delete changed body=%q status=%q err=%v", body, status, err)
	}
	inject(false)
	f.require(t, f.editor, http.MethodDelete, path, nil, 200)
	var edits, deletes, added, removed int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where action_id=$2),count(*) filter(where action_id=$3),
 coalesce(sum(markdown_added_bytes) filter(where action_id=$2),0),coalesce(sum(markdown_deleted_bytes) filter(where action_id=$2),0)
 from activity_event_outbox where object_public_id=$1`, item.ID, activity.ActionEdit, activity.ActionDelete).Scan(&edits, &deletes, &added, &removed); err != nil {
		t.Fatal(err)
	}
	wantAdded, wantRemoved := activity.MarkdownDeltaBytes(item.Body, replacement)
	if edits != 1 || deletes != 1 || added != wantAdded || removed != wantRemoved {
		t.Fatalf("edit/delete/add/remove=%d/%d/%d/%d want1/1/%d/%d", edits, deletes, added, removed, wantAdded, wantRemoved)
	}
}

func TestAuditFixCMP003ChangelogActivityFailureRollsBackCreationAndRevision(t *testing.T) {
	f := newTEST017Fixture(t)
	path := "/api/v1/changelogs?targetType=mod&targetId=" + f.modCode
	snapshot := test017Snapshot("Atomic changelog original")
	inject := func(on bool) {
		statement := `alter table activity_event_outbox drop constraint audit_reject_changelog_activity`
		if on {
			statement = fmt.Sprintf(`alter table activity_event_outbox add constraint audit_reject_changelog_activity check(user_id<>%d) not valid`, f.userIDs[f.editor])
		}
		if _, err := f.db.Exec(f.ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	inject(true)
	raw := f.require(t, f.editor, http.MethodPost, path, snapshot, 500)
	if !bytes.Contains(raw, []byte("failed to persist changelog activity")) {
		t.Fatalf("wrong creation failure: %s", raw)
	}
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from project_changelogs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed create count=%d err=%v", count, err)
	}
	inject(false)
	created := f.submit(t, f.editor, http.MethodPost, path, snapshot, 201)
	f.review(t, f.reviewer, created, "approved", 200)
	snapshot = test017Snapshot("Atomic changelog candidate")
	snapshot.BaseRevisionID = f.detail(t, f.editor, created.ID, 200).PublishedRevisionID
	path = "/api/v1/changelogs/" + created.ID
	inject(true)
	raw = f.require(t, f.editor, http.MethodPut, path, snapshot, 500)
	if !bytes.Contains(raw, []byte("failed to persist changelog activity")) {
		t.Fatalf("wrong edit failure: %s", raw)
	}
	if err := f.db.QueryRow(f.ctx, `select count(*) from content_revisions where aggregate_type=$1 and aggregate_key=$2`, projectChangelogAggregate, created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed edit left revisions=%d err=%v", count, err)
	}
	if item := f.detail(t, "", created.ID, 200); item.BodyMarkdown != "Atomic changelog original" {
		t.Fatalf("failed edit published body %q", item.BodyMarkdown)
	}
	inject(false)
	f.require(t, f.editor, http.MethodPut, path, snapshot, 200)
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where object_public_id=$1`, created.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("activity create/edit intents=%d want2 err=%v", count, err)
	}
}

func TestAuditFixCMP003DiagnosticIsReadOnlyAndDistinguishesQueuedIntents(t *testing.T) {
	fixture := newTEST038Fixture(t)
	f := fixture.test013Fixture
	missing := fixture.comment(t, f.editor, f.modCode, "diagnostic-lost", "Synthetic missing intent", "")
	queued := fixture.comment(t, f.editor, f.modCode, "diagnostic-queued", "Synthetic queued intent", "")
	if _, err := f.db.Exec(f.ctx, `delete from activity_event_outbox where object_public_id=$1`, missing.ID); err != nil {
		t.Fatal(err)
	}
	var author string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&author); err != nil {
		t.Fatal(err)
	}
	before := fixture.facts(t)
	command := exec.CommandContext(f.ctx, "go", "run", "../../cmd/diagnose-activity-intents", "--since", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), "--until", time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "--author-public-id", author, "--limit", "100")
	// pgx.Config.ConnString preserves the original parse string even after the
	// fixture changes ConnConfig.Database. Select the actual owned fixture DB.
	diagnosticURL, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil || diagnosticURL.Scheme == "" {
		t.Fatal("diagnostic fixture requires an explicit PostgreSQL URL")
	}
	diagnosticURL.Path = "/" + f.db.Config().ConnConfig.Database
	command.Env = append(os.Environ(), "DATABASE_URL="+diagnosticURL.String())
	output, err := command.Output()
	if err != nil {
		t.Fatalf("read-only diagnostic subprocess failed: %v", err)
	}
	var result struct {
		DryRun          bool `json:"dry_run"`
		RepairSupported bool `json:"repair_supported"`
		Pending         int  `json:"pending_intents_in_scope"`
		Candidates      []struct {
			CommentID string `json:"comment_id"`
		} `json:"missing_create_activity_candidates"`
	}
	if err = json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.RepairSupported || result.Pending != 1 || len(result.Candidates) != 1 || result.Candidates[0].CommentID != missing.ID || result.Candidates[0].CommentID == queued.ID {
		t.Fatalf("diagnostic misclassified synthetic scope: %s", output)
	}
	fixture.unchanged(t, before)
	var intents int
	if err = f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1`, f.userIDs[f.editor]).Scan(&intents); err != nil || intents != 1 {
		t.Fatalf("diagnostic mutated intents=%d err=%v", intents, err)
	}
}
