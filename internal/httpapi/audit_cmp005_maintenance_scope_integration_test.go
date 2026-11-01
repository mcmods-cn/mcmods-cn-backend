package httpapi

import (
	"fmt"
	"reflect"
	"testing"
)

func TestAuditCMP005MaintenanceUncoveredTablesAndSchemaChangeHoldWithoutIntentIntegration(t *testing.T) {
	f, _, _, plan := auditOSSByteRawFixture(t)
	if _, err := f.db.Exec(f.ctx, `create table audit_owned_uncovered(body text)`); err != nil {
		t.Fatal(err)
	}
	auditOSSByteReferenceScope(t, f, &plan, []string{"oss_files"})
	filtered := []string{}
	for _, name := range plan.QuiescedReferenceTables {
		if name != "audit_owned_uncovered" {
			filtered = append(filtered, name)
		}
	}
	plan.QuiescedReferenceTables = filtered
	before := auditOSSAllFacts(t, f)
	report, err := RunOSSByteMaintenance(f.ctx, f.db, f.server.cfg, plan, OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned synthetic snapshot"})
	if err != nil || len(report.Results) != 1 || report.Results[0].Status != "REFERENCE_WRITE_SCOPE_GAP" {
		t.Fatalf("uncovered writer table accepted: %+v %v", report, err)
	}
	if report.ReferenceScope.OnlineRaceSafe || !report.ReferenceScope.CatalogComplete || report.ReferenceScope.MutationPreconditionsMet || !reflect.DeepEqual(report.ReferenceScope.UncoveredTables, []string{"audit_owned_uncovered"}) {
		t.Fatalf("declaration was mistaken for enforced coverage: %+v", report.ReferenceScope)
	}
	auditOSSByteReferenceScope(t, f, &plan, []string{"oss_files"})
	if _, err = f.db.Exec(f.ctx, `create table audit_owned_new_schema(body jsonb)`); err != nil {
		t.Fatal(err)
	}
	if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, true); got.Status != "REFERENCE_SCHEMA_CHANGED" {
		t.Fatalf("unreviewed schema change accepted: %+v", got)
	}
	var intents int
	if err = f.db.QueryRow(f.ctx, `select count(*) from oss_upload_maintenance_actions`).Scan(&intents); err != nil || intents != 0 {
		t.Fatalf("scope hold wrote intent=%d err=%v", intents, err)
	}
	if !reflect.DeepEqual(before, auditOSSAllFacts(t, f)) {
		t.Fatal("scope holds changed normal facts")
	}
}

func TestAuditCMP005MaintenanceActualTableFenceAndDeclaredQuiescenceAreSeparateIntegration(t *testing.T) {
	f, _, binding, plan := auditOSSByteRawFixture(t)
	if _, err := f.db.Exec(f.ctx, `create table audit_owned_quiesced(body text)`); err != nil {
		t.Fatal(err)
	}
	auditOSSByteReferenceScope(t, f, &plan, []string{"oss_files"})
	report, err := RunOSSByteMaintenance(f.ctx, f.db, f.server.cfg, plan, OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned synthetic snapshot"})
	if err != nil || len(report.Results) != 1 || report.Results[0].Status != "RAW_REMOVED" {
		t.Fatalf("scoped quiescent cleanup: %+v %v", report, err)
	}
	if report.ReferenceScope.OnlineRaceSafe || !report.ReferenceScope.QuiescenceDeclared || !report.ReferenceScope.MutationPreconditionsMet {
		t.Fatalf("report overclaimed online coverage: %+v", report.ReferenceScope)
	}
	var actual, declared []string
	var sha string
	var quiesced bool
	if err = f.db.QueryRow(f.ctx, `select write_fence_tables,quiesced_reference_tables,reference_schema_sha256,quiescence_declared from oss_upload_maintenance_actions where binding_id=$1::uuid`, binding).Scan(&actual, &declared, &sha, &quiesced); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, []string{"oss_files"}) || !reflect.DeepEqual(declared, plan.QuiescedReferenceTables) || sha != plan.ReferenceSchemaSHA || !quiesced {
		t.Fatalf("journal confused actual locks and operator declaration: actual=%v declared=%v sha=%s quiesced=%v", actual, declared, sha, quiesced)
	}
}

func TestAuditCMP005MaintenancePartitionExpansionNeverExceedsPhysicalLockBudgetIntegration(t *testing.T) {
	f, _, _, plan := auditOSSByteRawFixture(t)
	if _, err := f.db.Exec(f.ctx, `create table audit_owned_partitioned_ref(part integer,body text) partition by range(part)`); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 16; n++ {
		if _, err := f.db.Exec(f.ctx, fmt.Sprintf(`create table audit_owned_ref_part_%02d partition of audit_owned_partitioned_ref for values from (%d) to (%d)`, n, n, n+1)); err != nil {
			t.Fatal(err)
		}
	}
	auditOSSByteReferenceScope(t, f, &plan, []string{"oss_files", "audit_owned_partitioned_ref"})
	run := ossMaintenanceRun{s: f.server, plan: plan, opts: OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned synthetic snapshot"}, hasJournal: true}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = run.freezeReferenceWriteScope(f.ctx, tx); ossMaintenanceScopeStatus(err) != "REFERENCE_LOCK_SCOPE_TOO_WIDE" {
		t.Fatalf("partition descendants escaped physical lock cap: %v", err)
	}
	var actual int
	if err = f.db.QueryRow(f.ctx, `select count(*) from pg_locks l join pg_class c on c.oid=l.relation where c.relname like 'audit_owned_ref_part_%' and l.mode='ShareRowExclusiveLock'`).Scan(&actual); err != nil || actual != 0 {
		t.Fatalf("over-budget scope took partial physical locks=%d err=%v", actual, err)
	}
}
