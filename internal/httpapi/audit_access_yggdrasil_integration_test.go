package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAuditCMP001PrivateYggdrasilChainFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	yc, service := f.server.yggdrasilRuntimeSnapshot()
	yc.PublicBaseURL = f.origin.URL + "/api/yggdrasil/"
	yc.TextureBaseURL = f.origin.URL + "/api/yggdrasil/textures/"
	f.server.replaceYggdrasilRuntime(yc, service)
	raw := test039PNG(t, 64, 64, 77)
	file := f.upload(t, f.editor, "audit-private.png", raw)
	asset := f.asset(t, f.editor, file, "Audit private texture", "private")
	f.review(t, asset.PublicID, "approved")
	profile := f.profile(t, f.editor, "AuditPrivateA", "private")
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+profile.PublicID+"/textures", map[string]any{"skinPublicId": asset.PublicID}, 200)
	uuid := strings.ReplaceAll(profile.UUID, "-", "")
	f.require(t, "", http.MethodGet, "/api/yggdrasil/users/profiles/minecraft/"+profile.Name, nil, 204)
	f.require(t, "", http.MethodGet, "/api/yggdrasil/sessionserver/session/minecraft/profile/"+uuid+"?unsigned=false", nil, 204)
	named := f.require(t, "", http.MethodPost, "/api/yggdrasil/api/profiles/minecraft", []string{profile.Name}, 200)
	if strings.TrimSpace(string(named)) != "[]" {
		t.Fatalf("private batch identity leaked: %s", named)
	}
	f.require(t, "", http.MethodGet, asset.TextureURL, nil, 404)
	f.conditionalTexture(t, asset.TextureURL, asset.TextureHash, 404)
	assertTEST039TextureBytes(t, f.require(t, f.editor, http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
	f.require(t, f.otherEditor, http.MethodGet, asset.TextureURL, nil, 404)
	// Sharing bytes does not grant access to a private identity, but a current
	// independently published asset is a valid public reference to those bytes.
	publicFile := f.upload(t, f.otherEditor, "audit-shared.png", raw)
	public := f.asset(t, f.otherEditor, publicFile, "Audit public shared texture", "public")
	f.require(t, "", http.MethodGet, asset.TextureURL, nil, 404) // still unreviewed
	f.review(t, public.PublicID, "approved")
	assertTEST039TextureBytes(t, f.require(t, "", http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
	publicProfile := f.profile(t, f.otherEditor, "AuditPublicB", "public")
	f.require(t, f.otherEditor, http.MethodPut, "/api/v1/users/me/player-profiles/"+publicProfile.PublicID+"/textures", map[string]any{"skinPublicId": public.PublicID}, 200)
	var result yggdrasilProfile
	if err := json.Unmarshal(f.require(t, "", http.MethodGet, "/api/yggdrasil/sessionserver/session/minecraft/profile/"+strings.ReplaceAll(publicProfile.UUID, "-", "")+"?unsigned=false", nil, 200), &result); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Properties[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	var textures yggdrasilTexturePayload
	if err = json.Unmarshal(decoded, &textures); err != nil {
		t.Fatal(err)
	}
	if textures.Textures["SKIN"].URL != f.origin.URL+asset.TextureURL || result.Properties[0].Signature == "" {
		t.Fatal("public signed texture protocol regressed")
	}
	f.require(t, f.otherEditor, http.MethodPut, "/api/v1/users/me/player-profiles/"+publicProfile.PublicID, map[string]any{"visibility": "private"}, 200)
	f.require(t, "", http.MethodGet, "/api/yggdrasil/users/profiles/minecraft/"+publicProfile.Name, nil, 204)
	f.require(t, "", http.MethodGet, "/api/yggdrasil/sessionserver/session/minecraft/profile/"+strings.ReplaceAll(publicProfile.UUID, "-", "")+"?unsigned=false", nil, 204)
	f.require(t, f.otherEditor, http.MethodDelete, "/api/v1/skins/"+public.PublicID, nil, 200)
	f.require(t, "", http.MethodGet, asset.TextureURL, nil, 404)
	f.conditionalTexture(t, asset.TextureURL, asset.TextureHash, 404)
	assertTEST039TextureBytes(t, f.require(t, f.editor, http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
}
