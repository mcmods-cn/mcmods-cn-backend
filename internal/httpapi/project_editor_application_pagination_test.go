package httpapi

import (
	"net/url"
	"testing"
	"time"
)

func TestProjectEditorApplicationCursorRoundTripBindsStatusAndLimit(t *testing.T) {
	request, err := parseProjectEditorApplicationPageRequest(url.Values{
		"status": {"approved"},
		"limit":  {"40"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeProjectEditorApplicationPageCursor(projectEditorApplicationPageCursor{
		Version:   projectEditorApplicationCursorVersion,
		Scope:     request.Scope,
		CreatedAt: time.Date(2026, 8, 23, 12, 0, 0, 123, time.UTC),
		ID:        42,
	})
	next, err := parseProjectEditorApplicationPageRequest(url.Values{
		"status": {"approved"},
		"limit":  {"40"},
		"cursor": {cursor},
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Cursor == nil || next.Cursor.ID != 42 || !next.Cursor.CreatedAt.Equal(time.Date(2026, 8, 23, 12, 0, 0, 123, time.UTC)) {
		t.Fatalf("decoded cursor=%+v", next.Cursor)
	}
	for _, values := range []url.Values{
		{"status": {"pending"}, "limit": {"40"}, "cursor": {cursor}},
		{"status": {"approved"}, "limit": {"41"}, "cursor": {cursor}},
	} {
		if _, parseErr := parseProjectEditorApplicationPageRequest(values); parseErr == nil {
			t.Fatalf("cross-scope cursor accepted for %v", values)
		}
	}
}

func TestProjectEditorApplicationPageRequestRejectsInvalidInputs(t *testing.T) {
	valid, err := parseProjectEditorApplicationPageRequest(nil)
	if err != nil {
		t.Fatal(err)
	}
	if valid.Status != "pending" || valid.Limit != 50 || valid.Cursor != nil {
		t.Fatalf("default request=%+v", valid)
	}
	for _, values := range []url.Values{
		{"status": {"unknown"}},
		{"limit": {"0"}},
		{"limit": {"101"}},
		{"limit": {"abc"}},
		{"cursor": {"not-base64"}},
		{"status": {"pending", "approved"}},
		{"extra": {"value"}},
	} {
		if _, parseErr := parseProjectEditorApplicationPageRequest(values); parseErr == nil {
			t.Fatalf("invalid request accepted: %v", values)
		}
	}
}
