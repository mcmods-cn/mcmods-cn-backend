package httpapi

import (
	"context"
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
