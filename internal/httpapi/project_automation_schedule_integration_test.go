package httpapi

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestProjectAutomationRetainsNewScheduleWhileRunFinishesIntegration(t *testing.T) {
	for _, tc := range []struct {
		name            string
		providerFailure bool
		wantStatus      string
	}{
		{name: "completion", wantStatus: "completed"},
		{name: "terminal_failure", providerFailure: true, wantStatus: "dead_letter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTEST017Fixture(t)
			server := f.origin.Config.Handler.(*Server)
			configureSeededAutomationFixture(t, f.ctx, f.db, "project.edit", "project.no-review", "content.no-review")
			reachedProvider, releaseProvider := make(chan struct{}), make(chan struct{})
			var reachedOnce, releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseProvider) }) }
			defer release()

			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/project/schedule-source":
					_, _ = w.Write([]byte(`{"id":"schedule-source","status":"approved"}`))
				case "/project/schedule-source/version":
					reachedOnce.Do(func() { close(reachedProvider) })
					select {
					case <-r.Context().Done():
						return
					case <-releaseProvider:
					}
					if tc.providerFailure {
						http.Error(w, "synthetic provider unavailable", http.StatusServiceUnavailable)
						return
					}
					_, _ = w.Write([]byte(`[]`))
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

			path := "/api/v1/projects/mod/" + f.modCode + "/automation"
			f.require(t, f.editor, http.MethodPost, path+"/sources", bindProjectSourceRequest{
				SourceType: "modrinth", URL: "https://modrinth.com/mod/schedule-source",
			}, http.StatusOK)
			configure := func(interval string) {
				t.Helper()
				f.require(t, f.editor, http.MethodPut, path, map[string]any{"items": []map[string]any{
					{"kind": "minecraft_versions", "interval": "never"},
					{"kind": "changelog", "sourceType": "modrinth", "interval": interval, "enabled": true},
					{"kind": "site_downloads", "interval": "never"},
				}}, http.StatusOK)
			}
			configure("week")
			submitted := decodeTEST022Data[struct{ ID, Status string }](t, f.require(t, f.editor, http.MethodPost, path+"/runs",
				map[string]string{"kind": "changelog"}, http.StatusAccepted))
			if submitted.ID == "" || submitted.Status != "pending" {
				t.Fatalf("unexpected run admission: %+v", submitted)
			}
			if tc.providerFailure {
				if _, err = f.db.Exec(f.ctx, `update project_auto_update_runs set attempts=4 where public_id=$1`, submitted.ID); err != nil {
					t.Fatal(err)
				}
			}

			type workerResult struct {
				processed bool
				err       error
			}
			done := make(chan workerResult, 1)
			worker := &ProjectAutomationWorker{server: server}
			go func() {
				processed, runErr := worker.processOne(f.ctx)
				done <- workerResult{processed, runErr}
			}()
			select {
			case <-reachedProvider:
			case early := <-done:
				t.Fatalf("worker stopped before the provider response: %+v", early)
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}

			configure("month")
			readState := func() (time.Time, string, string) {
				t.Helper()
				var next time.Time
				var interval, status string
				err := f.db.QueryRow(f.ctx, `select setting.next_run_at,setting.interval_code,run.status
					from project_auto_update_runs run join project_auto_update_settings setting on setting.id=run.setting_id
					where run.public_id=$1`, submitted.ID).Scan(&next, &interval, &status)
				if err != nil {
					t.Fatal(err)
				}
				return next, interval, status
			}
			configured, interval, status := readState()
			if interval != "month" || status != "running" {
				t.Fatalf("saved schedule interval=%q, active run=%q", interval, status)
			}
			release()
			select {
			case result := <-done:
				if !result.processed || result.err != nil {
					t.Fatalf("worker result=%+v", result)
				}
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			next, interval, status := readState()
			if interval != "month" || status != tc.wantStatus || !next.Equal(configured) {
				t.Fatalf("run status=%s interval=%s next=%s, want %s month %s", status, interval, next, tc.wantStatus, configured)
			}
		})
	}
}
