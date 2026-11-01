package httpapi

import (
	"fmt"
	"testing"
)

// A synthetic database fault moves only this owned action's expiry during its
// own file update. This tests the final lease fence, not normal reachability or
// an upload overwrite, and uses no timing sleeps or remote object mutations.
func TestAuditCMP005MaintenanceTerminalPromotionRejectsLeaseLostInsideFileUpdateIntegration(t *testing.T) {
	for _, kind := range []string{"legacy", "typed_endpoint"} {
		t.Run(kind, func(t *testing.T) {
			var f test037Fixture
			var e OSSByteMaintenanceEntry
			if kind == "legacy" {
				f = newTEST037Fixture(t)
				id, _, _, source, stored := auditOSSByteLegacyFile(t, f, false)
				e = OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: id, ExpectedDisplayEndpoint: f.cfg.displayEndpoint(), ExpectedSourceSHA: sha256Hex(source), ExpectedStoredSHA: sha256Hex(stored), ExpectedSourceSize: int64(len(source)), ExpectedStoredSize: int64(len(stored))}
			} else {
				var binding string
				var ticket test037Ticket
				f, ticket, binding, _ = auditOSSByteRawFixture(t)
				if err := f.db.QueryRow(f.ctx, `update oss_upload_byte_bindings set file_endpoint='' where id=$1::uuid returning (select public_id from oss_files where id=file_id)`, binding).Scan(&e.FilePublicID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Exec(f.ctx, `update oss_files set source_hash_verified=false where public_id=$1`, e.FilePublicID); err != nil {
					t.Fatal(err)
				}
				e.ActionID, e.Operation = newExportID(), "promote"
				e.ExpectedDisplayEndpoint, e.ExpectedSourceSHA, e.ExpectedStoredSHA = f.cfg.displayEndpoint(), ticket.SHA256, ticket.SHA256
				e.ExpectedSourceSize, e.ExpectedStoredSize = ticket.SizeBytes, ticket.SizeBytes
			}
			var before string
			if err := f.db.QueryRow(f.ctx, `select to_jsonb(file)::text from oss_files file where public_id=$1`, e.FilePublicID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			sql := fmt.Sprintf(`create function audit_owned_expire_maintenance_action() returns trigger language plpgsql as $$begin
    update public.oss_upload_maintenance_actions set lease_expires_at=clock_timestamp()-interval '1 second' where id='%s'::uuid and state='preparing';
    return new; end$$;
    create trigger audit_owned_expire_maintenance before update of source_hash_verified on oss_files for each row execute function audit_owned_expire_maintenance_action()`, e.ActionID)
			if _, err := f.db.Exec(f.ctx, sql); err != nil {
				t.Fatal(err)
			}
			plan := auditOSSByteMaintenancePlan(t, f, e)
			result := auditOSSByteMaintenanceRun(t, f, f.db, plan, true)
			if result.Status != "STATE_CONFLICT" {
				t.Fatalf("expired action owner committed terminal %s result: %+v", kind, result)
			}
			var after, state string
			if err := f.db.QueryRow(f.ctx, `select to_jsonb(file)::text,(select state from oss_upload_maintenance_actions where id=$2::uuid) from oss_files file where public_id=$1`, e.FilePublicID, e.ActionID).Scan(&after, &state); err != nil || after != before || state != "failed" {
				t.Fatalf("terminal lease loss did not roll back file/currentproof: state=%s err=%v", state, err)
			}
			var good int
			if err := f.db.QueryRow(f.ctx, `select count(*) from oss_upload_byte_bindings where file_id=(select id from oss_files where public_id=$1) and status='committed' and file_endpoint<>''`, e.FilePublicID).Scan(&good); err != nil || good != 0 {
				t.Fatalf("lease-expired promotion left approved byte proof=%d err=%v", good, err)
			}
		})
	}
}
