package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAuditGR001ReportRetentionUsesSharedTerminalRawIntentIntegration(t *testing.T) {
	for _, mode := range []string{"normal", "raw-intent-rollback"} {
		t.Run(mode, func(t *testing.T) {
			f := newTEST037Fixture(t)
			raw := []byte("owned report attachment; SYNTHETIC_PRIVATE_MACHINE_PATH=/home/owned-fixture\n")
			ticket := f.ticket(t, f.editor, "/api/v1/reports/evidence/uploads", "private-evidence.txt", "text/plain", "ignored-report-scope", raw)
			expiry := auditRawRequireCapability(t, f, ticket, 600, 1)
			f.put(t, ticket, raw)
			evidence := decodeTEST022Data[struct {
				ID string `json:"id"`
			}](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/reports/evidence/uploads/complete", ticket.completion(), 201))
			var fileID int64
			var stored, binding string
			if err := f.db.QueryRow(f.ctx, `select file.id,file.object_key,binding.id::text from report_evidence evidence join oss_files file on file.object_key=evidence.object_key join oss_upload_byte_bindings binding on binding.file_id=file.id where evidence.public_id=$1 and evidence.status='temporary' and binding.status='committed'`, evidence.ID).Scan(&fileID, &stored, &binding); err != nil {
				t.Fatal(err)
			}
			// Controlled existing report-retention eligibility, like TEST048. This
			// proves the real maintenance transaction after eligibility, not that
			// a day/fourteen days elapsed. No upload capability, raw receipt time,
			// signing TTL, seven-day tool or provider bytes are changed/aged.
			if _, err := f.db.Exec(f.ctx, `update report_evidence set cleanup_after=clock_timestamp() where public_id=$1 and status='temporary'`, evidence.ID); err != nil {
				t.Fatal(err)
			}
			worker := NewMaintenanceWorker(f.server)
			if mode == "raw-intent-rollback" {
				if _, err := f.db.Exec(f.ctx, `alter table oss_object_deletion_outbox add constraint reject_owned_report_raw check(reason<>'deleted-upload-original-input') not valid`); err != nil {
					t.Fatal(err)
				}
				_, err := f.db.Exec(f.ctx, `insert into oss_object_deletion_outbox(bucket,endpoint,region,object_key,reason) values('owned-oracle','https://example.invalid','cn-owned','never-written-report-object','deleted-upload-original-input')`)
				var fault *pgconn.PgError
				if !errors.As(err, &fault) || fault.Code != "23514" || fault.ConstraintName != "reject_owned_report_raw" {
					t.Fatalf("report cleanup fault reached unrelated SQL boundary: %v", err)
				}
				before := f.facts(t)
				worker.pruneReportEvidence(f.ctx)
				f.unchanged(t, before)
				if _, err = f.db.Exec(f.ctx, `alter table oss_object_deletion_outbox drop constraint reject_owned_report_raw`); err != nil {
					t.Fatal(err)
				}
			}
			worker.pruneReportEvidence(f.ctx)
			var deleted, metadata bool
			var intents, attempts int
			var notBefore *time.Time
			if err := f.db.QueryRow(f.ctx, `select (select status='deleted' from oss_files where id=$1),(select status='deleted' and delete_attempts=1 and original_name=$4 and byte_size=$5 from report_evidence where public_id=$2),(select count(*) from oss_object_deletion_outbox where raw_upload_binding_id=$3::uuid and object_key=$6),(select min(next_attempt_at) from oss_object_deletion_outbox where raw_upload_binding_id=$3::uuid),(select coalesce(max(attempts),0) from oss_object_deletion_outbox where raw_upload_binding_id=$3::uuid)`, fileID, evidence.ID, binding, ticket.OriginalName, int64(len(raw)), ticket.ObjectKey).Scan(&deleted, &metadata, &intents, &notBefore, &attempts); err != nil || !deleted || !metadata || intents != 1 || notBefore == nil || notBefore.Before(expiry.Add(time.Second)) || attempts != 0 {
				t.Fatalf("actual report-retention terminal result lacks one typed raw intent and retained evidence: deleted%v metadata%v intents%d hasNotBefore%v attempts%d err%v", deleted, metadata, intents, notBefore != nil, attempts, err)
			}
			before := f.facts(t)
			worker.pruneReportEvidence(f.ctx)
			f.unchanged(t, before)
			NewOSSDeletionWorker(f.server.cfg, f.db).drain(f.ctx)
			client, cfg, err := f.server.ossClient(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetObject(f.ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(stored)})
			if !isOSSRawObjectAbsent(err) {
				t.Fatal("actual canonical report object was not precisely absent after deletion worker")
			}
			response, err := client.GetObject(f.ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)})
			if err != nil {
				t.Fatal("report cleanup deleted original before its real issued capability expired")
			}
			actual, err := io.ReadAll(io.LimitReader(response.Body, int64(len(raw)+1)))
			_ = response.Body.Close()
			if err != nil || !bytes.Equal(actual, raw) {
				t.Fatal("held report original bytes changed")
			}
			t.Log("Actual ordinary report upload/complete and maintenance+deletion Workers retain immutable evidence and queue exact source cleanup after actual grant; report eligibility alone controlled, no claim of elapsed retention days and no raw/TTL aging.")
		})
	}
}
