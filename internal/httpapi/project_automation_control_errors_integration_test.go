package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutomationControlDistinguishesMissingSourcesAndUnavailableServicesIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.external_source.bind", "project.auto_update.view", "project.auto_update.run", "project.auto_update.view_logs")
	s := f.origin.Config.Handler.(*Server)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mods/41":
			_, _ = io.WriteString(w, `{"data":{"id":41,"isAvailable":true,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/example"}}}`)
		case "/mods/42":
			_, _ = io.WriteString(w, `{"data":{"id":42,"isAvailable":false}}`)
		case "/mods/44":
			http.Error(w, "fixture upstream unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mock.Close)
	cfg := defaultModImportConfig()
	cfg.CurseForge.Enabled, cfg.CurseForge.BaseURL, cfg.CurseForge.APIKey = true, mock.URL, "fixture"
	raw, err := s.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, raw); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/mod/" + f.modCode + "/automation"
	for _, tc := range []struct {
		id   int
		want int
	}{{41, 200}, {42, 400}, {43, 400}, {44, 503}} {
		t.Run(fmt.Sprint(tc.id), func(t *testing.T) {
			body := bindProjectSourceRequest{SourceType: "curseforge", URL: fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/%d", tc.id)}
			b := f.require(t, f.editor, http.MethodPost, path+"/sources", body, tc.want)
			if bytes.Contains(b, []byte("fixture upstream unavailable")) {
				t.Error("provider diagnostic reached the client")
			}
			var stored string
			if err := f.db.QueryRow(f.ctx, `select external_project_id from project_external_sources s join public_routes r on r.id=s.project_route_id where r.public_id=$1 and s.source_type='curseforge'`, f.modCode).Scan(&stored); err != nil || stored != "41" {
				t.Errorf("verified source=%s error=%v", stored, err)
			}
		})
	}
	run := map[string]string{"kind": "changelog"}
	f.require(t, f.editor, http.MethodPost, path+"/runs", run, http.StatusConflict)
	if _, err = f.db.Exec(f.ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code) select id,'changelog','curseforge','never' from public_routes where public_id=$1 and entity_type='mod'`, f.modCode); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, path+"/runs", run, http.StatusAccepted)
	f.require(t, f.editor, http.MethodPost, path+"/runs", run, http.StatusConflict)
	if _, err = f.db.Exec(f.ctx, `alter table project_auto_update_runs rename to held_synthetic_runs`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, path+"/runs", run, http.StatusInternalServerError)
	if _, err = f.db.Exec(f.ctx, `alter table public_routes rename to held_synthetic_routes`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, suffix string
		body           any
	}{{"GET", "", nil}, {"GET", "/runs", nil}, {"POST", "/runs", run}, {"POST", "/sources", bindProjectSourceRequest{}}} {
		t.Run(tc.method+tc.suffix, func(t *testing.T) {
			f.require(t, f.editor, tc.method, path+tc.suffix, tc.body, http.StatusInternalServerError)
		})
	}
}
