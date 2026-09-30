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
	ossHandlers := string(ossSource)
	for _, required := range []string{
		"createPendingBlueprint(r.Context(), currentClaims(r).Subject, req.OriginalName, uploadExpiresAt)",
		"discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, blueprintID",
		"discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, 0, req.ObjectKey)",
	} {
		if !strings.Contains(ossHandlers, required) {
			t.Fatalf("blueprint upload lifecycle is missing %q", required)
		}
	}
	if count := strings.Count(ossHandlers, "discardPendingBlueprintUpload("); count < 6 {
		t.Fatalf("blueprint discard coverage = %d call sites, want quota/object-key/multipart/presign/abort coverage", count)
	}

	blueprintSource, err := os.ReadFile("blueprint_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	blueprintHandlers := string(blueprintSource)
	for _, required := range []string{
		"upload_expires_at)",
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
