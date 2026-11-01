package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/auditdata"
)

func TestAuditKnownOSSLifecycleDiagnosisCountsCandidatesWithoutRepairOrDisclosureIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	owner := f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, owner, "report.create")
	raw := []byte("Owned synthetic private report evidence")
	ticket := f.ticket(t, f.editor, "/api/v1/reports/evidence/uploads", "private-candidate-source.txt", "text/plain", "ignored-report-source", raw)
	f.put(t, ticket, raw)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/reports/evidence/uploads/complete", ticket.completion(), 201)
	var fileID int64
	var storedKey string
	if err := f.db.QueryRow(f.ctx, `select file.id,file.object_key from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where binding.upload_object_key=$1 and binding.status='committed' and file.source_hash_verified`, ticket.ObjectKey).Scan(&fileID, &storedKey); err != nil {
		t.Fatal(err)
	}
	// Model old metadata loss on an owned test copy. It is not a claim that the
	// fixed atomic upload can produce the same anomaly through normal requests.
	if _, err := f.db.Exec(f.ctx, `delete from report_evidence where object_key=$1`, storedKey); err != nil {
		t.Fatal(err)
	}
	if err := f.server.reserveUserOSSUploadQuota(f.ctx, owner, "private-expired-reservation", 17, 17, time.Now().Add(time.Hour), ossUserQuotaLimits{single: 64 << 20, daily: 256 << 20, total: 256 << 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update oss_user_upload_quota_reservations set created_at=now()-interval '2 minutes',expires_at=now()-interval '1 minute' where user_id=$1 and object_key='private-expired-reservation'`, owner); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		query string
		args  []any
	}{
		{`insert into oss_object_deletion_outbox(bucket,endpoint,region,object_key,status) values('owned','https://private-provider.invalid','owned','private-delete-key','dead')`, nil},
		{`insert into oss_rehome_jobs(mod_id,status) values($1,'dead')`, []any{f.modID}},
		{`insert into oss_multipart_sessions(owner_id,bucket,endpoint,region,object_key,upload_id,original_name,content_type,size_bytes,sha256,category,source,status,expires_at) values($1,'owned','https://private-provider.invalid','owned','private-multipart-key','never-publish-original-upload-id','private-source-name.zip','application/zip',17,repeat('a',64),'owned','owned','dead',now()+interval '1 hour')`, []any{owner}},
	} {
		if _, err := f.db.Exec(f.ctx, entry.query, entry.args...); err != nil {
			t.Fatal(err)
		}
	}

	before := auditOSSAllFacts(t, f)
	report, err := auditdata.Inspect(f.ctx, f.db, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ReadOnly {
		t.Fatal("diagnosis did not establish a read-only transaction")
	}
	for _, section := range []string{"oss_report_evidence_metadata_repair_candidates", "oss_expired_quota_reservation_candidates", "oss_dead_deletion_jobs", "oss_dead_rehome_jobs", "oss_dead_multipart_sessions"} {
		result, exists := report.Sections[section]
		if !exists || result.Count != 1 || len(result.PublicIDs) != 1 {
			t.Fatalf("candidate classification/count/window/sample wrong for %s", section)
		}
		var publicID string
		if err = json.Unmarshal(result.PublicIDs[0], &publicID); err != nil || !validCatalogPublicID(publicID) {
			t.Fatalf("candidate %s did not emit a public ID", section)
		}
	}
	// This count is explicitly uncorrelated history, never a missing-event or
	// missing-reward count. No event/reward is reconstructed by the diagnosis.
	if report.Sections["oss_legacy_files_without_exact_historical_activity_linkage"].Count != 1 {
		t.Fatal("legacy file linkage limitation was lost")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{ticket.ObjectKey, storedKey, "private-candidate-source.txt", "private-expired-reservation", "private-provider.invalid", "private-delete-key", "private-multipart-key", "never-publish-original-upload-id", "private-source-name.zip"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("diagnosis disclosed a private lifecycle field")
		}
	}
	if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only candidate diagnosis repaired, deleted or rewarded data")
	}
}
