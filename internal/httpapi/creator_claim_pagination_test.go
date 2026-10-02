package httpapi

import (
	"net/url"
	"testing"
	"time"
)

func TestParseCreatorClaimPageRequest(t *testing.T) {
	request, err := parseCreatorClaimPageRequest(url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if request.Limit != 50 || request.Cursor != nil || request.Scope == "" {
		t.Fatalf("default request=%+v", request)
	}

	request, err = parseCreatorClaimPageRequest(url.Values{"limit": {"100"}})
	if err != nil {
		t.Fatal(err)
	}
	anchor := creatorClaimPageCursor{
		Version: creatorClaimCursorVersion,
		Scope:   request.Scope, CreatedAt: time.Date(2026, 8, 23, 4, 5, 6, 0, time.FixedZone("test", 8*60*60)), ID: 987,
	}
	raw := encodeCreatorClaimPageCursor(anchor)
	request, err = parseCreatorClaimPageRequest(url.Values{"limit": {"100"}, "cursor": {raw}})
	if err != nil {
		t.Fatal(err)
	}
	if request.Cursor == nil || request.Cursor.ID != anchor.ID || !request.Cursor.CreatedAt.Equal(anchor.CreatedAt) ||
		request.Cursor.CreatedAt.Location() != time.UTC {
		t.Fatalf("round-tripped cursor=%+v", request.Cursor)
	}
}

func TestParseCreatorClaimPageRequestRejectsUnstablePages(t *testing.T) {
	validRequest, err := parseCreatorClaimPageRequest(url.Values{"limit": {"50"}})
	if err != nil {
		t.Fatal(err)
	}
	validCursor := encodeCreatorClaimPageCursor(creatorClaimPageCursor{
		Version: creatorClaimCursorVersion,
		Scope:   validRequest.Scope, CreatedAt: time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC), ID: 42,
	})

	tests := []struct {
		name   string
		values url.Values
	}{
		{name: "unknown key", values: url.Values{"offset": {"1"}}},
		{name: "repeated limit", values: url.Values{"limit": {"50", "51"}}},
		{name: "zero limit", values: url.Values{"limit": {"0"}}},
		{name: "oversized limit", values: url.Values{"limit": {"101"}}},
		{name: "non numeric limit", values: url.Values{"limit": {"many"}}},
		{name: "malformed cursor", values: url.Values{"cursor": {"not-base64!"}}},
		{name: "cursor from another page size", values: url.Values{"limit": {"100"}, "cursor": {validCursor}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, parseErr := parseCreatorClaimPageRequest(test.values); parseErr == nil {
				t.Fatalf("accepted query %v", test.values)
			}
		})
	}
}
