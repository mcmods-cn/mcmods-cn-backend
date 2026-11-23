package httpapi

import (
	"io"
	"net/http"
	"testing"
)

func TestSkinMetadataResponsesUsePrivateCachePolicyIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "metadata-cache.png", test039PNG(t, 64, 64, 105))
	publicAsset := f.asset(t, f.editor, file, "Public texture", "public")
	f.review(t, publicAsset.PublicID, "approved")
	privateAsset := f.asset(t, f.editor, file, "Private texture", "private")
	publicProfile := f.profile(t, f.editor, "CachePlayer", "public")
	privateProfile := f.profile(t, f.editor, "HiddenPlayer", "private")
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+publicProfile.PublicID+"/textures",
		map[string]string{"skinPublicId": publicAsset.PublicID}, http.StatusOK)

	request := func(t *testing.T, path string, owner bool, status int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origin.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if owner {
			req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: f.editor})
		}
		response, err := f.origin.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil || response.StatusCode != status {
			t.Fatalf("GET %s status=%d want=%d owner=%v err=%v", path, response.StatusCode, status, owner, err)
		}
		if policy := response.Header.Get("Cache-Control"); policy != "private, no-store" {
			t.Fatalf("GET %s cache policy=%q owner=%v", path, policy, owner)
		}
		return data
	}

	for _, owner := range []bool{true, false} {
		publicList := decodeTEST022Data[struct{ Items []playerTextureResponse }](t, request(t, "/api/v1/skins", owner, http.StatusOK))
		if len(publicList.Items) != 1 || publicList.Items[0].PublicID != publicAsset.PublicID || publicList.Items[0].CanEdit != owner {
			t.Fatalf("unexpected catalog metadata for owner=%v", owner)
		}
		publicDetail := decodeTEST022Data[playerTextureResponse](t, request(t, "/api/v1/skins/"+publicAsset.PublicID, owner, http.StatusOK))
		if publicDetail.PublicID != publicAsset.PublicID || publicDetail.CanEdit != owner {
			t.Fatalf("unexpected public skin detail for owner=%v", owner)
		}
		profiles := decodeTEST022Data[struct{ Items []playerProfileResponse }](t, request(t, "/api/v1/users/"+publicProfile.Owner.ID+"/player-profiles", owner, http.StatusOK))
		profileCount, privateStatus := 1, http.StatusNotFound
		if owner {
			profileCount, privateStatus = 2, http.StatusOK
		}
		if len(profiles.Items) != profileCount {
			t.Fatalf("profile count=%d want=%d owner=%v", len(profiles.Items), profileCount, owner)
		}
		detail := decodeTEST022Data[playerProfileResponse](t, request(t, "/api/v1/player-profiles/"+publicProfile.PublicID, owner, http.StatusOK))
		if detail.Skin == nil || detail.Skin.PublicID != publicAsset.PublicID || detail.Skin.CanEdit != owner {
			t.Fatalf("unexpected equipped skin for owner=%v", owner)
		}
		request(t, "/api/v1/skins/"+privateAsset.PublicID, owner, privateStatus)
		request(t, "/api/v1/player-profiles/"+privateProfile.PublicID, owner, privateStatus)
	}
}
