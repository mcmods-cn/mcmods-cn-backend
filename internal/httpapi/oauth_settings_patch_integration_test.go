package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestOAuthAdminPatchPreservesConcurrentFieldsAndStoredSecretIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	const path = "/api/v1/admin/config/oauth"
	for _, editor := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[editor], "admin.config.read", "admin.config.write")
	}
	request := func(fields any) map[string]any {
		return map[string]any{"providers": map[string]any{"github": fields}}
	}
	original := oauthProviderConfig{Enabled: true, ClientID: "example-id", ClientSecret: "example-setting-value", RedirectURI: "https://example.test/auth/callback"}
	f.require(t, f.editor, http.MethodPut, path, request(original), http.StatusOK)

	first := auditCompletionAsyncJSON(f, f.editor, http.MethodPatch, path, request(map[string]any{"enabled": false}))
	second := auditCompletionAsyncJSON(f, f.otherEditor, http.MethodPatch, path, request(map[string]any{"clientId": "changed-id"}))
	for _, result := range []auditCompletionHTTPResult{<-first, <-second} {
		if result.err != nil || result.status != http.StatusOK || bytes.Contains(result.raw, []byte(original.ClientSecret)) {
			t.Fatalf("field update status=%d, error=%v, secret redacted=%t", result.status, result.err, !bytes.Contains(result.raw, []byte(original.ClientSecret)))
		}
	}
	check := func(want oauthProviderConfig) {
		t.Helper()
		cfg, err := f.origin.Config.Handler.(*Server).oauthConfig(f.ctx)
		if err != nil || cfg.Providers["github"] != want {
			t.Fatalf("stored provider did not preserve its intended fields: %v", err)
		}
	}
	want := original
	want.Enabled, want.ClientID = false, "changed-id"
	check(want)

	f.require(t, f.denied, http.MethodPatch, path, request(map[string]any{"clientId": "ignored"}), http.StatusForbidden)
	for _, invalid := range []any{nil, map[string]any{}, map[string]any{"enabled": nil},
		map[string]any{"clientId": nil}, map[string]any{"redirectUri": nil}, map[string]any{"clientSecret": nil},
		map[string]any{"enabled": "false"}, map[string]any{"unrecognized": true}} {
		f.require(t, f.editor, http.MethodPatch, path, request(invalid), http.StatusBadRequest)
	}
	f.require(t, f.editor, http.MethodPatch, path, map[string]any{"providers": nil}, http.StatusBadRequest)
	check(want)
	f.require(t, f.editor, http.MethodPatch, path, request(map[string]any{"clientId": "", "clientSecret": ""}), http.StatusOK)
	want.ClientID = ""
	check(want)
	f.require(t, f.editor, http.MethodPatch, path, request(map[string]any{"enabled": true}), http.StatusBadRequest)
	check(want)

	if _, err := f.db.Exec(f.ctx, `update system_settings set value='null'::jsonb where key='auth.oauth'`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPatch, path, request(map[string]any{"clientId": "new-id"}), http.StatusServiceUnavailable)
	var unchanged bool
	if err := f.db.QueryRow(f.ctx, `select value='null'::jsonb from system_settings where key='auth.oauth'`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("unreadable OAuth setting was overwritten", err)
	}
}

func TestOAuthStoredProviderNullProtectsReadAndWriteIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	server := f.origin.Config.Handler.(*Server)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "admin.config.read", "admin.config.write")
	const get, save = "/api/v1/admin/config/auth", "/api/v1/admin/config/oauth"
	update := map[string]any{"providers": map[string]any{"github": map[string]any{"clientId": "changed"}}}
	seed := func(doc any) string {
		t.Helper()
		sealed, err := server.sealSystemSetting(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values('auth.oauth',$1::jsonb)
			on conflict(key) do update set value=excluded.value`, sealed); err != nil {
			t.Fatal(err)
		}
		return oct02OAuthStored(t, f.ctx, server)
	}
	for _, fields := range []any{nil, map[string]any{"enabled": nil}, map[string]any{"clientId": nil}, map[string]any{"clientSecret": nil}, map[string]any{"redirectUri": nil}} {
		before := seed(map[string]any{"providers": map[string]any{"github": fields}})
		for _, action := range []struct {
			method, path string
			body         any
		}{
			{http.MethodGet, get, nil}, {http.MethodPut, save, update}, {http.MethodPatch, save, update},
		} {
			response := f.require(t, f.editor, action.method, action.path, action.body, http.StatusServiceUnavailable)
			if !bytes.Contains(response, []byte("OAUTH_SETTINGS_UNAVAILABLE")) || oct02OAuthStored(t, f.ctx, server) != before {
				t.Fatal("invalid stored provider must retain the original encrypted record and return the unavailable code")
			}
		}
	}
	const optional = `{"providers":{"github":{"clientId":"old","extension":{"marker":"private-extension"}},"other":{"futureNumber":9007199254740993}},"formatExtension":null}`
	seed(json.RawMessage(optional))
	for _, action := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, get, nil}, {http.MethodPut, save, update}, {http.MethodPatch, save, update},
	} {
		response := f.require(t, f.editor, action.method, action.path, action.body, http.StatusOK)
		if bytes.Contains(response, []byte("private-extension")) {
			t.Fatal("stored extensions must not enter the redacted API projection")
		}
		var saved struct {
			Providers map[string]struct {
				Extension    struct{ Marker string }
				FutureNumber json.Number
			}
			FormatExtension json.RawMessage
		}
		if err := server.openSystemSetting([]byte(oct02OAuthStored(t, f.ctx, server)), &saved); err != nil {
			t.Fatal(err)
		}
		if string(saved.FormatExtension) != "null" || saved.Providers["github"].Extension.Marker != "private-extension" || saved.Providers["other"].FutureNumber.String() != "9007199254740993" {
			t.Fatal("a supported setting update discarded stored extensions")
		}
	}
}
