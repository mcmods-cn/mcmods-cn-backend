package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

func TestAdminOSSTicketAndCompletionUseTheSameRootIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
	const presign = "/api/v1/admin/oss/uploads/presign"
	const complete = "/api/v1/admin/oss/uploads/complete"
	raw := []byte("archive entry\n")
	digest := sha256.Sum256(raw)
	root := ossRoot(f.cfg.Prefix)
	input := ossDirectUploadRequest{OriginalName: "document.txt", ContentType: "text/plain", Category: "reports",
		Source: "test037-prefix", SHA256: hex.EncodeToString(digest[:]), Prefix: root + "-other", SizeBytes: ossMultipartThreshold}
	f.require(t, f.editor, http.MethodPost, presign, input, http.StatusBadRequest)
	f.store.mu.Lock()
	created := len(f.store.uploads)
	f.store.mu.Unlock()
	if created != 0 {
		t.Fatal("opened remote multipart uploads for an unusable destination")
	}
	input.Prefix, input.SizeBytes = root+"/operations", int64(len(raw))
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, http.MethodPost, presign, input, http.StatusOK))
	if !strings.HasPrefix(ticket.ObjectKey, input.Prefix+"/reports/") {
		t.Fatalf("subdirectory not honored: %s", ticket.ObjectKey)
	}
	f.put(t, ticket, raw)
	result := decodeTEST022Data[struct{ ID string }](t, f.require(t, f.editor, http.MethodPost, complete, ticket.completion(), http.StatusCreated))
	if !validCatalogPublicID(result.ID) {
		t.Fatalf("no registered file from valid subdirectory: %#v", result)
	}
}
