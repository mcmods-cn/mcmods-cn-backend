package httpapi

import (
	"errors"
	"testing"
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
