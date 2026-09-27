package httpapi

import (
	"net/url"
	"strings"
	"testing"
)

func TestModRevisionHistoryCursorIsBoundToProjectAndLimit(t *testing.T) {
	request, err := parseModRevisionHistoryPageRequest(url.Values{"limit": {"25"}}, "project01")
	if err != nil || request.Limit != 25 || request.Cursor != nil {
		t.Fatalf("initial request=%+v err=%v", request, err)
	}
	raw := encodeModRevisionHistoryPageCursor(modRevisionHistoryPageCursor{
		Version: modRevisionHistoryCursorVersion, Scope: request.Scope, RevisionNo: 120,
	})
	parsed, err := parseModRevisionHistoryPageRequest(url.Values{"limit": {"25"}, "cursor": {raw}}, "project01")
	if err != nil || parsed.Cursor == nil || parsed.Cursor.RevisionNo != 120 {
		t.Fatalf("parsed request=%+v err=%v", parsed, err)
	}
	if _, err = parseModRevisionHistoryPageRequest(url.Values{"limit": {"25"}, "cursor": {raw}}, "project02"); err == nil {
		t.Fatal("cursor crossed project scope")
	}
	if _, err = parseModRevisionHistoryPageRequest(url.Values{"limit": {"50"}, "cursor": {raw}}, "project01"); err == nil {
		t.Fatal("cursor crossed page-size scope")
	}
	if _, err = parseModRevisionHistoryPageRequest(url.Values{"limit": {"101"}}, "project01"); err == nil {
		t.Fatal("oversized history page was accepted")
	}
	if _, err = decodeModRevisionHistoryPageCursor(strings.Repeat("a", modRevisionHistoryMaxCursorSize+1), request.Scope); err == nil {
		t.Fatal("oversized cursor was accepted")
	}
}
