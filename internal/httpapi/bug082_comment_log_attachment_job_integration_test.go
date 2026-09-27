package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestCommentLogAttachmentJobsRollbackRetryAndRecoverIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify comment log attachment jobs")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rawLog := []byte("token=secret\nplayer joined\n")
	digest := sha256.Sum256(rawLog)
	sha := hex.EncodeToString(digest[:])
	var providerUnavailable atomic.Bool
	providerUnavailable.Store(true)
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if providerUnavailable.Load() {
			response.Header().Set("Content-Type", "application/xml")
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(response, `<Error><Code>ServiceUnavailable</Code><Message>retry</Message></Error>`)
			return
		}
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.Header().Set("Content-Length", fmt.Sprint(len(rawLog)))
		response.Header().Set("ETag", `"bug082"`)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(rawLog)
	}))
	defer provider.Close()

	if _, err = pool.Exec(ctx, `
		create temporary table system_settings(key text primary key,value jsonb not null);
		create temporary table comments(id bigint primary key);
		create temporary table oss_files(
			id bigint primary key,public_id text not null unique,object_key text not null unique,
			original_name text not null,source_original_name text not null,content_type text not null,
			size_bytes bigint not null,source_size_bytes bigint not null,sha256 text not null,uploader_id bigint not null,
			source text not null,status text not null,scan_status text not null
		);
		create temporary table comment_attachments(
			comment_id bigint not null,attachment_file_id bigint not null,kind text not null default 'file',
			processing_status text not null default 'processing',created_at timestamptz not null default now(),
			primary key(comment_id,attachment_file_id)
		);
		create temporary table comment_log_attachment_jobs(
			id bigserial primary key,comment_id bigint not null,attachment_file_id bigint not null,requested_by bigint not null,
			status text not null default 'queued',attempts integer not null default 0,max_attempts integer not null default 5,
			next_attempt_at timestamptz not null default now(),lease_expires_at timestamptz,locked_by text not null default '',
			last_error text not null default '',created_at timestamptz not null default now(),started_at timestamptz,
			finished_at timestamptz,updated_at timestamptz not null default now(),unique(comment_id,attachment_file_id)
		);
		create temporary table nats_outbox(
			id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null,
			subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null,
			payload jsonb not null,occurred_at timestamptz not null,status text not null,available_at timestamptz not null
		);
		create temporary table log_shares(
			id bigserial primary key,public_code text not null unique,owner_user_id bigint,source_type text not null,
			source_file_id bigint,title text not null,original_name text not null,status text not null,
			redaction_version integer not null,redaction_counts jsonb not null,expires_at timestamptz not null,
			deleted_at timestamptz,created_at timestamptz not null default now(),unique(source_file_id,redaction_version)
		);
		create temporary table log_share_entries(
			id bigserial primary key,log_share_id bigint not null,entry_index integer not null,original_name text not null,
			safe_display_name text not null,content_type text not null,sanitized_text text,byte_size bigint not null,
			line_count bigint not null,checksum text not null,status text not null,unique(log_share_id,entry_index)
		);
		create temporary table comment_log_bindings(
			comment_id bigint not null,attachment_file_id bigint not null,log_share_id bigint not null,
			primary key(comment_id,attachment_file_id),unique(comment_id,log_share_id)
		);
		create temporary sequence bug082_reject_outbox_once;
		create function pg_temp.reject_first_bug082_outbox() returns trigger language plpgsql as $$
		begin
			if nextval('pg_temp.bug082_reject_outbox_once')=1 then
				raise exception 'BUG-082 transient outbox failure';
			end if;
			return new;
		end $$;
		create trigger reject_first_bug082_outbox before insert on nats_outbox
			for each row execute function pg_temp.reject_first_bug082_outbox();
	`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into oss_files(id,public_id,object_key,original_name,source_original_name,content_type,size_bytes,source_size_bytes,sha256,uploader_id,source,status,scan_status)
		values
			(821,'b082file1','comments/820/latest.log','latest.log','latest.log','text/plain',27,27,$1,820,'comment','active','pending'),
			(823,'b082file2','comments/822/restart.log','restart.log','restart.log','text/plain',27,27,$1,820,'comment','active','pending')`, sha); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Load()}
	ossConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: provider.URL, PublicEndpoint: "https://public.example.test",
		Bucket: "bug082-bucket", AccessKeyID: "bug082-key", AccessKeySecret: "bug082-secret", UseCName: true, Prefix: "mcmods",
	}
	sealed, err := server.sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into comments(id) values(820);insert into comment_attachments(comment_id,attachment_file_id) values(820,821)`); err != nil {
		t.Fatal(err)
	}
	if err = enqueueCommentLogAttachmentJobsTx(ctx, tx, 820, 820, []string{"b082file1"}); err == nil || !strings.Contains(err.Error(), "BUG-082 transient outbox failure") {
		t.Fatalf("outbox failure = %v", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertBUG082Counts(t, ctx, pool, 0, 0, 0, 0)

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into comments(id) values(820);insert into comment_attachments(comment_id,attachment_file_id) values(820,821)`); err != nil {
		t.Fatal(err)
	}
	if err = enqueueCommentLogAttachmentJobsTx(ctx, tx, 820, 820, []string{"b082file1"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertBUG082Counts(t, ctx, pool, 1, 1, 1, 0)
	var jobID int64
	if err = pool.QueryRow(ctx, `select id from comment_log_attachment_jobs where comment_id=820`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	worker := NewCommentLogAttachmentWorker(server.cfg, pool, nil)
	if err = worker.processJob(ctx, jobID); err == nil {
		t.Fatal("first provider failure unexpectedly completed the comment log job")
	}
	var status, attachmentStatus string
	var attempts int
	if err = pool.QueryRow(ctx, `select job.status,job.attempts,attachment.processing_status
		from comment_log_attachment_jobs job join comment_attachments attachment
		on attachment.comment_id=job.comment_id and attachment.attachment_file_id=job.attachment_file_id where job.id=$1`, jobID).
		Scan(&status, &attempts, &attachmentStatus); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || attempts != 1 || attachmentStatus != "processing" {
		t.Fatalf("retry state = %s attempts=%d attachment=%s", status, attempts, attachmentStatus)
	}
	providerUnavailable.Store(false)
	if _, err = pool.Exec(ctx, `update comment_log_attachment_jobs set next_attempt_at=now() where id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if err = worker.processJob(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	assertBUG082Completed(t, ctx, pool, jobID, 2)
	if err = worker.processJob(ctx, jobID); err != nil {
		t.Fatalf("completed delivery was not idempotent: %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into comments(id) values(822);insert into comment_attachments(comment_id,attachment_file_id) values(822,823)`); err != nil {
		t.Fatal(err)
	}
	if err = enqueueCommentLogAttachmentJobsTx(ctx, tx, 822, 820, []string{"b082file2"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var restartJobID int64
	if err = pool.QueryRow(ctx, `update comment_log_attachment_jobs set status='processing',attempts=1,
		locked_by='crashed-worker',lease_expires_at=now()-interval '1 second' where comment_id=822 returning id`).Scan(&restartJobID); err != nil {
		t.Fatal(err)
	}
	recovered, recoveryErr := worker.recoverDueJobs(ctx)
	if recoveryErr != nil || recovered != 1 {
		t.Fatalf("restart recovery = %d/%v", recovered, recoveryErr)
	}
	assertBUG082Completed(t, ctx, pool, restartJobID, 2)
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
