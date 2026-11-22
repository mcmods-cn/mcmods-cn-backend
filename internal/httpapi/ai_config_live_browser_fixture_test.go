package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAIConfigLiveBrowserFixture exposes the ordinary HTTP routes with a
// task-owned PostgreSQL schema and three synthetic authenticated principals.
// The browser runs separately so the frontend can use its normal production
// build and CORS configuration. A fixture exit alone is not browser evidence.
func TestAIConfigLiveBrowserFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_AI_CONFIG_BROWSER") != "1" {
		t.Skip("opt-in AI configuration browser journey")
	}
	directory := os.Getenv("MCMODS_AI_CONFIG_BROWSER_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("requires a new absolute, task-owned runtime directory")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal("cannot create private runtime directory:", err)
	}
	const frontendOrigin = "http://127.0.0.1:3329"
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	f := newAuditFixGRFixture(t, 10*time.Minute)
	for _, token := range []string{f.editor, f.reviewer} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "admin.access", "ai.read", "ai.write")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "admin.access", "ai.read")
	form := decodeTEST022Data[aiConfigView](t,
		f.require(t, f.denied, http.MethodGet, aiConfigTestPath, nil, http.StatusOK))
	f.require(t, f.denied, http.MethodPut, aiConfigTestPath, form, http.StatusForbidden)
	auditJourneyWriteJSON(t, filepath.Join(directory, "private.json"), map[string]string{
		"reader": f.denied, "editorA": f.editor, "editorB": f.reviewer,
	})
	auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), map[string]string{
		"apiOrigin": f.origin.URL, "frontendOrigin": frontendOrigin, "authCookieName": authSessionCookieName,
	})
	t.Log("AI configuration browser fixture ready")

	timer := time.NewTicker(200 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal("AI configuration browser did not complete")
		case <-timer.C:
			if _, err := os.Stat(filepath.Join(directory, "stop.flag")); err == nil {
				t.Fatal("browser requested cleanup after an unsuccessful run")
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			_, err := os.Stat(filepath.Join(directory, "done.flag"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := readAIConfigFromSettings(f.ctx, f.db, f.server.cfg.SettingsEncryptionKey)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Providers) < 2 ||
				cfg.Providers[0].Notes != "Round two browser edit A" ||
				cfg.Providers[1].Notes != "Round two browser edit B" {
				t.Fatal("the browser edits did not retain both administrators' fields")
			}
			var tasks int
			if err = f.db.QueryRow(f.ctx, `select count(*) from ai_tasks`).Scan(&tasks); err != nil || tasks != 0 {
				t.Fatalf("saving configuration created AI tasks: count=%d error=%v", tasks, err)
			}
			t.Log("PostgreSQL kept both reviewed edits and zero AI tasks")
			return
		}
	}
}
