package httpapi

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUserConnectionCursorIsOpaqueScopedAndRejectsLegacyPages(t *testing.T) {
	request, err := parseUserConnectionPageRequest(url.Values{"limit": {"24"}}, 42, "followers")
	if err != nil || request.Limit != 24 || request.Cursor != nil {
		t.Fatalf("first request=%+v err=%v", request, err)
	}
	want := userConnectionPageCursor{
		Version: userConnectionCursorVersion, Scope: request.Scope,
		CreatedAt: time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC), ConnectionUserID: 99,
	}
	raw := encodeUserConnectionPageCursor(want)
	next, err := parseUserConnectionPageRequest(url.Values{"limit": {"24"}, "cursor": {raw}}, 42, "followers")
	if err != nil || next.Cursor == nil || next.Cursor.CreatedAt != want.CreatedAt || next.Cursor.ConnectionUserID != want.ConnectionUserID {
		t.Fatalf("cursor round trip=%+v err=%v", next.Cursor, err)
	}
	for name, values := range map[string]url.Values{
		"legacy page":      {"page": {"2"}},
		"legacy page size": {"pageSize": {"24"}},
		"foreign user":     {"limit": {"24"}, "cursor": {raw}},
		"foreign type":     {"limit": {"24"}, "cursor": {raw}},
		"foreign limit":    {"limit": {"25"}, "cursor": {raw}},
		"malformed":        {"cursor": {"not-base64"}},
	} {
		userID, connectionType := int64(42), "followers"
		if name == "foreign user" {
			userID = 43
		}
		if name == "foreign type" {
			connectionType = "following"
		}
		if _, parseErr := parseUserConnectionPageRequest(values, userID, connectionType); parseErr == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestUserConnectionsHandlerHasNoCountOrOffsetContract(t *testing.T) {
	source, err := os.ReadFile("profile_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func (s *Server) userConnections")
	if start < 0 {
		t.Fatal("userConnections handler not found")
	}
	handler := text[start:]
	for _, forbidden := range []string{"countQuery", "offset :=", " limit $2 offset", `"total": total`, `"page": page`} {
		if strings.Contains(handler, forbidden) {
			t.Fatalf("userConnections retains deep-page contract %q", forbidden)
		}
	}
	for _, required := range []string{"parseUserConnectionPageRequest", "loadUserConnectionPage", `"hasMore"`, `"nextCursor"`} {
		if !strings.Contains(handler, required) {
			t.Fatalf("userConnections is missing cursor contract %q", required)
		}
	}
}
