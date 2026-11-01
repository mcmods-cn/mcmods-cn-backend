package httpapi

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"mcmods-cn-backend/internal/queue"
)

// This uses ordinary HTTP upload/job creation, real PostgreSQL, controlled OSS
// bytes and the actual import/notification business handlers. Calling handlers
// directly proves these transactional boundaries, not NATS delivery.
func TestAuditCMP003ImportWorkerNotificationIntentSharesTerminalTransactionIntegration(t *testing.T) {
	for _, source := range []string{"mcmods_exporter", iconRendererImportSource} {
		t.Run(source, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			var file string
			var job modExportJobResponse
			if source == "mcmods_exporter" {
				file = f.upload(t, auditFixGRZip(t, nil))
				job = f.job(t, file)
			} else {
				file = auditActivityCatalogUpload(t, f)
				job = decodeTEST022Data[modExportJobResponse](t, f.require(t, f.editor, "POST", "/api/v1/mods/audit-gr-"+f.modCode+"/catalog-imports", map[string]any{"ossFileId": file, "targetVersionPublicId": f.versionPublic, "source": source}, 202))
			}
			if _, err := f.db.Exec(f.ctx, `create function audit_cmp003_import_notify_fault() returns trigger language plpgsql as $$
    begin
     if new.payload->>'templateKey'='mod_import_success' then
      raise exception 'AUDIT_CMP003_IMPORT_NOTIFICATION_INTENT_UNAVAILABLE' using errcode='XX001',constraint='audit_cmp003_import_notification_intent';
     end if;
     return new;
    end $$;
    create trigger audit_cmp003_import_notify_fault before insert on nats_outbox for each row execute function audit_cmp003_import_notify_fault()`); err != nil {
				t.Fatal(err)
			}
			worker := &ModExportWorker{server: f.server}
			raw, err := json.Marshal(modExportJobMessage{JobID: job.ID})
			if err != nil {
				t.Fatal(err)
			}
			workerErr := worker.handle(f.ctx, raw)
			var status string
			var successIntents, activeArtifacts int64
			if err = f.db.QueryRow(f.ctx, `select j.status,
    (select count(*) from nats_outbox where payload->'data'->>'jobId'=j.id and payload->>'templateKey'='mod_import_success'),
    (select count(*) from catalog_import_job_artifacts where job_id=j.id and status='active')
    from catalog_import_jobs j where j.id=$1`, job.ID).Scan(&status, &successIntents, &activeArtifacts); err != nil {
				t.Fatal(err)
			}
			if workerErr == nil {
				t.Fatalf("exact notification intent fault was acknowledged: job=%s success_intents=%d active_artifacts=%d; terminal success cannot commit without its durable notification intent", status, successIntents, activeArtifacts)
			}
			var pgerr *pgconn.PgError
			if !errors.As(workerErr, &pgerr) || pgerr.Code != "XX001" || pgerr.ConstraintName != "audit_cmp003_import_notification_intent" {
				t.Fatalf("unrelated failure did not prove notification boundary: %v", workerErr)
			}
			if status == "ready" || status == "partial" || successIntents != 0 || activeArtifacts != 0 {
				t.Fatalf("notification fault committed success facts: job=%s intents=%d artifacts=%d", status, successIntents, activeArtifacts)
			}
			if _, err = f.db.Exec(f.ctx, `drop trigger audit_cmp003_import_notify_fault on nats_outbox;drop function audit_cmp003_import_notify_fault()`); err != nil {
				t.Fatal(err)
			}
			retried, retryErr := f.server.resetModExportJobForRetry(f.ctx, job.ID, f.modID, f.userIDs[f.editor])
			if retryErr != nil || !retried {
				t.Fatalf("creator-owned internal retry failed: %v/%v", retried, retryErr)
			}
			if err = worker.handle(f.ctx, raw); err != nil {
				t.Fatalf("actual import recovery failed: %v", err)
			}
			if err = worker.handle(f.ctx, raw); err != nil {
				t.Fatalf("duplicate completed delivery failed: %v", err)
			}
			if n := f.number(t, `select count(*) from nats_outbox where payload->'data'->>'jobId'=$1 and payload->>'templateKey'='mod_import_success'`, job.ID); n != 1 {
				t.Fatalf("success notification intent count=%d want1", n)
			}
			var eventID string
			var notificationRaw []byte
			if err = f.db.QueryRow(f.ctx, `select event_id,payload from nats_outbox where payload->'data'->>'jobId'=$1 and payload->>'templateKey'='mod_import_success'`, job.ID).Scan(&eventID, &notificationRaw); err != nil {
				t.Fatal(err)
			}
			notifications := &NotificationWorker{db: f.db, cache: f.server.cache}
			delivery := queue.WithEventID(f.ctx, eventID)
			for range 2 {
				if err = notifications.handleEvent(delivery, notificationRaw); err != nil {
					t.Fatal(err)
				}
			}
			if n := f.number(t, `select count(*) from notifications where source_event_id=$1 and recipient_id=$2`, eventID, f.userIDs[f.editor]); n != 1 {
				t.Fatalf("actual notification consumer duplicate count=%d want1", n)
			}
			t.Logf("CMP003_IMPORT_NOTIFY source=%s exactXX001 rollback; recovered terminal with1durableSuccessIntent; duplicate actual import+notification handlers preserve1notification; controlledOSS, noNATSTransportClaim", source)
		})
	}
}
