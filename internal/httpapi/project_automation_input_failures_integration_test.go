package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestProjectAutomationSeparatesUnavailableFactsFromPolicyDecisionsIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	_, worker, job, release := f.managed(t, false)
	job.Kind = "site_downloads"
	if _, err := f.db.Exec(f.ctx, `update mods set license='ARR' where id=$1`, job.InternalID); err != nil {
		t.Fatal(err)
	}
	if _, code, err := worker.execute(f.ctx, job); err == nil || code != "license_denied" {
		t.Fatalf("expected the actual restrictive license, code=%s err=%v", code, err)
	}
	job.LicenseOverride, job.OverrideReason, job.OverrideSource = true, "Written permission", "Fixture agreement"
	if err := worker.checkRedistribution(f.ctx, job); err != nil {
		t.Fatalf("explicit redistribution permission: %v", err)
	}

	if _, err := f.db.Exec(f.ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at)
		values($1,'modrinth','source','https://modrinth.com/mod/source',now())`, job.RouteID); err != nil {
		t.Fatal(err)
	}
	// TEST017 owns the entire database. The normal handler and worker below
	// must report a failed read separately from a valid restrictive policy.
	if _, err := f.db.Exec(f.ctx, `alter table license_policies rename to unavailable_policies`); err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if err := worker.checkRedistribution(f.ctx, job); !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("policy storage error under manual override=%v", err)
	}
	job.LicenseOverride = false
	if _, code, err := worker.execute(f.ctx, job); err == nil || code != "configuration_error" {
		t.Errorf("policy read failure code=%s err=%v", code, err)
	}
	f.require(t, f.editor, http.MethodPut, "/api/v1/projects/mod/"+job.ProjectPublicID+"/automation", map[string]any{"items": []map[string]any{
		{"kind": "minecraft_versions", "interval": "never"}, {"kind": "changelog", "interval": "never"},
		{"kind": "site_downloads", "sourceType": "modrinth", "interval": "week", "enabled": true},
	}}, http.StatusInternalServerError)

	if _, err := f.db.Exec(f.ctx, `alter table mod_loader_compatibilities rename to unavailable_compatibilities`); err != nil {
		t.Fatal(err)
	}
	job.Kind = "changelog"
	release.Body = "Another release with explicit versions"
	if counts, err := worker.syncChangelogs(f.ctx, job, []projectAutomationRelease{release}); err != nil || counts["updated"] != 1 {
		t.Errorf("provider-supplied versions should not require a fallback: %v, %v", counts, err)
	}
	release.GameVersions = nil
	counts, err := worker.syncChangelogs(f.ctx, job, []projectAutomationRelease{release})
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("missing version data source was counted as an unmapped version: %v, %v", counts, err)
	}
}
