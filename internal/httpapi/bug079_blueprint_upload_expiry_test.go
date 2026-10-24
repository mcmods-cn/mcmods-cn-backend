package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestBlueprintUploadFailureAndAbortPathsDiscardPendingSubjects(t *testing.T) {
	ossSource, err := os.ReadFile("oss_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	ticketSource, err := os.ReadFile("oss_upload_ticket.go")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(ossSource) + string(ticketSource)
	ticket := goFunctionBody(t, combined, "persistOSSUploadTicket")
	for _, required := range []string{"createPendingBlueprintTx", "original_object_key=$2", "reserveUserOSSUploadQuotaTx", "registerOSSMultipartSessionTx", "enqueueRequestActivityTx(r, tx)", "defer tx.Rollback(ctx)", "tx.Commit(ctx)"} {
		if !strings.Contains(ticket, required) {
			t.Fatalf("pending blueprint/ticket rollback contract is missing %q", required)
		}
	}
	issuer := goFunctionBody(t, combined, "issueOSSUploadTicket")
	if strings.Contains(issuer, "createPendingBlueprint(") || strings.Contains(issuer, "reserveUserOSSUploadQuota(") {
		t.Fatal("remote ticket issuer commits partial blueprint/quota facts before its final transaction")
	}
	// A failed ticket never persisted a subject to discard; the original abort
	// entry must still clean an already issued pending upload.
	if !strings.Contains(combined, "discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, 0, req.ObjectKey)") {
		t.Fatal("issued pending blueprint abort cleanup disappeared")
	}

	blueprintSource, err := os.ReadFile("blueprint_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	completionSource, err := os.ReadFile("oss_upload_completion_helpers.go")
	if err != nil {
		t.Fatal(err)
	}
	blueprintHandlers := string(blueprintSource) + string(completionSource)
	for _, required := range []string{
		"insert into blueprints(owner_id,title,source_format,upload_expires_at,review_status,public_id)",
		"values($1,$2,$3,$4,'pending',$5)",
		"delete from content_localizations where subject_type='blueprint'",
		"delete from content_subjects where subject_type='blueprint'",
		"upload_expires_at=null",
	} {
		if !strings.Contains(blueprintHandlers, required) {
			t.Fatalf("blueprint upload subject lifecycle is missing %q", required)
		}
	}

	workerSource, err := os.ReadFile("maintenance_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	worker := string(workerSource)
	for _, required := range []string{
		"worker.pruneBlueprintUploads,",
		"prune(pruneCtx)",
		"context.WithTimeout(ctx, timeout)",
		"for update skip locked limit $1",
		"deletePendingBlueprintUploadRowsTx",
	} {
		if !strings.Contains(worker, required) {
			t.Fatalf("blueprint upload maintenance lifecycle is missing %q", required)
		}
	}
}
