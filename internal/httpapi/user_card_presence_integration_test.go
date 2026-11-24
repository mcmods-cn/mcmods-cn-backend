package httpapi

import (
	"io"
	"net/http"
	"testing"
)

func TestPublicUserCardUsesCurrentPresencePreferenceIntegration(t *testing.T) {
	f := newTEST022Fixture(t, true)
	const settingsPath = "/api/v1/users/me/profile-settings"
	cardPath := "/api/v1/users/" + f.publicIDs[f.editor] + "/card"
	if !f.servers[1].cache.Config().UserCardCacheEnabled {
		t.Fatal("enable user-card caching for the public preference integration fixture")
	}
	readCard := func(path string, wantStatus int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origins[1].URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := f.origins[1].Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil || response.StatusCode != wantStatus {
			t.Fatalf("GET %s status=%d want=%d err=%v", path, response.StatusCode, wantStatus, err)
		}
		if policy := response.Header.Get("Cache-Control"); policy != "private, no-store" {
			t.Fatalf("card cache policy=%q", policy)
		}
		return body
	}

	profilePath := "/api/v1/users/" + f.publicIDs[f.editor] + "/profile"
	f.requireAt(t, 0, f.editor, http.MethodPost, "/api/v1/site/presence", map[string]string{}, http.StatusOK)
	for _, state := range []struct {
		show bool
		want publicOnlineStatus
	}{
		{true, publicOnlineStatusOnline},
		{false, publicOnlineStatusHidden},
		{true, publicOnlineStatusOnline},
	} {
		f.requireAt(t, 0, f.editor, http.MethodPut, settingsPath, map[string]bool{"showOnlineStatus": state.show}, http.StatusOK)
		card := decodeTEST022Data[publicUserCard](t, readCard(cardPath, http.StatusOK))
		profile := decodeTEST022Data[struct{ OnlineStatus publicOnlineStatus }](t, f.requireAt(t, 1, "", http.MethodGet, profilePath, nil, http.StatusOK))
		if card.ID != f.publicIDs[f.editor] || card.Username == "" || len(card.Statistics) != 6 || card.OnlineStatus != state.want || profile.OnlineStatus != state.want {
			t.Fatalf("show=%t card=%s profile=%s want=%s", state.show, card.OnlineStatus, profile.OnlineStatus, state.want)
		}
	}
	// Instance 1 already has a warm card. Each successful settings change at
	// instance 0 must control exactly which slots anonymous readers see.
	for _, selected := range []string{"active_days", "", "follower_count"} {
		slots := []string{selected, "", "", "", "", ""}
		f.requireAt(t, 0, f.editor, http.MethodPut, settingsPath, map[string]any{"publicCardStatSlots": slots}, http.StatusOK)
		card := decodeTEST022Data[publicUserCard](t, readCard(cardPath, http.StatusOK))
		if len(card.Statistics) != len(slots) {
			t.Fatalf("card has %d slots", len(card.Statistics))
		}
		for i, key := range slots {
			stat := card.Statistics[i]
			if key == "" && stat != nil || key != "" && (stat == nil || stat.Key != key) {
				t.Fatalf("after selecting %q at instance 0, anonymous slot %d at instance 1 = %+v", selected, i, stat)
			}
		}
	}
	readCard("/api/v1/users/not-a-user/card", http.StatusBadRequest)
	if _, err := f.db.Exec(f.ctx, "update users set status='deleted' where id=$1", f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	readCard(cardPath, http.StatusNotFound)
}
