package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestAcceptedBountyAnswerSurvivesConcurrentAuthorDeletionIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	f.reviewPolicy(t, false)
	currency := f.seedMoney(t)
	snapshot := f.snapshot("discussion", "Question with a concurrent answer decision")
	snapshot.BountyCurrency, snapshot.BountyAmount = "test042", 100
	question := f.create(t, f.editor, snapshot)
	answer := f.comment(t, f.otherEditor, question.ID, "award-while-answer-editor-deletes")

	ctx, cancel := context.WithTimeout(f.ctx, 20*time.Second)
	defer cancel()
	balanceLock, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer balanceLock.Rollback(ctx)
	if _, err = lockCurrencyBalanceTx(ctx, balanceLock, f.userIDs[f.otherEditor], currency); err != nil {
		t.Fatal(err)
	}
	balancePID := int32(balanceLock.Conn().PgConn().PID())
	type result struct {
		status int
		err    error
	}
	start := func(token, method, path string) <-chan result {
		returned := make(chan result, 1)
		go func() {
			status, _, err := test048HTTPRequest(ctx, f.origin.Client(), f.origin.URL, token, method, path, "null")
			returned <- result{status: status, err: err}
		}()
		return returned
	}
	waitBehind := func(blockerPID int32, returned <-chan result) int32 {
		t.Helper()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			var waiterPID *int32
			if err := f.db.QueryRow(ctx, `select (select pid from pg_stat_activity where datname=current_database()
				and wait_event_type='Lock' and $1=any(pg_blocking_pids(pid)) limit 1)`, blockerPID).Scan(&waiterPID); err != nil {
				t.Fatal(err)
			}
			if waiterPID != nil {
				return *waiterPID
			}
			select {
			case early := <-returned:
				t.Fatalf("ordinary operation finished before its expected row lock: status=%d err=%v", early.status, early.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-ticker.C:
			}
		}
	}

	// The award holds the question and answer rows while the recipient's balance
	// is busy. Bring the deletion to its real conflicting row before release.
	accept := start(f.editor, http.MethodPost, "/api/v1/community/posts/"+question.ID+"/answers/"+answer.ID)
	acceptPID := waitBehind(balancePID, accept)
	deleted := start(f.otherEditor, http.MethodDelete, "/api/v1/comments/"+answer.ID)
	waitBehind(acceptPID, deleted)
	if err = balanceLock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	expectOutcome := func(returned <-chan result, want int) {
		t.Helper()
		select {
		case actual := <-returned:
			if actual.status != want || actual.err != nil {
				t.Errorf("concurrent operation status=%d want=%d err=%v", actual.status, want, actual.err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	expectOutcome(accept, http.StatusOK)
	expectOutcome(deleted, http.StatusConflict)

	visible := f.detail(t, "", question.ID)
	if visible.ResolutionStatus != "answered" || visible.AcceptedCommentID != answer.ID || visible.Bounty == nil || visible.Bounty.Status != "awarded" {
		t.Error("the question no longer points to its settled answer")
	}
	var status, body string
	var awards int
	if err = f.db.QueryRow(f.ctx, `select status,body,
		(select count(*)::int from currency_transactions where reference_key=$2 and transaction_type='question_bounty_award')
		from comments where public_id=$1`, answer.ID, question.ID).Scan(&status, &body, &awards); err != nil {
		t.Fatal(err)
	}
	if status != "published" || body != answer.Body || awards != 1 {
		t.Errorf("accepted answer status=%q original body kept=%v awards=%d", status, body == answer.Body, awards)
	}
	f.balance(t, f.editor, currency, 900)
	f.balance(t, f.otherEditor, currency, 1090)
}
