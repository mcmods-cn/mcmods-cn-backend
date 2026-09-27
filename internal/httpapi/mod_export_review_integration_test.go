package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestOptionalExportContentLocaleOnlyOverridesWhenExplicitAndValid(t *testing.T) {
	for _, value := range []string{"", "   ", "not a locale!", "x" + string(make([]byte, 40))} {
		if locale, ok := optionalExportContentLocale(value); ok || locale != "" {
			t.Fatalf("optional locale %q = %q,%v; want no override", value, locale, ok)
		}
	}
	if locale, ok := optionalExportContentLocale(" en_us "); !ok || locale != "en-US" {
		t.Fatalf("explicit locale = %q,%v; want en-US,true", locale, ok)
	}
}

func TestModExportRevisionReviewAuditCommitsFactsAndFailsAtomicallyIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `create temp table audit_events(
		id bigserial primary key,aggregate_type text not null,aggregate_key text not null,
		actor_id bigint not null,action text not null,ip text not null,user_agent text not null,
		metadata jsonb not null,created_at timestamptz not null default now(),
		check(coalesce(metadata->>'note','') <> 'force-audit-failure'));
		create temp table export_review_state(id text primary key,status text not null,is_active boolean not null);
		insert into export_review_state values('revision-commit','ready',false),('revision-rollback','ready',false)`); err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update export_review_state set is_active=true where id='revision-commit'`); err != nil {
		t.Fatal(err)
	}
	audit := modExportRevisionReviewAudit{
		RevisionID: "revision-commit", Decision: "approved", Note: "checked assets and recipes",
		BeforeStatus: "ready", BeforeActive: false, AfterStatus: "ready", AfterActive: true,
		ModID: 17, TargetVersionID: 23, Namespace: "example", SourceKind: "mcmods_exporter",
		ActorID: 42, IP: "203.0.113.7", UserAgent: "review-test",
	}
	if err = recordModExportRevisionReviewTx(ctx, tx, audit); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var aggregateType, aggregateKey, action string
	var actorID int64
	var metadataRaw []byte
	var createdAt time.Time
	if err = db.QueryRow(ctx, `select aggregate_type,aggregate_key,actor_id,action,metadata,created_at from audit_events`).
		Scan(&aggregateType, &aggregateKey, &actorID, &action, &metadataRaw, &createdAt); err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err = json.Unmarshal(metadataRaw, &metadata); err != nil {
		t.Fatal(err)
	}
	if aggregateType != "catalog_import_revision" || aggregateKey != audit.RevisionID || actorID != 42 || action != "review_approved" || createdAt.IsZero() {
		t.Fatalf("incomplete audit authority: type=%s key=%s actor=%d action=%s created=%s", aggregateType, aggregateKey, actorID, action, createdAt)
	}
	if metadata["note"] != audit.Note || metadata["decision"] != audit.Decision || metadata["beforeActive"] != false || metadata["afterActive"] != true {
		t.Fatalf("review metadata was not preserved: %#v", metadata)
	}

	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update export_review_state set status='rejected' where id='revision-rollback'`); err != nil {
		t.Fatal(err)
	}
	audit.RevisionID = "revision-rollback"
	audit.Decision = "rejected"
	audit.Note = "force-audit-failure"
	audit.AfterStatus = "rejected"
	audit.AfterActive = false
	if err = recordModExportRevisionReviewTx(ctx, tx, audit); err == nil {
		t.Fatal("injected audit failure was accepted")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	var active bool
	if err = db.QueryRow(ctx, `select status,is_active from export_review_state where id='revision-rollback'`).Scan(&status, &active); err != nil {
		t.Fatal(err)
	}
	var failedAuditCount int
	if err = db.QueryRow(ctx, `select count(*) from audit_events where aggregate_key='revision-rollback'`).Scan(&failedAuditCount); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || active || failedAuditCount != 0 {
		t.Fatalf("audit failure did not roll back review facts: status=%s active=%v audit=%d", status, active, failedAuditCount)
	}
}

func TestModExportJobResponseRejectsWrongJSONShapesIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `create temp table mods(id bigint primary key,slug text not null);
		create temp table mod_content_versions(id bigint primary key,public_id text not null);
		create temp table catalog_import_jobs(
			id text primary key,mod_id bigint not null,package_id text not null,target_version_id bigint not null,
			overwrite_existing boolean not null,status text not null,progress integer not null,current_stage text not null,
			error_code text not null,error_detail jsonb not null,created_at timestamptz not null default now(),updated_at timestamptz not null default now(),
			configured_modids text[] not null,detected_modids jsonb not null,primary_detected_modid text not null,
			modid_confirmation_required boolean not null,modid_analysis_hash text not null);
		create temp table catalog_import_revisions(job_id text not null,status text not null,is_active boolean not null);
		insert into mods values(1,'example-mod');
		insert into mod_content_versions values(2,'version-public');
		insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,overwrite_existing,status,progress,current_stage,error_code,error_detail,
			configured_modids,detected_modids,primary_detected_modid,modid_confirmation_required,modid_analysis_hash)
			values('job-shape',1,'package',2,false,'ready',100,'ready','','{}',array['example'],'[]','example',false,'hash')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db}
	if _, err := server.modExportJobByID(ctx, "job-shape", 1); err != nil {
		t.Fatalf("valid job JSON rejected: %v", err)
	}
	if _, err := db.Exec(ctx, `update catalog_import_jobs set configured_modids=null where id='job-shape'`); err == nil {
		t.Fatal("configured Mod IDs accepted null despite the production not-null contract")
	}
	if _, err := db.Exec(ctx, `alter table catalog_import_jobs alter column configured_modids drop not null;
		update catalog_import_jobs set configured_modids=null where id='job-shape'`); err != nil {
		t.Fatal(err)
	}
	if _, err := server.modExportJobByID(ctx, "job-shape", 1); err == nil {
		t.Fatal("job response accepted null configured_modids")
	}
	if _, err := db.Exec(ctx, `update catalog_import_jobs set configured_modids='{}'::text[],error_detail='[]'::jsonb where id='job-shape'`); err != nil {
		t.Fatal(err)
	}
	if _, err := server.modExportJobByID(ctx, "job-shape", 1); err == nil {
		t.Fatal("job response accepted error_detail with an array shape")
	}
}
