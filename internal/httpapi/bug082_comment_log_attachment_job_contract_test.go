package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCommentLogAttachmentProcessingIsCommittedAsARecoverableJob(t *testing.T) {
	handlers, err := os.ReadFile("comment_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	createBody := goFunctionBody(t, string(handlers), "createComment")
	enqueueAt := strings.Index(createBody, "enqueueCommentLogAttachmentJobsTx(")
	commitAt := strings.LastIndex(createBody, "tx.Commit(")
	if enqueueAt < 0 || commitAt < 0 || enqueueAt > commitAt {
		t.Fatalf("comment log job/outbox must be written before the comment transaction commits")
	}
	if strings.Contains(createBody[commitAt:], "bindCommentLogAttachment(") {
		t.Fatal("comment creation still performs unrecoverable post-commit log attachment work")
	}

	details, err := os.ReadFile("comment_detail_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	detailSource := string(details)
	for _, required := range []string{
		"func enqueueCommentLogAttachmentJobsTx(",
		"queue.EnqueueTx(",
		`"comment_log_attachment"`,
		`"comment.log_attachment.requested"`,
	} {
		if !strings.Contains(detailSource, required) {
			t.Fatalf("comment log attachment enqueue contract is missing %q", required)
		}
	}

	worker, err := os.ReadFile("comment_log_attachment_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	workerSource := string(worker)
	for _, required := range []string{
		"type CommentLogAttachmentWorker struct",
		`SubscribeTask("comment_log_attachment"`,
		"recoverDueJobs(",
		"lease_expires_at",
		"attempts<max_attempts",
		"createFileLogShare(",
		"insert into comment_log_bindings",
	} {
		if !strings.Contains(workerSource, required) {
			t.Fatalf("comment log attachment worker contract is missing %q", required)
		}
	}

	runtimeSource, err := os.ReadFile("../app/runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runtimeSource), "NewCommentLogAttachmentWorker(cfg, db, queueClient)") ||
		!strings.Contains(string(runtimeSource), "commentLogWorker.Start(ctx)") {
		t.Fatal("application runtime does not start the comment log attachment worker")
	}
	queueSource, err := os.ReadFile("../queue/nats.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(queueSource), `Code:           "comment_log_attachment"`) {
		t.Fatal("default NATS tasks do not include comment log attachment processing")
	}
}
