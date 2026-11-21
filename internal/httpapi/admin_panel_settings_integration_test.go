package httpapi

import (
	"bytes"
	"net/http"
	"testing"
)

func TestAdminSettingsPanelsReadIndependentlyAndPreserveUnreadableSettingsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "admin.config.read", "admin.config.write")
	const root = "/api/v1/admin/config"
	for _, panel := range []string{"/auth", "/mail", "/profile", "/markdown", "/features"} {
		f.require(t, f.denied, http.MethodGet, root+panel, nil, http.StatusForbidden)
		f.require(t, f.editor, http.MethodGet, root+panel, nil, http.StatusOK)
	}
	// Simulate two unreadable stored configurations in this fixture's own DB.
	// These failure responses must never supply fallbacks to an edit form.
	if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values
		('markdown.rendering','{"tabSize":"unavailable"}'),('profile.config','null')
		on conflict(key) do update set value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var result string
		if err := f.db.QueryRow(f.ctx, `select jsonb_object_agg(key,value)::text from system_settings
			where key in ('markdown.rendering','profile.config')`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	for _, panel := range []struct {
		path string
		body any
	}{{"/markdown", defaultMarkdownConfig()}, {"/profile", defaultProfileConfig()}} {
		f.require(t, f.editor, http.MethodGet, root+panel.path, nil, http.StatusServiceUnavailable)
		f.require(t, f.editor, http.MethodPut, root+panel.path, panel.body, http.StatusServiceUnavailable)
	}
	if after := snapshot(); after != before {
		t.Fatal("a failed read allowed default settings to replace the stored values")
	}
	f.require(t, f.editor, http.MethodGet, root, nil, http.StatusServiceUnavailable)
	f.require(t, f.editor, http.MethodGet, root+"/auth", nil, http.StatusOK)
	mail := f.require(t, f.editor, http.MethodGet, root+"/mail", nil, http.StatusOK)
	if bytes.Contains(mail, []byte(`"password":`)) {
		t.Fatal("mail form must receive only the redacted configuration")
	}
	f.require(t, f.editor, http.MethodGet, root+"/features", nil, http.StatusOK)
	f.require(t, "", http.MethodGet, "/api/v1/site/config", nil, http.StatusOK)
	fallback := decodeTEST022Data[markdownConfigPayload](t, f.require(t, "", http.MethodGet, "/api/v1/markdown/config", nil, http.StatusOK))
	if fallback != defaultMarkdownConfig() {
		t.Fatal("public renderer lost its safe fallback")
	}
	if _, err := f.db.Exec(f.ctx, `update system_settings set value='{}'::jsonb
		where key in ('markdown.rendering','profile.config')`); err != nil {
		t.Fatal(err)
	}
	want := profileConfigPayload{SignatureMaxBytes: 160}
	f.require(t, f.editor, http.MethodPut, root+"/profile", want, http.StatusOK)
	got := decodeTEST022Data[profileConfigPayload](t, f.require(t, f.editor, http.MethodGet, root+"/profile", nil, http.StatusOK))
	if got != want {
		t.Fatalf("recovered profile setting=%v want=%v", got, want)
	}
}
