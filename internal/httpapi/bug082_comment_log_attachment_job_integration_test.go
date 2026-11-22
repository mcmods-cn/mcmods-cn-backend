package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func TestCommentLogAttachmentJobsRollbackRetryAndRecoverIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	fileID := uploadTestLogFile(t, f.test037Fixture, "latest.log", []byte("token=synthetic\nplayer joined\n"))
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox add constraint bug082_test_outbox_available
		check(event_type<>'comment.log_attachment.requested')`); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/comment-targets/mod/" + f.modCode + "/comments"
	request := createCommentRequest{Body: "log attachment", IdempotencyKey: "bug082-comment-log", AttachmentFileIDs: []string{fileID}}
	f.require(t, f.editor, http.MethodPost, path, request, http.StatusInternalServerError)
	assertBUG082Counts(t, f.ctx, f.db, 0, 0, 0, 0)
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox drop constraint bug082_test_outbox_available`); err != nil {
		t.Fatal(err)
	}
	item := decodeTEST022Data[commentResponse](t, f.require(t, f.editor, http.MethodPost, path, request, http.StatusCreated))
	assertBUG082Counts(t, f.ctx, f.db, 1, 1, 1, 0)
	var jobID int64
	if err := f.db.QueryRow(f.ctx, `select id from comment_log_attachment_jobs where comment_id=(select id from comments where public_id=$1)`, item.ID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}

	var providerReads atomic.Int32
	unavailable := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.Header.Get("Authorization") != "" {
			providerReads.Add(1)
		}
		response.Header().Set("Content-Type", "application/xml")
		response.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(response, `<Error><Code>ServiceUnavailable</Code><Message>synthetic outage</Message></Error>`)
	}))
	defer unavailable.Close()
	setProvider := func(endpoint string) {
		cfg := f.cfg
		cfg.Endpoint = endpoint
		sealed, err := f.server.sealSystemSetting(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealed); err != nil {
			t.Fatal(err)
		}
	}
	setProvider(unavailable.URL)
	worker := NewCommentLogAttachmentWorker(f.server)
	if err := worker.processJob(f.ctx, jobID); err == nil || providerReads.Load() == 0 {
		t.Fatalf("expected the provider's HTTP failure to reach the job: signed reads=%d err=%v", providerReads.Load(), err)
	}
	var status, attachmentStatus string
	var attempts int
	if err := f.db.QueryRow(f.ctx, `select job.status,job.attempts,attachment.processing_status
		from comment_log_attachment_jobs job join comment_attachments attachment
		on attachment.comment_id=job.comment_id and attachment.attachment_file_id=job.attachment_file_id where job.id=$1`, jobID).
		Scan(&status, &attempts, &attachmentStatus); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || attempts != 1 || attachmentStatus != "processing" {
		t.Fatalf("retry state = %s attempts=%d attachment=%s", status, attempts, attachmentStatus)
	}
	setProvider(f.cfg.Endpoint)
	if _, err := f.db.Exec(f.ctx, `update comment_log_attachment_jobs set next_attempt_at=now() where id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := worker.processJob(f.ctx, jobID); err != nil {
		t.Fatal(err)
	}
	assertBUG082Completed(t, f.ctx, f.db, jobID, 2)
	if err := worker.processJob(f.ctx, jobID); err != nil {
		t.Fatalf("completed delivery was not idempotent: %v", err)
	}
	assertBUG082Completed(t, f.ctx, f.db, jobID, 2)

	_, _, restartJobID := createTestCommentLogAttachment(t, f, f.modCode, "restart-log")
	if _, err := f.db.Exec(f.ctx, `update comment_log_attachment_jobs set status='processing',attempts=1,
		locked_by='crashed-worker',lease_expires_at=now()-interval '1 second' where id=$1`, restartJobID); err != nil {
		t.Fatal(err)
	}
	policy := config.NATSConfig{Tasks: []config.NATSTaskConfig{{Code: "comment_log_attachment", Enabled: false}}}
	worker.queue = queue.New(f.ctx, policy)
	defer worker.queue.Close()
	if n, err := worker.recoverDueJobs(f.ctx); err != nil || n != 0 {
		t.Fatalf("paused recovery started work: %d/%v", n, err)
	}
	if err := f.db.QueryRow(f.ctx, `select attempts from comment_log_attachment_jobs where id=$1`, restartJobID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("paused recovery consumed an attempt: %d/%v", attempts, err)
	}
	policy.Tasks[0].Enabled = true
	if err := worker.queue.Reconfigure(policy); err != nil {
		t.Fatal(err)
	}
	recovered, recoveryErr := worker.recoverDueJobs(f.ctx)
	if recoveryErr != nil || recovered != 1 {
		t.Fatalf("restart recovery = %d/%v", recovered, recoveryErr)
	}
	assertBUG082Completed(t, f.ctx, f.db, restartJobID, 2)
}

func assertBUG082Counts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, comments, attachments, jobs, bindings int) {
	t.Helper()
	var actualComments, actualAttachments, actualJobs, actualBindings int
	if err := pool.QueryRow(ctx, `select (select count(*) from comments),(select count(*) from comment_attachments),
		(select count(*) from comment_log_attachment_jobs),(select count(*) from comment_log_bindings)`).
		Scan(&actualComments, &actualAttachments, &actualJobs, &actualBindings); err != nil {
		t.Fatal(err)
	}
	if actualComments != comments || actualAttachments != attachments || actualJobs != jobs || actualBindings != bindings {
		t.Fatalf("counts comments/attachments/jobs/bindings = %d/%d/%d/%d, want %d/%d/%d/%d",
			actualComments, actualAttachments, actualJobs, actualBindings, comments, attachments, jobs, bindings)
	}
}

func assertBUG082Completed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID int64, attempts int) {
	t.Helper()
	var status, attachmentStatus string
	var actualAttempts, bindings, shares, entries int
	if err := pool.QueryRow(ctx, `select job.status,job.attempts,attachment.processing_status,
		(select count(*) from comment_log_bindings where comment_id=job.comment_id and attachment_file_id=job.attachment_file_id),
		(select count(*) from log_shares where source_file_id=job.attachment_file_id),
		(select count(*) from log_share_entries entry join log_shares share on share.id=entry.log_share_id where share.source_file_id=job.attachment_file_id)
		from comment_log_attachment_jobs job join comment_attachments attachment
		on attachment.comment_id=job.comment_id and attachment.attachment_file_id=job.attachment_file_id where job.id=$1`, jobID).
		Scan(&status, &actualAttempts, &attachmentStatus, &bindings, &shares, &entries); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || actualAttempts != attempts || attachmentStatus != "ready" || bindings != 1 || shares != 1 || entries != 1 {
		t.Fatalf("completed state = status %s attempts %d attachment %s binding/share/entry %d/%d/%d",
			status, actualAttempts, attachmentStatus, bindings, shares, entries)
	}
}
