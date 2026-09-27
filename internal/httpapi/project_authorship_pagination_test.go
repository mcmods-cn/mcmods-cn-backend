package httpapi

import (
	"net/url"
	"testing"
	"time"
)

func TestProjectAuthorshipCursorRoundTripBindsStatusIdentityAndPermissions(t *testing.T) {
	request, err := parseProjectAuthorshipPageRequest(url.Values{
		"status": {"pending"}, "limit": {"25"},
	}, 42, true, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodeProjectAuthorshipPageCursor(projectAuthorshipPageCursor{
		Version: projectAuthorshipCursorVersion,
		Scope:   request.Scope, CreatedAt: time.Unix(100, 123000000).UTC(), ID: 77,
	})
	values := url.Values{"status": {"pending"}, "limit": {"25"}, "cursor": {encoded}}
	next, err := parseProjectAuthorshipPageRequest(values, 42, true, false)
	if err != nil || next.Cursor == nil || next.Cursor.ID != 77 || !next.Cursor.CreatedAt.Equal(time.Unix(100, 123000000).UTC()) {
		t.Fatalf("round trip request=%+v err=%v", next, err)
	}
	for name, mutate := range map[string]func(){
		"status":      func() { values.Set("status", "approved") },
		"limit":       func() { values.Set("limit", "20") },
		"identity":    func() {},
		"permissions": func() {},
	} {
		t.Run(name, func(t *testing.T) {
			copyValues := url.Values{}
			for key, entries := range values {
				copyValues[key] = append([]string(nil), entries...)
			}
			values = copyValues
			values.Set("status", "pending")
			values.Set("limit", "25")
			mutate()
			userID, authors, teams := int64(42), true, false
			if name == "identity" {
				userID = 99
			}
			if name == "permissions" {
				authors, teams = false, true
			}
			if _, err := parseProjectAuthorshipPageRequest(values, userID, authors, teams); err == nil {
				t.Fatal("cursor was reusable outside its review scope")
			}
		})
	}
}

func TestProjectAuthorshipPageRequestRejectsInvalidInputs(t *testing.T) {
	for name, values := range map[string]url.Values{
		"status":        {"status": {"unknown"}},
		"zero limit":    {"limit": {"0"}},
		"large limit":   {"limit": {"101"}},
		"invalid limit": {"limit": {"many"}},
		"cursor":        {"cursor": {"not-base64"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseProjectAuthorshipPageRequest(values, 1, true, true); err == nil {
				t.Fatal("invalid review page request was accepted")
			}
		})
	}
}
