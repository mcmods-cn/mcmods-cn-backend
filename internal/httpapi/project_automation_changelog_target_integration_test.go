package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagedChangelogUsesCurrentApprovedParentIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	id, worker, job, release := f.managed(t, false)
	var previous int64
	if err := f.db.QueryRow(f.ctx, `select published_revision_id from project_changelogs where public_id=$1`, id).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, job.InternalID); err != nil {
		t.Fatal(err)
	}
	release.Body = "New upstream release note"
	if _, err := worker.syncChangelogs(f.ctx, job, []projectAutomationRelease{release}); err == nil {
		t.Error("a parent awaiting approval received an automated changelog update")
	}
	var current int64
	if err := f.db.QueryRow(f.ctx, `select published_revision_id from project_changelogs where public_id=$1`, id).Scan(&current); err != nil || current != previous {
		t.Fatalf("pending parent revision changed from %d to %d: %v", previous, current, err)
	}
	// Approval is being saved on a different connection. A fresh synchronization
	// waits until that decision is known and can then publish normally.
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `update mods set review_status='approved' where id=$1`, job.InternalID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		counts, err := worker.syncChangelogs(ctx, job, []projectAutomationRelease{release})
		if err == nil && counts["updated"] != 1 {
			err = errors.New("expected a single updated changelog")
		}
		done <- err
	}()
	parentPID := tx.Conn().PgConn().PID()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := f.db.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where datname=current_database()
			and $1::int=any(pg_blocking_pids(pid)))`, parentPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("synchronization finished before approval committed: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
