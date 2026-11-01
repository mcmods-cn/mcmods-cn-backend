package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAuditGR001TerminalRawIntentFailureRollsBackFileAndActivityIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("ordinary owned atomic terminal original")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "atomic-terminal.zip", "application/zip", "test037-retention", raw)
	f.put(t, ticket, raw)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
	if _, err := f.db.Exec(f.ctx, `alter table oss_object_deletion_outbox add constraint reject_owned_original_terminal check(reason<>'deleted-upload-original-input') not valid`); err != nil {
		t.Fatal(err)
	}
	// Establish the precise intended failure, rather than accepting arbitrary
	// SQL errors as evidence for rollback at the new raw-intent boundary.
	_, err := f.db.Exec(f.ctx, `insert into oss_object_deletion_outbox(bucket,endpoint,region,object_key,reason) values('owned-oracle','https://example.invalid','cn-owned','never-created-provider-key','deleted-upload-original-input')`)
	var failure *pgconn.PgError
	if !errors.As(err, &failure) || failure.Code != "23514" || failure.ConstraintName != "reject_owned_original_terminal" {
		t.Fatalf("fault oracle did not establish precise raw-intent constraint: %v", err)
	}
	before := f.facts(t)
	response := auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 500)
	if !strings.Contains(string(response), "创建 OSS 文件删除任务失败") {
		t.Fatal("terminal raw-intent failure reached unrelated HTTP boundary")
	}
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, `alter table oss_object_deletion_outbox drop constraint reject_owned_original_terminal`); err != nil {
		t.Fatal(err)
	}
	auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	var terminal bool
	var count int
	if err = f.db.QueryRow(f.ctx, `select (select bool_and(status='deleted') from oss_files),(select count(*) from oss_object_deletion_outbox where raw_upload_binding_id is not null and object_key=$1)`, ticket.ObjectKey).Scan(&terminal, &count); err != nil || !terminal || count != 1 {
		t.Fatal("normal recovery did not commit exactly one terminal raw intent with deleted file")
	}
}

func TestAuditGR001SyntheticLegacyMissingCapabilityHoldsWithoutFailureBudgetIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("owned synthetic legacy grant-unknown original")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "legacy-unknown.zip", "application/zip", "test037-retention", raw)
	f.put(t, ticket, raw)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	// Represent an older deployment's genuinely unknown authorization row.
	// This is synthetic legacy-data fault setup, not a normal issuer or a
	// claim that the real unexpired capability has become expired.
	if _, err := f.db.Exec(f.ctx, `delete from oss_upload_capabilities where bucket=$1 and endpoint=$2 and object_key=$3`, f.cfg.Bucket, f.cfg.Endpoint, ticket.ObjectKey); err != nil {
		t.Fatal(err)
	}
	auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	worker := NewOSSDeletionWorker(f.server.cfg, f.db)
	worker.drain(f.ctx)
	var status, reason, diagnostic string
	var attempts int
	if err := f.db.QueryRow(f.ctx, `select status,attempts,raw_hold_reason,last_error from oss_object_deletion_outbox where object_key=$1 and raw_upload_binding_id is not null and failure_class=''`, ticket.ObjectKey).Scan(&status, &attempts, &reason, &diagnostic); err != nil || status != "pending" || attempts != 0 || reason != "raw_capability_unknown" {
		t.Fatalf("unknown historical authority was treated as expiry or provider failure: status%s attempts%d reason%s diagnostic%q err=%v", status, attempts, reason, diagnostic, err)
	}
	client, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.GetObject(f.ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)})
	if err != nil {
		t.Fatal("unknown legacy authorization caused physical raw deletion")
	}
	actual, err := io.ReadAll(io.LimitReader(response.Body, int64(len(raw)+1)))
	_ = response.Body.Close()
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("held original bytes changed")
	}
	report, err := InspectOSSRawRetirements(f.ctx, f.db, []string{file["id"].(string)})
	if err != nil || report.Counts["HOLD_CAPABILITY_UNKNOWN"] != 1 || report.ProviderBytesObserved {
		t.Fatalf("read-only legacy-capability diagnosis did not retain its exact HOLD: counts%v err%v", report.Counts, err)
	}
}
