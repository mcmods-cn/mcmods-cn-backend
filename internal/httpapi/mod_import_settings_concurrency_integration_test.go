package httpapi

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestImportSourceClearedCredentialsStayClearedIntegration(t *testing.T) {
	const placeholder = "synthetic-local-import-setting"
	for _, key := range []string{"MODRINTH_TOKEN", "CURSEFORGE_API_KEY", "GITHUB_TOKEN"} {
		t.Setenv(key, placeholder)
	}
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "admin.config.read", "admin.config.write")
	const path = "/api/v1/admin/config/mod-imports"
	read := func() map[string]any {
		raw := f.require(t, f.editor, http.MethodGet, path, nil, http.StatusOK)
		if bytes.Contains(raw, []byte(placeholder)) {
			t.Fatal("response contains the synthetic credential")
		}
		return decodeTEST022Data[map[string]any](t, raw)
	}
	body := read()
	for _, row := range []struct{ provider, has, clear string }{
		{"modrinth", "hasToken", "clearToken"}, {"curseforge", "hasApiKey", "clearApiKey"}, {"github", "hasToken", "clearToken"},
	} {
		provider := body[row.provider].(map[string]any)
		if provider[row.has] != true {
			t.Fatalf("missing initial credential for %s", row.provider)
		}
		provider[row.clear] = true
	}
	f.require(t, f.editor, http.MethodPut, path, body, http.StatusOK)
	again := read()
	for _, row := range []struct{ provider, has string }{{"modrinth", "hasToken"}, {"curseforge", "hasApiKey"}, {"github", "hasToken"}} {
		if again[row.provider].(map[string]any)[row.has] == true {
			t.Errorf("cleared %s credential reappeared on subsequent read", row.provider)
		}
	}
}

func TestImportSourceRequiresCurrentRevisionAcrossEditorsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "admin.config.read", "admin.config.write")
	}
	const path = "/api/v1/admin/config/mod-imports"
	read := func(token string) map[string]any {
		return decodeTEST022Data[map[string]any](t, f.require(t, token, http.MethodGet, path, nil, http.StatusOK))
	}
	a, b := read(f.editor), read(f.otherEditor)
	oldRev, _ := a["revision"].(string)
	a["requestTimeoutSeconds"] = 43
	got := decodeTEST022Data[map[string]any](t, f.require(t, f.editor, http.MethodPut, path, a, http.StatusOK))
	b["userAgent"] = "synthetic-second-editor"
	f.require(t, f.otherEditor, http.MethodPut, path, b, http.StatusConflict)
	current := read(f.otherEditor)
	rev, _ := got["revision"].(string)
	if len(rev) != 64 || len(oldRev) != 64 || rev == oldRev || current["revision"] != rev || strings.ToLower(rev) != rev ||
		current["requestTimeoutSeconds"] != float64(43) || current["userAgent"] != a["userAgent"] {
		t.Fatal("stale writer altered the saved settings or the revision did not round trip")
	}
	delete(current, "revision")
	f.require(t, f.otherEditor, http.MethodPut, path, current, http.StatusPreconditionRequired)
}
