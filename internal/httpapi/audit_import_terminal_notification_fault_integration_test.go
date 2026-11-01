package httpapi

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"mcmods-cn-backend/internal/queue"
)

func auditImportTerminalFacts(t *testing.T, f auditFixGRFixture) map[string]string {
	t.Helper()
	facts := auditImportActivityFacts(t, f.test013Fixture)
	var rows string
	if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(value) order by id),'[]'::jsonb)::text from catalog_import_job_logs value`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	facts["catalog_import_job_logs"] = rows
	return facts
}

func TestAuditCMP003FailureAndStalledNotificationIntentsAreAtomicIntegration(t *testing.T) {
	for _, mode := range []string{"worker-failure", "stalled-http"} {
		t.Run(mode, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			job := f.job(t, f.upload(t, auditFixGRZip(t, nil)))
			runToken, err := f.server.claimModExportJob(f.ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "stalled-http" {
				if _, err = f.db.Exec(f.ctx, `update catalog_import_jobs set heartbeat_at=now()-interval '6 minutes' where id=$1`, job.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = f.db.Exec(f.ctx, `create function audit_terminal_notify_fault() returns trigger language plpgsql as $$ begin if new.payload->>'templateKey'='mod_import_failure' then raise exception 'AUDIT_TERMINAL_NOTIFICATION_UNAVAILABLE' using errcode='XX001',constraint='audit_terminal_notification_intent';end if;return new;end $$;create trigger audit_terminal_notify_fault before insert on nats_outbox for each row execute function audit_terminal_notify_fault()`); err != nil {
				t.Fatal(err)
			}
			before := auditImportTerminalFacts(t, f)
			failure := errors.New("Synthetic controlled import failure")
			if mode == "worker-failure" {
				err = f.server.failModExportJob(job.ID, runToken, "import_failed", failure)
			} else {
				var updated bool
				updated, err = f.server.markStalledModExportJob(f.ctx, job.ID, f.modID)
				if updated {
					t.Fatal("stalled fact committed while notification intent unavailable")
				}
			}
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "XX001" || pgerr.ConstraintName != "audit_terminal_notification_intent" {
				t.Fatalf("terminal path reached unrelated failure: %v", err)
			}
			auditFixRequireProjectFactsUnchanged(t, before, auditImportTerminalFacts(t, f))
			path := "/api/v1/mods/audit-gr-" + f.modCode + "/export-imports/" + job.ID
			if mode == "stalled-http" {
				raw := f.require(t, f.editor, "GET", path, nil, 500)
				if !strings.Contains(string(raw), "Failed to inspect the import job") {
					t.Fatal("HTTP error did not reach exact stalled persistence boundary")
				}
				auditFixRequireProjectFactsUnchanged(t, before, auditImportTerminalFacts(t, f))
			}
			if _, err = f.db.Exec(f.ctx, `drop trigger audit_terminal_notify_fault on nats_outbox;drop function audit_terminal_notify_fault()`); err != nil {
				t.Fatal(err)
			}
			if mode == "worker-failure" {
				if err = f.server.failModExportJob(job.ID, runToken, "import_failed", failure); err != nil {
					t.Fatal(err)
				}
				if err = f.server.failModExportJob(job.ID, runToken, "import_failed", failure); err != nil {
					t.Fatal(err)
				}
			} else {
				f.require(t, f.editor, "GET", path, nil, 200)
				f.require(t, f.editor, "GET", path, nil, 200)
			}
			if n := f.number(t, `select count(*) from catalog_import_jobs where id=$1 and status='failed' and run_token=''`, job.ID); n != 1 {
				t.Fatal("recovery did not persist exact terminal failure")
			}
			if n := f.number(t, `select count(*) from nats_outbox where payload->'data'->>'jobId'=$1 and payload->>'templateKey'='mod_import_failure'`, job.ID); n != 1 {
				t.Fatalf("failure notification intent count=%d", n)
			}
			var eventID string
			var raw []byte
			if err = f.db.QueryRow(f.ctx, `select event_id,payload from nats_outbox where payload->'data'->>'jobId'=$1 and payload->>'templateKey'='mod_import_failure'`, job.ID).Scan(&eventID, &raw); err != nil {
				t.Fatal(err)
			}
			consumer := &NotificationWorker{db: f.db, cache: f.server.cache}
			for range 2 {
				if err = consumer.handleEvent(queue.WithEventID(f.ctx, eventID), raw); err != nil {
					t.Fatal(err)
				}
			}
			if n := f.number(t, `select count(*) from notifications where source_event_id=$1 and recipient_id=$2`, eventID, f.userIDs[f.editor]); n != 1 {
				t.Fatal("failure consumer redelivery duplicated notification")
			}
			t.Logf("TERMINAL_NOTIFY mode=%s exactXX001 zero terminal/log/outbox writes; recovery and duplicate source/consumer preserve1failureIntent+1notification; noNATSTransport", mode)
		})
	}
}
