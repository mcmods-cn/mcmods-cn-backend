package httpapi

import (
	"context"
	"testing"
)

func TestOCT02CommentLogLastAttemptCrashReachesTerminalFailureIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `create table comment_log_attachment_jobs(id bigint primary key,comment_id bigint,attachment_file_id bigint,
 status text,attempts integer,max_attempts integer,next_attempt_at timestamptz,lease_expires_at timestamptz,locked_by text,last_error text,finished_at timestamptz,updated_at timestamptz);
 create table comment_attachments(comment_id bigint,attachment_file_id bigint,kind text,processing_status text);
 insert into comment_log_attachment_jobs values(7,8,9,'processing',3,3,now(),now()-interval '1 second','crashed-fixture','',null,now());
 insert into comment_attachments values(8,9,'log','processing')`); err != nil {
		t.Fatal(err)
	}
	worker := &CommentLogAttachmentWorker{db: pool}
	if _, err := worker.recoverDueJobs(ctx); err != nil {
		t.Fatal(err)
	}
	var jobStatus, attachmentStatus, owner string
	if err := pool.QueryRow(ctx, `select job.status,attachment.processing_status,job.locked_by
 from comment_log_attachment_jobs job join comment_attachments attachment on attachment.comment_id=job.comment_id and attachment.attachment_file_id=job.attachment_file_id where job.id=7`).Scan(&jobStatus, &attachmentStatus, &owner); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" || attachmentStatus != "failed" || owner != "" {
		t.Fatalf("exhausted crashed job/attachment/owner=%s/%s/%s, want failed/failed/empty", jobStatus, attachmentStatus, owner)
	}
	if _, err := worker.recoverDueJobs(ctx); err != nil {
		t.Fatal(err)
	} // Terminal transition is idempotent.
}
