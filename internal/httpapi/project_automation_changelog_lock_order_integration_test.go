package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestProjectChangelogManualPublicationAndSyncShareLockOrderIntegration(t *testing.T) {
	fixture := newTEST017Fixture(t)
	publicID, worker, job, release := fixture.managed(t, false)
	grantTEST044Permissions(t, fixture.ctx, fixture.db, fixture.userIDs[fixture.editor], "project.no-review."+fixture.modCode)

	const manualBody = "Published by the project editor while an upstream change arrived"
	edit := test017Snapshot(manualBody)
	edit.BaseRevisionID = fixture.detail(t, fixture.editor, publicID, http.StatusOK).PublishedRevisionID
	if edit.BaseRevisionID == nil {
		t.Fatal("managed changelog did not publish its initial revision")
	}
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	fixture.ctx = ctx
	barrier, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(barrier)
	if _, err = barrier.Exec(ctx, "select pg_advisory_xact_lock(hashtext($1))", projectChangelogAggregate+":"+publicID); err != nil {
		t.Fatal(err)
	}
	barrierPID := barrier.Conn().PgConn().PID()
	waitForAdvisoryQueue := func(want int) {
		t.Helper()
		poll := time.NewTicker(10 * time.Millisecond)
		defer poll.Stop()
		for {
			var waiters int
			if e := fixture.db.QueryRow(ctx, `with held as (
				select database,classid,objid,objsubid from pg_locks
				where pid=$1 and locktype='advisory' and granted
			) select count(*) from pg_locks waiter join held using(database,classid,objid,objsubid)
				where waiter.locktype='advisory' and not waiter.granted`, barrierPID).Scan(&waiters); e != nil {
				t.Fatal(e)
			}
			if waiters == want {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("observed %d waiters on the changelog, want %d", waiters, want)
			case <-poll.C:
			}
		}
	}

	editResult := auditCompletionAsyncJSON(fixture.test013Fixture, fixture.editor, http.MethodPut, "/api/v1/changelogs/"+publicID, edit)
	waitForAdvisoryQueue(1)
	type syncResult struct {
		counts map[string]any
		err    error
	}
	syncDone := make(chan syncResult, 1)
	release.Body = "A different upstream release description"
	go func() {
		counts, syncErr := worker.syncChangelogs(ctx, job, []projectAutomationRelease{release})
		syncDone <- syncResult{counts, syncErr}
	}()
	waitForAdvisoryQueue(2)
	if err = barrier.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var saved auditCompletionHTTPResult
	var synced syncResult
	select {
	case saved = <-editResult:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case synced = <-syncDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if saved.err != nil || saved.status != http.StatusOK || synced.err != nil || synced.counts["manualOverrideConflicts"] != 1 {
		t.Fatalf("concurrent publication: HTTP %d (%v), sync count=%v err=%v", saved.status, saved.err, synced.counts, synced.err)
	}
	assertBUG034ManualOverride(t, ctx, fixture.db, publicID, true)
	if visible := fixture.detail(t, "", publicID, http.StatusOK); visible.BodyMarkdown != manualBody {
		t.Fatalf("published changelog body=%q, want the approved edit", visible.BodyMarkdown)
	}
}

func TestManualChangelogSubmissionReadsPolicyAfterProjectAndRevisionLocksIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	ctx, cancel := context.WithTimeout(f.ctx, 25*time.Second)
	defer cancel()
	f.ctx = ctx
	base := "/api/v1/changelogs?targetType=mod&targetId=" + f.modCode
	initial := f.submit(t, f.editor, http.MethodPost, base, test017Snapshot("Original published version"), http.StatusCreated)
	f.review(t, f.denied, initial, "approved", http.StatusOK)
	edit := test017Snapshot("A new version requiring independent review")
	edit.BaseRevisionID = f.detail(t, f.editor, initial.ID, http.StatusOK).PublishedRevisionID

	grantTEST044Permissions(t, ctx, f.db, f.userIDs[f.editor], "project.no-review."+f.modCode)
	barrier, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(barrier)
	if _, err = barrier.Exec(ctx, "select pg_advisory_xact_lock(hashtext($1))", projectChangelogAggregate+":"+initial.ID); err != nil {
		t.Fatal(err)
	}
	saving := auditCompletionAsyncJSON(f.test013Fixture, f.editor, http.MethodPut, "/api/v1/changelogs/"+initial.ID, edit)
	auditCompletionAwaitLocks(t, f.test013Fixture, 1)
	if _, err = f.db.Exec(ctx, `delete from user_permissions using permissions
		where user_permissions.permission_id=permissions.id and user_id=$1 and permissions.code=$2`, f.userIDs[f.editor], "project.no-review."+f.modCode); err != nil {
		t.Fatal(err)
	}
	if err = barrier.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	saved := <-saving
	var result struct {
		Data struct{ ReviewStatus, ChangeRequestID string }
	}
	if saved.err != nil || saved.status != http.StatusOK || json.Unmarshal(saved.raw, &result) != nil ||
		result.Data.ReviewStatus != "pending" || result.Data.ChangeRequestID == "" {
		t.Fatalf("review-required save: status=%d receipt=%+v err=%v", saved.status, result, saved.err)
	}
	if got := f.detail(t, "", initial.ID, http.StatusOK); got.BodyMarkdown != "Original published version" {
		t.Fatalf("public body changed before review: %q", got.BodyMarkdown)
	}

	// The administrator can withdraw the parent while a new submission waits.
	hide, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(hide)
	if _, err = hide.Exec(ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	creating := auditCompletionAsyncJSON(f.test013Fixture, f.editor, http.MethodPost, base, test017Snapshot("Parent no longer published"))
	auditCompletionAwaitLocks(t, f.test013Fixture, 1)
	if err = hide.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	rejected := <-creating
	if rejected.err != nil || rejected.status != http.StatusNotFound {
		t.Fatalf("hidden parent creation: status=%d err=%v", rejected.status, rejected.err)
	}
	var revision, state string
	if err = f.db.QueryRow(ctx, `select revision.public_id,request.status from change_requests request
		join content_revisions revision on revision.id=request.proposed_revision_id where request.public_id=$1`, result.Data.ChangeRequestID).Scan(&revision, &state); err != nil || state != "pending" {
		t.Fatalf("pending review state=%q err=%v", state, err)
	}
	proposal := test017Mutation{RevisionID: revision}
	f.review(t, f.denied, proposal, "approved", http.StatusConflict)
	f.review(t, f.denied, proposal, "rejected", http.StatusOK)
	var entries int
	if err = f.db.QueryRow(ctx, `select count(*) from project_changelogs entry join public_routes project on project.id=entry.object_route_id where project.public_id=$1`, f.modCode).Scan(&entries); err != nil || entries != 1 {
		t.Fatalf("published + aborted create: entries=%d err=%v", entries, err)
	}
}
