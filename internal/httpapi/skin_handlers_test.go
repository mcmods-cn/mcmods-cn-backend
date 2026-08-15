package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestParsePlayerTextureUpdatesPatchSemantics(t *testing.T) {
	t.Parallel()
	updates, err := parsePlayerTextureUpdates(playerTextureRequest{
		SkinPublicID: json.RawMessage("null"),
		CapePublicID: json.RawMessage(`"abc234567"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 || updates[0].Kind != "skin" || updates[0].AssetPublicID != "" ||
		updates[1].Kind != "cape" || updates[1].AssetPublicID != "abc234567" {
		t.Fatalf("unexpected updates: %#v", updates)
	}

	updates, err = parsePlayerTextureUpdates(playerTextureRequest{SkinPublicID: json.RawMessage(`"abc234567"`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Kind != "skin" {
		t.Fatalf("an omitted cape must remain unchanged: %#v", updates)
	}
}

func TestParsePlayerTextureUpdatesRejectsInvalidOrEmptyPatches(t *testing.T) {
	t.Parallel()
	for _, request := range []playerTextureRequest{
		{SkinPublicID: json.RawMessage(`"not-an-id"`)},
		{CapePublicID: json.RawMessage(`123`)},
		{},
	} {
		if _, err := parsePlayerTextureUpdates(request); err == nil {
			t.Fatalf("expected request to fail: %#v", request)
		}
	}
}

func TestSkinAssetJSONCanonicalContract(t *testing.T) {
	t.Parallel()
	record := skinAssetRecord{
		PublicID: "abc234567", OwnerID: 7, OwnerPublicID: "def234567", OwnerName: "owner",
		BlobHash: "hash", Kind: "skin", Model: "default", Name: "Example",
		Visibility: "public", ReviewStatus: "approved", Status: "active",
	}
	payload := skinAssetJSON(record, 7)
	if payload["name"] != "Example" || payload["textureHash"] != "hash" || payload["canEdit"] != true || payload["canUse"] != true {
		t.Fatalf("canonical skin fields are incomplete: %#v", payload)
	}
	for _, removedAlias := range []string{"displayName", "hash", "isOwner", "ownerId"} {
		if _, exists := payload[removedAlias]; exists {
			t.Fatalf("legacy alias %q is still present: %#v", removedAlias, payload)
		}
	}
}

func TestSkinRoutesAreRegistered(t *testing.T) {
	t.Parallel()
	server := &Server{mux: http.NewServeMux()}
	server.routes()
	requests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/skin-service"},
		{http.MethodGet, "/api/v1/skins"},
		{http.MethodPost, "/api/v1/skins"},
		{http.MethodGet, "/api/v1/skins/abc234567"},
		{http.MethodPut, "/api/v1/users/me/skin-wardrobe/abc234567"},
		{http.MethodPost, "/api/v1/users/me/player-profiles"},
		{http.MethodPut, "/api/v1/users/me/player-profiles/abc234567/textures"},
		{http.MethodGet, "/api/v1/users/42/player-profiles"},
		{http.MethodGet, "/api/v1/player-profiles/abc234567"},
		{http.MethodDelete, "/api/v1/users/me/launcher-sessions/12"},
	}
	for _, request := range requests {
		req, err := http.NewRequest(request.method, request.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, pattern := server.mux.Handler(req)
		if pattern == "" {
			t.Errorf("route is not registered: %s %s", request.method, request.path)
		}
	}
}
