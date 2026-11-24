package httpapi

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCurseForgeChangelogRetainsPublishedRevisionWhenProviderUnavailableIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	_, worker, job, _ := f.managed(t, false)
	job.Kind, job.SourceType, job.ExternalID = "changelog", "curseforge", "11"
	var mode atomic.Int32
	serveAutomationCurseForgeFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/mods/11/files":
			_, _ = io.WriteString(w, `{"data":[{"id":23,"modId":11,"displayName":"Release 1","fileName":"mod.jar","gameVersions":["1.21.1","Fabric"],"fileDate":"2026-01-01T00:00:00Z"}],"pagination":{"resultCount":1,"totalCount":1}}`)
		case "/mods/11/files/23/changelog":
			switch mode.Load() {
			case 0:
				_, _ = io.WriteString(w, `{"data":"Updated translations."}`)
			case 1:
				w.WriteHeader(http.StatusServiceUnavailable)
			case 2:
				_, _ = io.WriteString(w, `{}`)
			case 3:
				_, _ = io.WriteString(w, `{"data":""}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	cfg := defaultModImportConfig()
	cfg.CurseForge.Enabled, cfg.CurseForge.BaseURL, cfg.CurseForge.APIKey = true, "https://api.curseforge.com/v1", "fixture-only"
	sealed, err := worker.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	if outcome, _, err := worker.execute(f.ctx, job); err != nil || outcome["created"] != 1 {
		t.Fatalf("initial release outcome=%v, error=%v", outcome, err)
	}
	var id string
	if err = f.db.QueryRow(f.ctx, `select changelog_public_id from external_release_bindings where source_type='curseforge' and project_route_id=$1 and external_release_id='23'`, job.RouteID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	original := f.detail(t, "", id, http.StatusOK)
	if original.BodyMarkdown != "Updated translations." || original.PublishedRevisionID == nil {
		t.Fatalf("initial public release has no expected body or revision")
	}
	for _, unavailable := range []int32{1, 2} {
		mode.Store(unavailable)
		if _, code, err := worker.execute(f.ctx, job); err == nil || code != "external_service_unavailable" {
			t.Errorf("unavailable changelog mode %d, code=%s, err=%v", unavailable, code, err)
		}
		after := f.detail(t, "", id, http.StatusOK)
		if after.BodyMarkdown != original.BodyMarkdown || after.PublishedRevisionID == nil || *after.PublishedRevisionID != *original.PublishedRevisionID {
			t.Errorf("unavailable changelog mode %d replaced the published revision", unavailable)
		}
	}
	mode.Store(0)
	if outcome, _, err := worker.execute(f.ctx, job); err != nil || outcome["unchanged"] != 1 {
		t.Errorf("recovered unchanged release outcome=%v, error=%v", outcome, err)
	}
	mode.Store(3)
	if outcome, _, err := worker.execute(f.ctx, job); err != nil || outcome["updated"] != 1 {
		t.Fatalf("declared empty release outcome=%v, error=%v", outcome, err)
	}
	if item := f.detail(t, "", id, http.StatusOK); !strings.Contains(item.BodyMarkdown, "[View the external release](") {
		t.Fatalf("declared empty changelog did not keep the external release link")
	}
}
