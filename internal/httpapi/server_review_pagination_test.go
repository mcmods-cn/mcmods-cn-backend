package httpapi

import (
	"net/url"
	"testing"
	"time"
)

func TestServerReviewCursorRoundTripBindsStatusAndLimit(t *testing.T) {
	request, err := parseServerReviewPageRequest(url.Values{"status": {"pending"}, "limit": {"40"}})
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeServerReviewPageCursor(serverReviewPageCursor{
		Version: serverReviewCursorVersion, Scope: request.Scope,
		CreatedAt: time.Unix(100, 123000000).UTC(), ID: 77,
	})
	next, err := parseServerReviewPageRequest(url.Values{
		"status": {"pending"}, "limit": {"40"}, "cursor": {cursor},
	})
	if err != nil || next.Cursor == nil || next.Cursor.ID != 77 ||
		!next.Cursor.CreatedAt.Equal(time.Unix(100, 123000000).UTC()) {
		t.Fatalf("round trip request=%+v err=%v", next, err)
	}
	for name, values := range map[string]url.Values{
		"status": {"status": {"approved"}, "limit": {"40"}, "cursor": {cursor}},
		"limit":  {"status": {"pending"}, "limit": {"50"}, "cursor": {cursor}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, parseErr := parseServerReviewPageRequest(values); parseErr == nil {
				t.Fatal("cursor was reusable outside its review scope")
			}
		})
	}
}

func TestServerReviewPageRequestRejectsInvalidInputs(t *testing.T) {
	for name, values := range map[string]url.Values{
		"status":        {"status": {"unknown"}},
		"zero limit":    {"limit": {"0"}},
		"large limit":   {"limit": {"101"}},
		"invalid limit": {"limit": {"many"}},
		"cursor":        {"cursor": {"not-base64"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseServerReviewPageRequest(values); err == nil {
				t.Fatal("invalid server review page request was accepted")
			}
		})
	}
}
