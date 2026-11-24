package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDownloadedMirrorUsesCurrentRedistributionDecisionIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	_, worker, job, _ := f.managed(t, false)
	job.Kind, job.ExternalID = "site_downloads", "upstream-project"
	file := providerProjectFile{ID: "test-release", FileName: "test.jar", Source: "modrinth"}
	cfg := ossConfigPayload{Bucket: "owned", Endpoint: "https://owned.example.invalid", Region: "cn-owned"}
	const key = "owned/test.jar"
	const digest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if _, err := scheduleOSSUploadExpiry(f.ctx, f.db, cfg, key, projectMirrorUploadExpiryReason, nil); err != nil {
		t.Fatal(err)
	}
	register := func() error { return worker.registerDownloadedProjectMirror(f.ctx, job, cfg, file, key, 4, digest) }
	if _, err := f.db.Exec(f.ctx, `update mods set license='MIT' where id=$1`, job.InternalID); err != nil {
		t.Fatal(err)
	}
	if _, err := requireProjectRedistribution(f.ctx, f.db, job); err != nil {
		t.Fatal(err)
	}
	change, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(change)
	if _, err = change.Exec(f.ctx, `update mods set license='ARR' where id=$1`, job.InternalID); err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() { completed <- register() }()
	auditCompletionAwaitLocks(t, f.test013Fixture, 1)
	if err = change.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-completed; !errors.Is(err, errRedistributionNotAllowed) {
		t.Fatalf("registration after license change: %v", err)
	}
	var rows int
	if err = f.db.QueryRow(f.ctx, `select count(*) from oss_files where object_key=$1`, key).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("denied registration left %d storage records: %v", rows, err)
	}
	job.LicenseOverride, job.OverrideReason, job.OverrideSource = true, "Written authorization", "Test publisher"
	if _, err = f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, job.InternalID); err != nil {
		t.Fatal(err)
	}
	if err = register(); !errors.Is(err, errProjectAutomationParentUnavailable) {
		t.Fatalf("pending parent result: %v", err)
	}
	if _, err = f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, job.InternalID); err != nil {
		t.Fatal(err)
	}
	// Hold a real registration at COMMIT, when another connection can still see
	// the old due receipt and cannot yet see the registered file.
	if _, err = f.db.Exec(f.ctx, `update oss_object_deletion_outbox set next_attempt_at=now() where object_key='owned/test.jar';
		create table mirror_registration_barrier(id int primary key);
		insert into mirror_registration_barrier values(1);
		create function wait_for_mirror_registration() returns trigger language plpgsql as $$
		begin perform id from mirror_registration_barrier where id=1 for update; return null; end $$;
		create constraint trigger mirror_registration_at_commit after insert on mirrored_project_files
		deferrable initially deferred for each row execute function wait_for_mirror_registration()`); err != nil {
		t.Fatal(err)
	}
	barrier, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(barrier)
	if _, err = barrier.Exec(f.ctx, `select id from mirror_registration_barrier where id=1 for update`); err != nil {
		t.Fatal(err)
	}
	go func() { completed <- register() }()
	auditCompletionAwaitLocks(t, f.test013Fixture, 1)
	var visible bool
	if err = f.db.QueryRow(f.ctx, `select not exists(select 1 from oss_files where object_key=$1)
		and exists(select 1 from oss_object_deletion_outbox where object_key=$1 and next_attempt_at<=now())`, key).Scan(&visible); err != nil || !visible {
		t.Fatalf("second connection did not observe the pre-commit facts: visible=%v err=%v", visible, err)
	}
	cleanup := NewOSSDeletionWorker(worker.server.cfg, f.db)
	if _, err = cleanup.claim(f.ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claimed a transfer while registration was committing: %v", err)
	}
	if err = barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-completed; err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `select count(*) from oss_object_deletion_outbox where bucket=$1 and object_key=$2`, cfg.Bucket, key).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("committed mirror retained an expiry: rows=%d err=%v", rows, err)
	}
	var state, license, project string
	if err = f.db.QueryRow(f.ctx, `select status,license_spdx_id,metadata->>'providerProjectId' from mirrored_project_files where project_route_id=$1 and external_file_id=$2`, job.RouteID, file.ID).Scan(&state, &license, &project); err != nil || state != "scanning" || license != "ARR" || project != job.ExternalID {
		t.Fatalf("register authorized mirror: state=%s license=%s project=%s err=%v", state, license, project, err)
	}
	// An earlier delete can arrive after its retry has gone back to pending.
	const retiredKey = "owned/already-attempted.jar"
	id, err := scheduleOSSUploadExpiry(f.ctx, f.db, cfg, retiredKey, projectMirrorUploadExpiryReason, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update oss_object_deletion_outbox set attempts=1 where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	file.ID = "another-release"
	if err = worker.registerDownloadedProjectMirror(f.ctx, job, cfg, file, retiredKey, 4, digest); err == nil {
		t.Fatal("a transfer with a previously attempted deletion was registered")
	}
	if err = f.db.QueryRow(f.ctx, `select count(*) from oss_files where object_key=$1`, retiredKey).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("lost cleanup ownership still produced a file: rows=%d err=%v", rows, err)
	}
	if _, err = f.db.Exec(f.ctx, `update oss_object_deletion_outbox set attempts=0,replay_count=1 where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = worker.registerDownloadedProjectMirror(f.ctx, job, cfg, file, retiredKey, 4, digest); err == nil {
		t.Fatal("a replayed deletion became eligible for registration again")
	}

}

func enableMirrorPublicationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at)
		values($1,'modrinth','fixture-provider-project','https://example.invalid/project',now())`, routeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,
		license_override,license_override_reason,license_override_source)
		values($1,'site_downloads','modrinth','never',true,'Written permission','Fixture publisher')`, routeID); err != nil {
		t.Fatal(err)
	}
}

func TestCleanMirrorHoldsFirstPublicationUntilCurrentPolicyAllowsItIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	ctx := f.ctx
	configureSeededAutomationFixture(t, ctx, f.db, "project.edit."+f.modCode, "project.no-review."+f.modCode)
	var routeID int64
	if err := f.db.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, f.modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	enableMirrorPublicationFixture(t, ctx, f.db, routeID)
	worker := &ProjectAutomationWorker{server: f.server}
	job := projectAutomationJob{RouteID: routeID, InternalID: f.modID, ProjectType: "mod", ProjectPublicID: f.modCode, SourceType: "modrinth", ExternalID: "fixture-provider-project"}
	file := providerProjectFile{ID: "publication-policy", FileName: "approved.jar", DirectURL: "https://cdn.example.invalid/approved.jar", SizeBytes: 128}
	insertBUG035CleanMirror(t, ctx, f.db, routeID, file.ID)
	if _, err := f.db.Exec(ctx, `update mods set license='ARR' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	setGrant := func(allow bool) {
		t.Helper()
		if _, err := f.db.Exec(ctx, `update project_auto_update_settings set license_override=$2 where project_route_id=$1 and update_kind='site_downloads'`, routeID, allow); err != nil {
			t.Fatal(err)
		}
	}
	resume := func() {
		t.Helper()
		res, err := worker.mirrorFiles(ctx, job, modImportConfig{}, []providerProjectFile{file})
		if err != nil || res["awaitingScan"] != 1 || res["queuedForScan"] != 0 {
			t.Fatalf("resume existing candidate: %v, %v", res, err)
		}
	}
	want := func(status, reason string, count int) {
		t.Helper()
		if err := worker.promoteCleanMirrors(ctx); err != nil {
			t.Fatal(err)
		}
		var gotStatus, gotReason string
		var files int
		if err := f.db.QueryRow(ctx, `select m.status,coalesce(m.metadata->>'publicationHold',''),
			(select count(*) from project_files where project_type='mod' and project_internal_id=$1 and oss_file_id=m.oss_file_id)
			from mirrored_project_files m where m.project_route_id=$2 and m.external_file_id=$3`, f.modID, routeID, file.ID).
			Scan(&gotStatus, &gotReason, &files); err != nil || gotStatus != status || gotReason != reason || files != count {
			t.Fatalf("mirror=%s reason=%s files=%d; want %s %s %d; err=%v", gotStatus, gotReason, files, status, reason, count, err)
		}
	}
	setGrant(false)
	want("review", "redistribution_unavailable", 0)
	setGrant(true)
	configureSeededAutomationFixture(t, ctx, f.db, "project.edit."+f.modCode)
	resume()
	want("review", "actor_unavailable", 0)
	configureSeededAutomationFixture(t, ctx, f.db, "project.edit."+f.modCode, "project.no-review."+f.modCode)
	resume()
	if _, err := f.db.Exec(ctx, `update project_external_sources set external_project_id='other-upstream' where project_route_id=$1`, routeID); err != nil {
		t.Fatal(err)
	}
	want("review", "source_changed", 0)
	if _, err := f.db.Exec(ctx, `update project_external_sources set external_project_id=$2 where project_route_id=$1`, routeID, job.ExternalID); err != nil {
		t.Fatal(err)
	}
	resume()
	want("ready", "", 1)
	setGrant(false)
	if _, err := f.db.Exec(ctx, `update mirrored_project_files set status='scanning' where project_route_id=$1 and external_file_id=$2`, routeID, file.ID); err != nil {
		t.Fatal(err)
	}
	want("ready", "", 1)
}
