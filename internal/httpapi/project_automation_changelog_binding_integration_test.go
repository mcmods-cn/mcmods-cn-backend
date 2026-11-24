package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProjectChangelogAutomationRetainsIndependentHistoryWhenSourceReboundIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	server := f.origin.Config.Handler.(*Server)
	configureSeededAutomationFixture(t, f.ctx, f.db, "project.edit", "project.no-review", "content.no-review")
	worker := &ProjectAutomationWorker{server: server}

	const source = "release-scope-source"
	const replacement = "release-scope-replacement"
	const release = "release-scope-version"
	const originalBody = "Original release notes"
	const updatedBody = "Updated release notes"
	var updated atomic.Bool
	published := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/project/" + source:
			_, _ = fmt.Fprintf(w, `{"id":%q,"status":"approved"}`, source)
		case "/project/" + replacement:
			_, _ = fmt.Fprintf(w, `{"id":%q,"status":"approved"}`, replacement)
		case "/project/" + source + "/version":
			body := originalBody
			if updated.Load() {
				body = updatedBody
			}
			_, _ = fmt.Fprintf(w, `[{"id":%q,"status":"listed","version_number":"1.0.0","changelog":%q,"date_published":%q,
				"game_versions":["1.21.1"],"loaders":["fabric"],"files":[]}]`, release, body, published)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = provider.URL
	sealed, err := server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
		on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}

	const restPath = "/api/v1/projects/mod/"
	bind := func(token, projectID, sourceID string) {
		t.Helper()
		f.require(t, token, http.MethodPost, restPath+projectID+"/automation/sources", bindProjectSourceRequest{
			SourceType: "modrinth", URL: "https://modrinth.com/mod/" + sourceID,
		}, http.StatusOK)
	}
	configure := func(token, projectID string) {
		t.Helper()
		f.require(t, token, http.MethodPut, restPath+projectID+"/automation", map[string]any{"items": []map[string]any{
			{"kind": "minecraft_versions", "interval": "never"},
			{"kind": "changelog", "sourceType": "modrinth", "interval": "week", "enabled": true},
			{"kind": "site_downloads", "interval": "never"},
		}}, http.StatusOK)
	}
	run := func(token, projectID, outcome string) {
		t.Helper()
		raw := f.require(t, token, http.MethodPost, restPath+projectID+"/automation/runs", map[string]string{"kind": "changelog"}, http.StatusAccepted)
		var submitted struct {
			Data struct{ ID, Status string }
		}
		if err := json.Unmarshal(raw, &submitted); err != nil || submitted.Data.ID == "" || submitted.Data.Status != "pending" {
			t.Fatalf("expected a pending changelog job: %s, err=%v", raw, err)
		}
		if processed, err := worker.processOne(f.ctx); err != nil || !processed {
			t.Fatalf("run project %s changelog job: processed=%t, err=%v", projectID, processed, err)
		}
		var status string
		var counts map[string]int
		if err := f.db.QueryRow(f.ctx, `select status,jsonb_build_object('created',result->'created','updated',result->'updated',
			'unchanged',result->'unchanged','manualOverrideConflicts',result->'manualOverrideConflicts')
			from project_auto_update_runs where public_id=$1`, submitted.Data.ID).Scan(&status, &counts); err != nil {
			t.Fatal(err)
		}
		if status != "completed" || counts[outcome] != 1 || counts["created"]+counts["updated"]+counts["unchanged"]+counts["manualOverrideConflicts"] != 1 {
			t.Fatalf("project %s changelog job status=%s result=%v, expected one %s", projectID, status, counts, outcome)
		}
	}
	changelogFor := func(projectID string) string {
		t.Helper()
		var id string
		if err := f.db.QueryRow(f.ctx, `select binding.changelog_public_id from external_release_bindings binding
			join public_routes route on route.id=binding.project_route_id
			where route.public_id=$1 and binding.source_type='modrinth' and binding.external_release_id=$2`, projectID, release).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	bind(f.editor, f.modCode, source)
	configure(f.editor, f.modCode)
	run(f.editor, f.modCode, "created")
	first := changelogFor(f.modCode)
	const manualBody = "Approved notes from the first project"
	proposal := f.submit(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+first, test017Snapshot(manualBody), http.StatusOK)
	f.review(t, f.reviewer, proposal, "approved", http.StatusOK)
	before := f.detail(t, "", first, http.StatusOK)
	assertBUG034ManualOverride(t, f.ctx, f.db, first, true)

	bind(f.editor, f.modCode, replacement)
	bind(f.otherEditor, f.otherModCode, source)
	configure(f.otherEditor, f.otherModCode)
	run(f.otherEditor, f.otherModCode, "created")
	second := changelogFor(f.otherModCode)
	if first == second || f.detail(t, "", second, http.StatusOK).BodyMarkdown != originalBody {
		t.Fatal("the second project did not receive its own original release notes")
	}
	updated.Store(true)
	run(f.otherEditor, f.otherModCode, "updated")
	run(f.otherEditor, f.otherModCode, "unchanged")
	if item := f.detail(t, "", second, http.StatusOK); item.BodyMarkdown != updatedBody {
		t.Fatalf("second project changelog body=%q want=%q", item.BodyMarkdown, updatedBody)
	}
	if item := f.detail(t, "", first, http.StatusOK); item.BodyMarkdown != manualBody || item.PublishedRevisionID == nil || *item.PublishedRevisionID != proposal.RevisionID ||
		before.PublishedRevisionID == nil || *item.PublishedRevisionID != *before.PublishedRevisionID {
		t.Fatalf("the first project's approved manual revision was not retained: %+v", item)
	}
}
