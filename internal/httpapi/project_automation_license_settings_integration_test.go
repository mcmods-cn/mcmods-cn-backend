package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProjectScheduleChangesPreserveApprovedRedistributionDecisionIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	path := "/api/v1/projects/mod/" + f.modCode + "/automation"
	admin, editor := f.reviewer, f.editor
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[admin], "project.auto_update.view", "project.auto_update.configure", "project.auto_update.redistribution_override")
	var routeID int64
	if err := f.db.QueryRow(f.ctx, `select id from public_routes where entity_type='mod' and public_id=$1`, f.modCode).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at) values
        ($1,'modrinth','test-public-project','https://modrinth.com/mod/test-public-project',now()),($1,'curseforge','11','https://www.curseforge.com/minecraft/mc-mods/test-public-project',now())`, routeID); err != nil {
		t.Fatal(err)
	}
	schedule := []map[string]any{
		{"kind": "minecraft_versions", "interval": "never"},
		{"kind": "changelog", "sourceType": "modrinth", "interval": "week", "enabled": true},
		{"kind": "site_downloads", "sourceType": "curseforge", "interval": "month", "enabled": true, "licenseOverride": true, "licenseOverrideReason": "Written authorization", "licenseOverrideSource": "Publication agreement"},
	}
	body := map[string]any{"items": schedule}
	f.require(t, admin, http.MethodPut, path, body, http.StatusOK)
	var nextRun time.Time
	var author int64
	readSchedule := func() (time.Time, int64) {
		t.Helper()
		var next time.Time
		var by int64
		if err := f.db.QueryRow(f.ctx, `select next_run_at,configured_by from project_auto_update_settings where project_route_id=$1 and update_kind='site_downloads'`, routeID).Scan(&next, &by); err != nil {
			t.Fatal(err)
		}
		return next, by
	}
	nextRun, author = readSchedule()
	schedule[1]["interval"] = "quarter"
	f.require(t, editor, http.MethodPut, path, body, http.StatusOK)
	if next, by := readSchedule(); !next.Equal(nextRun) || by != author || by != f.userIDs[admin] {
		t.Error("an unrelated schedule edit changed the original mirror schedule or its recorded operator")
	}
	schedule[2]["interval"] = "week"
	f.require(t, editor, http.MethodPut, path, body, http.StatusOK)
	var auditActor int64
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*),max(actor_id) from audit_events where aggregate_type='project_automation_license' and aggregate_key=$1`, f.modCode).Scan(&count, &auditActor); err != nil || count != 1 || auditActor != f.userIDs[admin] {
		t.Fatalf("approved decision history count=%d actor=%d err=%v", count, auditActor, err)
	}
	for _, change := range []struct {
		field string
		value any
	}{
		{"licenseOverrideReason", "Another agreement"}, {"licenseOverrideSource", "Another source"}, {"sourceType", "modrinth"}, {"licenseOverride", false},
	} {
		previous := schedule[2][change.field]
		schedule[2][change.field] = change.value
		f.require(t, editor, http.MethodPut, path, body, http.StatusForbidden)
		schedule[2][change.field] = previous
	}
	// The agreement is attached to the currently bound external project.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mods/22" {
			_, _ = io.WriteString(w, `{"data":{"id":22,"isAvailable":true,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/replacement"}}}`)
			return
		}
		if r.URL.Path != "/mods/search" || r.URL.Query().Get("slug") != "replacement" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":22}]}`)
	}))
	defer provider.Close()
	server := f.origin.Config.Handler.(*Server)
	cfg := defaultModImportConfig()
	cfg.CurseForge.Enabled, cfg.CurseForge.BaseURL, cfg.CurseForge.APIKey = true, provider.URL, "fixture"
	sealed, err := server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	replacement := bindProjectSourceRequest{SourceType: "curseforge", URL: "https://www.curseforge.com/minecraft/mc-mods/replacement"}
	f.require(t, editor, http.MethodPost, path+"/sources", replacement, http.StatusConflict)
	schedule[2]["licenseOverride"], schedule[2]["licenseOverrideReason"], schedule[2]["licenseOverrideSource"] = false, "", ""
	schedule[2]["interval"], schedule[2]["enabled"] = "never", false
	f.require(t, admin, http.MethodPut, path, body, http.StatusOK)
	if err := f.db.QueryRow(f.ctx, `select count(*) from audit_events where aggregate_type='project_automation_license' and aggregate_key=$1`, f.modCode).Scan(&count); err != nil || count != 2 {
		t.Fatalf("approved and revoked decisions=%d err=%v", count, err)
	}
	var raw, encrypted []byte
	var previousProject string
	if err := f.db.QueryRow(f.ctx, `select metadata,metadata->'evidence',metadata->>'previousExternalProjectId' from audit_events
        where aggregate_type='project_automation_license' and aggregate_key=$1 and metadata->>'enabled'='false'`, f.modCode).Scan(&raw, &encrypted, &previousProject); err != nil {
		t.Fatal(err)
	}
	var proof map[string]struct {
		Enabled        bool
		Reason, Source string
	}
	if err := server.openSystemSetting(encrypted, &proof); err != nil {
		t.Fatal(err)
	}
	if previousProject != "11" || !proof["before"].Enabled || proof["before"].Reason != "Written authorization" || proof["before"].Source != "Publication agreement" || proof["after"].Enabled || bytes.Contains(raw, []byte("Publication agreement")) {
		t.Error("revocation did not retain the private approval evidence and its original external project")
	}
	f.require(t, editor, http.MethodPost, path+"/sources", replacement, http.StatusOK)
}
