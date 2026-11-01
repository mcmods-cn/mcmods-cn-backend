package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

// Only one ordinary PUT to a freshly issued owned key occurs. This test never
// overwrites a raw object, changes its metadata, forges an old timestamp or
// exercises a previously stopped overwrite action. The expected durable intent
// may wait for the real issued capability expiry; it must not silently vanish.
func TestAuditGR001DeletedCompletedUploadHasDurableOriginalRawCleanupIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	marker := []byte("SYNTHETIC_OWNED_PRIVATE_PLAYER_AND_MACHINE_PATH")
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "owned-config.json", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(append([]byte(`{"machine_path":"/home/owned-fixture","private_marker":"`), append(marker, []byte(`"}`)...)...)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	source := archive.Bytes()
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "owned-private-retention.zip", "application/zip", "test037-retention", source)
	var issuedExpiry time.Time
	if err := f.db.QueryRow(f.ctx, `select expires_at from oss_user_upload_quota_reservations where user_id=$1 and object_key=$2`, f.userIDs[f.editor], ticket.ObjectKey).Scan(&issuedExpiry); err != nil || !issuedExpiry.After(time.Now()) {
		t.Fatal("fresh actual presign did not persist its real unexpired quota capability")
	}
	f.put(t, ticket, source)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), http.StatusCreated))
	var fileID int64
	var stored string
	if err := f.db.QueryRow(f.ctx, `select file.id,file.object_key from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where file.public_id=$1 and binding.status='committed' and binding.upload_object_key=$2 and binding.owner_id=$3`, file["id"], ticket.ObjectKey, f.userIDs[f.editor]).Scan(&fileID, &stored); err != nil || stored == ticket.ObjectKey || !isOSSSealedObjectKey(stored) {
		t.Fatal("normal completion did not adopt the exact committed sealed bytes")
	}
	deleted := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, http.StatusOK))
	if deleted["deleted"] != true {
		t.Fatal("ordinary owner deletion did not acknowledge the terminal file")
	}
	worker := NewOSSDeletionWorker(f.server.cfg, f.db)
	worker.drain(f.ctx)
	var terminal, physicalCompleted bool
	if err := f.db.QueryRow(f.ctx, `select (select status='deleted' from oss_files where id=$1),exists(select 1 from oss_object_deletion_outbox where object_key=$2 and status='completed')`, fileID, stored).Scan(&terminal, &physicalCompleted); err != nil || !terminal || !physicalCompleted {
		t.Fatalf("actual file/deletion Worker did not finish the canonical object: %v", err)
	}
	client, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetObject(f.ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(stored)})
	var absent *aliyunoss.ServiceError
	if !errors.As(err, &absent) || absent.StatusCode != http.StatusNotFound || absent.Code != "NoSuchKey" {
		t.Fatalf("stored object was not precisely absent after actual DELETE: %v", err)
	}
	retained, err := client.GetObject(f.ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)})
	if err != nil {
		t.Fatal("original raw input should remain until its still-live issued capability expires")
	}
	actual, readErr := io.ReadAll(io.LimitReader(retained.Body, int64(len(source)+1)))
	closeErr := retained.Body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, source) || !bytes.Contains(actual, marker) {
		t.Fatal("actual raw provider bytes do not match the single original owned upload")
	}
	var durable int
	var notBefore *time.Time
	if err := f.db.QueryRow(f.ctx, `select count(*),min(next_attempt_at) from oss_object_deletion_outbox where bucket=$1 and endpoint=$2 and object_key=$3`, cfg.Bucket, cfg.Endpoint, ticket.ObjectKey).Scan(&durable, &notBefore); err != nil {
		t.Fatal(err)
	}
	t.Logf("normal201/ownerDELETE/actualDeletionWorker: terminalFile=true canonicalObjectAbsent=true rawOriginalPIIStillPresent=true rawCleanupIntents=%d originalSHA256=%x realCapabilityStillLive=%v; no overwrite or timestamp mutation", durable, sha256.Sum256(actual), issuedExpiry.After(time.Now()))
	if durable != 1 || notBefore == nil || notBefore.Before(issuedExpiry) {
		t.Fatalf("deleted file retained its original private raw input without one durable cleanup after its actual issued expiry: intents=%d hasNotBefore=%v", durable, notBefore != nil)
	}
}
