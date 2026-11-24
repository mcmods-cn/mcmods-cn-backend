package httpapi

import (
	"context"
	"errors"
	"testing"

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
	if err = register(); err != nil {
		t.Fatal(err)
	}
	var state, license, project string
	if err = f.db.QueryRow(f.ctx, `select status,license_spdx_id,metadata->>'providerProjectId' from mirrored_project_files where project_route_id=$1 and external_file_id=$2`, job.RouteID, file.ID).Scan(&state, &license, &project); err != nil || state != "scanning" || license != "ARR" || project != job.ExternalID {
		t.Fatalf("register authorized mirror: state=%s license=%s project=%s err=%v", state, license, project, err)
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
