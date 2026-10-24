package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in short-lived service for a real frontend/API/PG private texture journey.
// The source provider and all accounts are synthetic; asset creation, independent
// approval, cookies and actual PNG bytes use the product handlers unchanged.
func TestAuditLivePrivateSkinBrowserFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_PRIVATE_SKIN") != "1" {
		t.Skip("opt-in owned real private-skin browser fixture")
	}
	dir := os.Getenv("MCMODS_AUDIT_PRIVATE_SKIN_DIR")
	authPath := os.Getenv("MCMODS_AUDIT_PRIVATE_SKIN_AUTH")
	if !filepath.IsAbs(dir) || !filepath.IsAbs(authPath) {
		t.Fatal("requires absolute owned runtime and private auth paths")
	}
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal("cannot identify source commit")
	}
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil || len(strings.TrimSpace(string(status))) != 0 {
		t.Fatal("fixture requires clean immutable checkout")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal("runtime directory must be a new owned directory")
	}
	base := newAuditFixGRFixture(t, 10*time.Minute)
	if base.server.cfg.FrontendOrigin != "http://127.0.0.1:3311" {
		t.Fatal("fixture requires exact configured frontend origin http://127.0.0.1:3311")
	}
	f := test039Fixture{test038Fixture{test037Fixture{test013Fixture: base.test013Fixture, server: base.server, provider: base.provider}}}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "skin.library.upload", "skin.profile.create", "skin.profile.write")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "skin.admin")
	claims, err := resolveUsersRootPermissionsQuery(f.ctx, f.db, []int64{f.userIDs[f.editor]})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range claims[f.userIDs[f.editor]].Permissions {
		if rule.Allow && (rule.Code == "skin.admin" || rule.Code == "admin" || rule.Code == "*") {
			t.Fatal("fixture owner must be an ordinary user")
		}
	}
	yc, service := f.server.yggdrasilRuntimeSnapshot()
	yc.PublicBaseURL = f.origin.URL + "/api/yggdrasil/"
	yc.TextureBaseURL = f.origin.URL + "/api/yggdrasil/textures/"
	f.server.replaceYggdrasilRuntime(yc, service)
	png := test039PNG(t, 64, 64, 77)
	file := f.upload(t, f.editor, "owned-private-browser.png", png)
	asset := f.asset(t, f.editor, file, "AuditPrivateBrowserSkin", "private")
	f.review(t, asset.PublicID, "approved")
	f.require(t, "", http.MethodGet, asset.TextureURL, nil, 404)
	f.require(t, f.otherEditor, http.MethodGet, asset.TextureURL, nil, 404)
	assertTEST039TextureBytes(t, f.require(t, f.editor, http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
	request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origin.URL+asset.TextureURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "http://127.0.0.1:3311")
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: f.editor})
	response, err := f.origin.Client().Do(request)
	if err != nil {
		t.Fatal("owner cookie texture request failed")
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Access-Control-Allow-Origin") != "http://127.0.0.1:3311" || response.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("owner cookie texture/CORS contract failed")
	}
	auth, err := json.Marshal(map[string]any{"ownerToken": f.editor, "otherToken": f.otherEditor})
	if err != nil {
		t.Fatal(err)
	}
	private, err := os.OpenFile(authPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("auth file must be new and private")
	}
	if _, err = private.Write(auth); err != nil {
		private.Close()
		t.Fatal("private auth write failed")
	}
	if err = private.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err := json.MarshalIndent(map[string]any{"apiOrigin": f.origin.URL, "frontendOrigin": "http://127.0.0.1:3311", "authPath": authPath, "authCookieName": authSessionCookieName, "skinPublicId": asset.PublicID, "textureHash": asset.TextureHash, "textureUrl": asset.TextureURL, "skinName": "AuditPrivateBrowserSkin", "sourceCommit": strings.TrimSpace(string(head)), "sourceTreeClean": true, "scope": "Real API+PG+browser; synthetic OSS provider; ordinary owner, approved private asset,64x64PNG; not real cloud or launcher acceptance."}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime.json"), runtime, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("PRIVATE_SKIN_READY runtime=%s source=%s ownerAdmin=false png=64x64 anonymous=404 other=404 ownerCookie=200; credentials stored privately", filepath.Join(dir, "runtime.json"), strings.TrimSpace(string(head)))
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	wait, cancel := context.WithTimeout(f.ctx, 8*time.Minute)
	defer cancel()
	for {
		select {
		case <-wait.Done():
			t.Fatal("owned browser fixture timed out; no frontend acceptance inferred")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(dir, "done.flag")); err == nil {
				t.Log("Owned browser consumer completed; closing owned fixture")
				return
			} else if !os.IsNotExist(err) {
				t.Fatal("cannot inspect owned completion marker")
			}
		}
	}
}
