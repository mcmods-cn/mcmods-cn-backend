package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// Acquire an ordinary PostgreSQL lock without modifying the blocked row.
// Observe the actual waiter before the lease ends and keep the lock until the
// database clock has crossed that same deadline.
func TestCommentLogAttachmentLeaseExpiresDuringWriteIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	worker := NewCommentLogAttachmentWorker(f.server)
	for _, operation := range []string{"complete-job-lock", "complete-binding-lock", "retry-job-lock", "terminal-attachment-lock"} {
		t.Run(operation, func(t *testing.T) {
			raw := []byte("ordinary comment log " + operation)
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", operation+".log", "text/plain", "comment", raw)
			f.put(t, ticket, raw)
			file := decodeTEST022Data[struct{ ID string }](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
			comment := f.comment(t, f.editor, f.modCode, operation, "Log attachment "+operation, "", file.ID)
			var id int64
			if err := f.db.QueryRow(f.ctx, `select id from comment_log_attachment_jobs where comment_id=(select id from comments where public_id=$1)`, comment.ID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if operation == "terminal-attachment-lock" {
				if _, err := f.db.Exec(f.ctx, `update comment_log_attachment_jobs set max_attempts=1 where id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
			job, claimed, err := worker.claimJob(f.ctx, id)
			if err != nil || !claimed {
				t.Fatalf("claim=%v error=%v", claimed, err)
			}
			prepared, err := prepareFileLogShare(f.ctx, f.db, worker.readSource, job.requestedBy, file.ID)
			if err != nil {
				t.Fatal(err)
			}
			var deadline time.Time
			var before string
			if err = f.db.QueryRow(f.ctx, `update comment_log_attachment_jobs job set lease_expires_at=clock_timestamp()+interval '2 seconds'
				where id=$1 returning lease_expires_at,to_jsonb(job)::text`, id).Scan(&deadline, &before); err != nil {
				t.Fatal(err)
			}
			holder, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(f.ctx)
			switch operation {
			case "complete-binding-lock":
				// This waits at INSERT after the share and its entries were built in
				// the worker transaction; an empty binding has no tuple to lock.
				_, err = holder.Exec(f.ctx, `lock table comment_log_bindings in share mode`)
			case "terminal-attachment-lock":
				_, err = holder.Exec(f.ctx, `select comment_id from comment_attachments where comment_id=$1 and attachment_file_id=$2 for update`, job.commentID, job.fileID)
			default:
				_, err = holder.Exec(f.ctx, `select id from comment_log_attachment_jobs where id=$1 for update`, id)
			}
			if err != nil {
				t.Fatal(err)
			}
			blocker := int(holder.Conn().PgConn().PID())
			result := make(chan error, 1)
			go func() {
				if operation == "retry-job-lock" || operation == "terminal-attachment-lock" {
					result <- worker.failJob(f.ctx, job, errors.New("ordinary attachment processing error"))
					return
				}
				result <- worker.completeJob(f.ctx, job, prepared)
			}()
			wait, stop := context.WithTimeout(f.ctx, 8*time.Second)
			defer stop()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			observed := false
			for {
				var waiting, expired bool
				if err = f.db.QueryRow(wait, `select exists(select 1 from pg_stat_activity
					where datname=current_database() and $1=any(pg_blocking_pids(pid)) and wait_event_type='Lock'),clock_timestamp()>=$2`,
					blocker, deadline).Scan(&waiting, &expired); err != nil {
					t.Fatal(err)
				}
				if waiting && !expired {
					observed = true
				}
				if expired {
					if !observed || !waiting {
						t.Fatal("expected the same PostgreSQL waiter on both sides of the unmodified lease deadline")
					}
					break
				}
				select {
				case <-ticker.C:
				case <-wait.Done():
					t.Fatal(wait.Err())
				}
			}
			if err = holder.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			err = <-result
			var status, attachment, after string
			var shares, entries, bindings int
			if readErr := f.db.QueryRow(f.ctx, `select job.status,attachment.processing_status,to_jsonb(job)::text,
				(select count(*) from log_shares where source_file_id=job.attachment_file_id),
				(select count(*) from log_share_entries entry join log_shares share on share.id=entry.log_share_id where share.source_file_id=job.attachment_file_id),
				(select count(*) from comment_log_bindings where comment_id=job.comment_id and attachment_file_id=job.attachment_file_id)
				from comment_log_attachment_jobs job join comment_attachments attachment on attachment.comment_id=job.comment_id and attachment.attachment_file_id=job.attachment_file_id
				where job.id=$1`, id).Scan(&status, &attachment, &after, &shares, &entries, &bindings); readErr != nil {
				t.Fatal(readErr)
			}
			if err == nil || before != after || attachment != "processing" || shares+entries+bindings != 0 {
				t.Fatalf("expired attempt returned %v; job=%s attachment=%s shares=%d entries=%d bindings=%d", err, status, attachment, shares, entries, bindings)
			}
			if operation == "terminal-attachment-lock" {
				if _, err = worker.recoverDueJobs(f.ctx); err != nil {
					t.Fatal(err)
				}
				if err = f.db.QueryRow(f.ctx, `select job.status,attachment.processing_status from comment_log_attachment_jobs job join comment_attachments attachment
					on attachment.comment_id=job.comment_id and attachment.attachment_file_id=job.attachment_file_id where job.id=$1`, id).Scan(&status, &attachment); err != nil || status != "failed" || attachment != "failed" {
					t.Fatalf("exhausted lease recovery: %s/%s %v", status, attachment, err)
				}
			} else {
				if err = worker.processJob(f.ctx, id); err != nil {
					t.Fatal(err)
				}
				assertBUG082Completed(t, f.ctx, f.db, id, 2)
			}
		})
	}
}
