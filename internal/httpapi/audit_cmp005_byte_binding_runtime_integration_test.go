package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"os"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func TestAuditCMP005NormalUploadAdoptsVerifiedServerBytesAndReplaysOriginalIdentityIntegration(t *testing.T) {
	for _, replayKind := range []string{"original-upload", "stored-result"} {
		t.Run(replayKind, func(t *testing.T) {
			f := newTEST037Fixture(t)
			raw := []byte("ordinary owned byte binding " + replayKind)
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "bound.zip", "application/zip", "test037-binding", raw)
			f.put(t, ticket, raw)
			file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
			storedKey := file["objectKey"].(string)
			if storedKey == ticket.ObjectKey || !isOSSSealedObjectKey(storedKey) || file["category"] != ticket.Category || file["sourceSha256"] != ticket.SHA256 || file["storedSha256"] != ticket.SHA256 {
				t.Fatal("ordinary completion did not separate logical identity from server-owned verified bytes")
			}
			if got := f.get(t, file["accessUrl"].(string), 200); !bytes.Equal(got, raw) {
				t.Fatal("actual adopted bytes changed")
			}
			var n int
			var upload, stored, sourceSHA, storedSHA, status string
			var sourceBytes, storedBytes int64
			if err := f.db.QueryRow(f.ctx, `select binding.upload_object_key,binding.stored_object_key,binding.source_verified_sha256,binding.stored_verified_sha256,binding.source_verified_size_bytes,binding.stored_verified_size_bytes,binding.status from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id where file.public_id=$1`, file["id"]).Scan(&upload, &stored, &sourceSHA, &storedSHA, &sourceBytes, &storedBytes, &status); err != nil {
				t.Fatal(err)
			}
			if upload != ticket.ObjectKey || stored != storedKey || sourceSHA != ticket.SHA256 || storedSHA != ticket.SHA256 || sourceBytes != int64(len(raw)) || storedBytes != sourceBytes || status != "committed" {
				t.Fatal("committed receipt disagrees with actual file/source proof")
			}
			before := f.facts(t)
			replay := ticket.completion()
			if replayKind == "stored-result" {
				replay["objectKey"] = storedKey
			}
			repeated := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", replay, 200))
			if repeated["id"] != file["id"] || repeated["objectKey"] != storedKey {
				t.Fatal("completion replay changed file identity")
			}
			f.unchanged(t, before)
			if err := f.db.QueryRow(f.ctx, `select count(*) from oss_upload_byte_bindings`).Scan(&n); err != nil || n != 1 {
				t.Fatalf("replay allocated new attempt: %d %v", n, err)
			}
			var active, reserved int64
			if err := f.db.QueryRow(f.ctx, `select active_stored_bytes,reserved_stored_bytes from oss_user_quota_usage where user_id=$1`, f.userIDs[f.editor]).Scan(&active, &reserved); err != nil || active != int64(len(raw)) || reserved != 0 {
				t.Fatalf("quota settlement %d/%d err=%v", active, reserved, err)
			}
			t.Log("Normal single PUT -> server-only copy -> actual sealed GET/hash -> one committed file/receipt; original and stored-result replay stable; no second source PUT performed")
		})
	}
}

func TestAuditCMP005SingleIncorrectDeclaredHashIsRejectedBeforeAnyBindingIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("one ordinary declared upload")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "bad-hash.zip", "application/zip", "test037-binding", raw)
	bad := bytes.Repeat([]byte{'x'}, len(raw))
	f.put(t, ticket, bad)
	before := f.facts(t)
	response := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 422)
	if !strings.Contains(string(response), "source bytes do not match") {
		t.Fatal("single incorrect hash did not hit actual source-byte validation")
	}
	f.unchanged(t, before)
	var bindings int
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_upload_byte_bindings`).Scan(&bindings); err != nil || bindings != 0 {
		t.Fatalf("wrong source created binding %d %v", bindings, err)
	}
}

func TestAuditCMP005CopyFailureCompensatesOnlyOwnCandidateAndKeepsOriginalRetryIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("ordinary copy outage source")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "copy-outage.zip", "application/zip", "test037-binding", raw)
	f.put(t, ticket, raw)
	f.store.failCopy.Store(true)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 502)
	var files, activityCount int
	var status, candidate string
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from oss_files),(select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3),status,sealed_source_key from oss_upload_byte_bindings`, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&files, &activityCount, &status, &candidate); err != nil {
		t.Fatal(err)
	}
	// The successful presign already contributed one existing Upload event.
	if files != 0 || activityCount != 1 || status != "cleanup_pending" || candidate == ticket.ObjectKey {
		t.Fatalf("copy failure facts files=%d events=%d status=%s", files, activityCount, status)
	}
	var cleanup int
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_object_deletion_outbox where object_key=$1 and status='pending'`, candidate).Scan(&cleanup); err != nil || cleanup != 1 {
		t.Fatalf("candidate cleanup %d %v", cleanup, err)
	}
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_object_deletion_outbox where object_key=$1`, ticket.ObjectKey).Scan(&cleanup); err != nil || cleanup != 0 {
		t.Fatal("failure scheduled deletion of retryable client source")
	}
	f.store.mu.Lock()
	input, exists := f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if !exists || !bytes.Equal(input.data, raw) {
		t.Fatal("copy failure discarded original upload")
	}
	f.store.failCopy.Store(false)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	worker := NewOSSDeletionWorker(f.server.cfg, f.db)
	worker.drain(f.ctx)
	if got := f.get(t, file["accessUrl"].(string), 200); !bytes.Equal(got, raw) {
		t.Fatal("candidate cleanup removed adopted retry bytes")
	}
}

func TestAuditCMP005ReservedUploadNamespaceCannotIssueClientWriteCapabilityIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
	before := f.facts(t)
	raw := []byte("ordinary administrator upload")
	body := map[string]any{"originalName": "reserved.zip", "sizeBytes": len(raw), "sha256": sha256Hex(raw), "contentType": "application/zip", "prefix": "mcmods/" + ossSealedUploadComponent + "/forbidden", "category": "admin", "source": "test037-binding"}
	result := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/admin/oss/uploads/presign", body, 400)
	if !strings.Contains(string(result), "reserved OSS upload namespace") {
		t.Fatal("request did not reach reserved write-capability boundary")
	}
	f.unchanged(t, before)
}

func TestAuditCMP005ExpiredUnadoptedReceiptQueuesExactObjectsAndCannotCommitIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("ordinary unfinished binding")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "expired.zip", "application/zip", "test037-binding", raw)
	f.put(t, ticket, raw)
	client, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	head, err := client.HeadObject(f.ctx, &aliyunoss.HeadObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)})
	if err != nil {
		t.Fatal(err)
	}
	var req ossCompleteUploadRequest
	encoded, _ := json.Marshal(ticket.completion())
	if err = json.Unmarshal(encoded, &req); err != nil {
		t.Fatal(err)
	}
	b, err := f.server.sealOSSUploadedSource(f.ctx, client, cfg, req, f.userIDs[f.editor], head)
	if err != nil {
		t.Fatal(err)
	}
	b.stop()
	defer f.server.discardOSSByteBinding(b)
	if _, err = f.db.Exec(f.ctx, `update oss_upload_byte_bindings set lease_expires_at=clock_timestamp()-interval '1 second' where id=$1::uuid`, b.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.server.recoverOSSByteBindingAttempt(f.ctx)
	if err != nil || !recovered {
		t.Fatalf("expired receipt recovery=%v %v", recovered, err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = lockOSSByteBindingTx(f.ctx, tx, b)
	_ = tx.Rollback(f.ctx)
	if !errors.Is(err, errOSSByteBindingLeaseLost) {
		t.Fatalf("late adopter accepted or wrong error: %v", err)
	}
	var status string
	var cleanup int
	if err = f.db.QueryRow(f.ctx, `select status,(select count(*) from oss_object_deletion_outbox where object_key=$2 and status='pending') from oss_upload_byte_bindings where id=$1::uuid`, b.ID, b.SourceKey).Scan(&status, &cleanup); err != nil || status != "cleanup_pending" || cleanup != 1 {
		t.Fatalf("expired receipt %s cleanup=%d err=%v", status, cleanup, err)
	}
	if _, err = f.db.Exec(f.ctx, `select 1`); err != nil {
		t.Fatal(err)
	}
	worker := NewOSSDeletionWorker(f.server.cfg, f.db)
	worker.drain(f.ctx)
	f.store.mu.Lock()
	_, candidateExists := f.store.objects[b.SourceKey]
	input, inputExists := f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if candidateExists || !inputExists || !bytes.Equal(input.data, raw) {
		t.Fatal("expired receipt cleanup lost its exact source/candidate scope")
	}

}

func TestAuditCMP005ConvertedReceiptKeepsPrivateSourceUntilFileDeletionIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	source, err := os.ReadFile("testdata/audit-20261024/oss-atomic-source.png")
	if err != nil {
		t.Fatal(err)
	}
	converted, err := os.ReadFile("testdata/audit-20261024/oss-atomic-source.webp")
	if err != nil {
		t.Fatal(err)
	}
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "source-preserved.png", "image/png", "markdown", source)
	f.put(t, ticket, source)
	auditOSSInstallBindingWebPConversion(t, f, ticket, converted)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	var sealedSource, stored string
	if err = f.db.QueryRow(f.ctx, `select sealed_source_key,stored_object_key from oss_upload_byte_bindings where file_id=(select id from oss_files where public_id=$1) and status='committed'`, file["id"]).Scan(&sealedSource, &stored); err != nil {
		t.Fatal(err)
	}
	worker := NewOSSDeletionWorker(f.server.cfg, f.db)
	worker.drain(f.ctx)
	f.store.mu.Lock()
	input, inputPresent := f.store.objects[sealedSource]
	output, outputPresent := f.store.objects[stored]
	f.store.mu.Unlock()
	if !inputPresent || !outputPresent || !bytes.Equal(input.data, source) || !bytes.Equal(output.data, converted) {
		t.Fatal("committed converted receipt lost its private immutable source or adopted bytes during ordinary cleanup")
	}
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/files/presign", map[string]any{"objectKey": sealedSource}, 403)
	if _, err = f.db.Exec(f.ctx, `update oss_files set source_hash_verified=false where public_id=$1`, file["id"]); err != nil {
		t.Fatal(err)
	}
	repeated := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	if repeated["id"] != file["id"] || repeated["sourceSha256"] != sha256Hex(source) || repeated["storedSha256"] != sha256Hex(converted) {
		t.Fatal("converted receipt reverify changed identity or conflated actual source/stored hash")
	}
	deleted := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200))
	if deleted["deleted"] != true {
		t.Fatal("file delete did not acknowledge its actual tombstone")
	}
	worker.drain(f.ctx)
	f.store.mu.Lock()
	_, inputPresent = f.store.objects[sealedSource]
	_, outputPresent = f.store.objects[stored]
	f.store.mu.Unlock()
	if inputPresent || outputPresent {
		t.Fatal("deleted file retained its exclusively owned sealed source/final objects")
	}
}

func TestAuditCMP005CleanupReceiptWaitsForDeletionAndVerifiesAbsenceIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("ordinary unadopted candidate cleanup")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "cleanup.zip", "application/zip", "test037-binding", raw)
	f.put(t, ticket, raw)
	client, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	head, err := client.HeadObject(f.ctx, &aliyunoss.HeadObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)})
	if err != nil {
		t.Fatal(err)
	}
	var req ossCompleteUploadRequest
	encoded, _ := json.Marshal(ticket.completion())
	if err = json.Unmarshal(encoded, &req); err != nil {
		t.Fatal(err)
	}
	candidate, err := f.server.sealOSSUploadedSource(f.ctx, client, cfg, req, f.userIDs[f.editor], head)
	if err != nil {
		t.Fatal(err)
	}
	f.server.discardOSSByteBinding(candidate)
	makeDue := func() {
		t.Helper()
		if _, e := f.db.Exec(f.ctx, `update oss_upload_byte_bindings set lease_expires_at=clock_timestamp()-interval '1 second' where id=$1::uuid and status in ('cleanup_pending','discarded')`, candidate.ID); e != nil {
			t.Fatal(e)
		}
	}
	state := func() string {
		t.Helper()
		var value string
		if e := f.db.QueryRow(f.ctx, `select status from oss_upload_byte_bindings where id=$1::uuid`, candidate.ID).Scan(&value); e != nil {
			t.Fatal(e)
		}
		return value
	}
	makeDue()
	if more, e := f.server.reconcileOSSByteBindingCleanup(f.ctx); e != nil || !more {
		t.Fatalf("pending deletion reconciliation %v %v", more, e)
	}
	if state() != "cleanup_pending" {
		t.Fatal("receipt declared physical cleanup before deletion completed")
	}
	worker := NewOSSDeletionWorker(f.server.cfg, f.db)
	worker.drain(f.ctx)
	makeDue()
	if more, e := f.server.reconcileOSSByteBindingCleanup(f.ctx); e != nil || !more {
		t.Fatalf("actual absent candidate reconciliation %v %v", more, e)
	}
	if state() != "discarded" {
		t.Fatal("completed deletion plus actual provider HEAD absence did not reach discarded")
	}
	var scheduled bool
	if err = f.db.QueryRow(f.ctx, `select lease_expires_at>clock_timestamp()+interval '59 minutes' from oss_upload_byte_bindings where id=$1::uuid`, candidate.ID).Scan(&scheduled); err != nil || !scheduled {
		t.Fatal("discarded receipt lost indexed periodic observation schedule")
	}
	makeDue()
	if more, e := f.server.reconcileOSSByteBindingCleanup(f.ctx); e != nil || !more {
		t.Fatalf("discarded absence recheck %v %v", more, e)
	}
	if state() != "discarded" {
		t.Fatal("absence recheck changed discarded observation into adoption")
	}
	f.store.mu.Lock()
	input, present := f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if !present || !bytes.Equal(input.data, raw) {
		t.Fatal("receipt cleanup removed the retryable original input")
	}
	t.Log("Own candidate deletion intent completion followed by actual provider HEAD absence; discarded receipts retain future indexed recheck. No late provider write or client overwrite reproduction performed")
}

func TestAuditCMP005DatabaseFailureIsNotAnInvalidUploadOrProviderErrorIntegration(t *testing.T) {
	for _, kind := range []string{"incoming", "sealed-result"} {
		t.Run(kind, func(t *testing.T) {
			f := newTEST037Fixture(t)
			raw := []byte("ordinary source for exact DB failure " + kind)
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "db-boundary.zip", "application/zip", "test037-binding", raw)
			f.put(t, ticket, raw)
			body := ticket.completion()
			column := "stored_verified_size_bytes"
			if kind == "sealed-result" {
				completed := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", body, 201))
				body["objectKey"] = completed["objectKey"]
				column = "stored_verified_sha256"
			}
			if _, err := f.db.Exec(f.ctx, `alter table oss_upload_byte_bindings rename column `+column+` to audit_unavailable_column`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := f.db.Exec(f.ctx, `alter table oss_upload_byte_bindings rename column audit_unavailable_column to `+column); err != nil {
					t.Error(err)
				}
			})
			var ignored string
			err := f.db.QueryRow(f.ctx, `select `+column+`::text from oss_upload_byte_bindings limit 1`).Scan(&ignored)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "42703" {
				t.Fatalf("fixture did not establish specific unavailable-column42703: %v", err)
			}
			before := f.facts(t)
			problem := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", body, 500)
			var envelope struct {
				Code string `json:"code"`
			}
			if json.Unmarshal(problem, &envelope) != nil || envelope.Code != "HTTP_500" {
				t.Fatal("real DB error was reclassified as invalid client identity or remote provider failure")
			}
			f.unchanged(t, before)
		})
	}
}
