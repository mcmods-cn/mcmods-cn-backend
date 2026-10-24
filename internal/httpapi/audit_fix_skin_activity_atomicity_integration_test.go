package httpapi

import (
	"net/http"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func TestAuditFixCMP003SkinProfileWardrobeAndTextureActivityAtomicity(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "atomic-shared.png", test039PNG(t, 64, 64, 104))
	// Reuse a legitimate shared canonical blob: a rejected second creation must
	// not delete the first asset's pixels or any of its business references.
	control := f.asset(t, f.editor, file, "Atomic shared control", "public")
	f.review(t, control.PublicID, "approved")
	create := skinAssetCreateRequest{FileID: file, Kind: "skin", Model: "default", Name: "Atomic target", Visibility: "public", DefaultLocale: "en-US", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Atomic target"}}}
	fault := func(token, method, path string, body any) {
		t.Helper()
		auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[token], true)
		before := f.facts(t)
		raw := f.require(t, token, method, path, body, 500)
		auditFixRequireSpecificActivityError(t, raw, "failed to persist skin library activity")
		f.unchanged(t, before)
		auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[token], false)
	}
	fault(f.editor, http.MethodPost, "/api/v1/skins", create)
	target := decodeTEST022Data[playerTextureResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/skins", create, 201))
	f.review(t, target.PublicID, "approved")
	fault(f.editor, http.MethodPut, "/api/v1/skins/"+target.PublicID, map[string]any{"name": "Atomic edited target"})
	f.require(t, f.editor, http.MethodPut, "/api/v1/skins/"+target.PublicID, map[string]any{"name": "Atomic edited target"}, 200)
	profilePayload := playerProfileCreateRequest{Name: "AtomicPlayerA", Visibility: "private"}
	fault(f.editor, http.MethodPost, "/api/v1/users/me/player-profiles", profilePayload)
	profile := decodeTEST022Data[playerProfileResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/player-profiles", profilePayload, 201))
	profilePath := "/api/v1/users/me/player-profiles/" + profile.PublicID
	fault(f.editor, http.MethodPut, profilePath, map[string]any{"name": "AtomicPlayerB"})
	f.require(t, f.editor, http.MethodPut, profilePath, map[string]any{"name": "AtomicPlayerB"}, 200)
	wardrobePath := "/api/v1/users/me/skin-wardrobe/" + target.PublicID
	fault(f.otherEditor, http.MethodPut, wardrobePath, nil)
	f.require(t, f.otherEditor, http.MethodPut, wardrobePath, nil, 200)
	fault(f.otherEditor, http.MethodDelete, wardrobePath, nil)
	f.require(t, f.otherEditor, http.MethodDelete, wardrobePath, nil, 200)
	texturePayload := map[string]any{"skinPublicId": target.PublicID}
	fault(f.editor, http.MethodPut, profilePath+"/textures", texturePayload)
	f.require(t, f.editor, http.MethodPut, profilePath+"/textures", texturePayload, 200)
	fault(f.editor, http.MethodDelete, profilePath, nil)
	f.require(t, f.editor, http.MethodDelete, profilePath, nil, 200)
	fault(f.editor, http.MethodDelete, "/api/v1/skins/"+target.PublicID, nil)
	f.require(t, f.editor, http.MethodDelete, "/api/v1/skins/"+target.PublicID, nil, 200)
	// The original shared reference remains active after both failed and valid
	// deletion of the independently created second asset.
	f.require(t, "", http.MethodGet, "/api/v1/skins/"+control.PublicID, nil, 200)
	var creates, edits, deletes int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where action_id=$2),count(*) filter(where action_id=$3),count(*) filter(where action_id=$4)
 from activity_event_outbox where user_id=$1`, f.userIDs[f.editor], activity.ActionCreate, activity.ActionEdit, activity.ActionDelete).Scan(&creates, &edits, &deletes); err != nil {
		t.Fatal(err)
	}
	if creates != 3 || edits != 3 || deletes != 2 {
		t.Fatalf("successful durable actions create/edit/delete=%d/%d/%d want3/3/2", creates, edits, deletes)
	}
}
