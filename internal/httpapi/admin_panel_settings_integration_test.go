package httpapi

import (
	"bytes"
	"net/http"
	"testing"
)

func TestKnownSettingFieldsDistinguishNullFromAbsent(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		config    any
	}{
		{"profile", `{"signatureMaxBytes":null}`, &profileConfigPayload{SignatureMaxBytes: 256}},
		{"markdown", `{"CORE":null}`, &markdownConfigPayload{Core: true}},
		{"brand", `{"siteName":null}`, &siteGeneralConfig{SiteName: "MCMods"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := decodeOptionalSetting([]byte(tc.raw), nil, tc.name, tc.config); err == nil {
				t.Fatal("an explicit null was treated as a missing field")
			}
		})
	}
	value := defaultMarkdownConfig()
	if err := decodeOptionalSetting([]byte(`{"core":false,"futureOption":null}`), nil, markdownConfigSettingKey, &value); err != nil || value.Core || value.TabSize != defaultMarkdownConfig().TabSize {
		t.Fatalf("valid false, missing and unknown setting fields: core=%v tabSize=%d err=%v", value.Core, value.TabSize, err)
	}
	label := "saved"
	nullable := struct {
		Label *string `json:"label"`
	}{Label: &label}
	if err := decodeOptionalSetting([]byte(`{"label":null}`), nil, "optional", &nullable); err != nil || nullable.Label != nil {
		t.Fatalf("nullable setting field: %v", err)
	}
}

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
	if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values('site.general','null')
		on conflict(key) do update set value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/site/config", nil, http.StatusServiceUnavailable)
	f.require(t, f.editor, http.MethodPut, root+"/general", defaultSiteGeneralConfig(), http.StatusServiceUnavailable)
	f.require(t, f.editor, http.MethodGet, root+"/mail", nil, http.StatusOK)
	var unchanged bool
	if err := f.db.QueryRow(f.ctx, `select value='null'::jsonb from system_settings where key='site.general'`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("unreadable site settings were overwritten: %v", err)
	}
	for _, panel := range []struct {
		key, raw, read, write string
		replacement           any
	}{
		{profileConfigSettingKey, `{"signatureMaxBytes":null}`, root + "/profile", root + "/profile", defaultProfileConfig()},
		{markdownConfigSettingKey, `{"core":null}`, root + "/markdown", root + "/markdown", defaultMarkdownConfig()},
		{siteGeneralSettingKey, `{"siteName":null}`, "/api/v1/site/config", root + "/general", defaultSiteGeneralConfig()},
	} {
		if _, err := f.db.Exec(f.ctx, `update system_settings set value=$2::jsonb where key=$1`, panel.key, panel.raw); err != nil {
			t.Fatal(err)
		}
		f.require(t, f.editor, http.MethodGet, panel.read, nil, http.StatusServiceUnavailable)
		f.require(t, f.editor, http.MethodPut, panel.write, panel.replacement, http.StatusServiceUnavailable)
		var preserved bool
		if err := f.db.QueryRow(f.ctx, `select value=$2::jsonb from system_settings where key=$1`, panel.key, panel.raw).Scan(&preserved); err != nil || !preserved {
			t.Fatalf("%s: unreadable field was overwritten: %v", panel.key, err)
		}
	}
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
