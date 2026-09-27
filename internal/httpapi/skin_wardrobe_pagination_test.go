package httpapi

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSkinWardrobePageRequestIsStrictAndScopeBound(t *testing.T) {
	request, err := parseSkinWardrobePageRequest(url.Values{
		"kind": {"skin"}, "model": {"slim"}, "limit": {"75"},
	}, 42)
	if err != nil || request.Limit != 75 || request.Kind != "skin" || request.Model != "slim" {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	anchor := skinWardrobePageRow{AddedAt: time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC)}
	anchor.Asset.ID = 91
	cursor := skinWardrobeNextCursor(request, anchor)
	withCursor := url.Values{
		"kind": {"skin"}, "model": {"slim"}, "limit": {"75"}, "cursor": {cursor},
	}
	parsed, err := parseSkinWardrobePageRequest(withCursor, 42)
	if err != nil || parsed.Cursor == nil || parsed.Cursor.AssetID != 91 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	for name, values := range map[string]url.Values{
		"foreign user":  withCursor,
		"foreign kind":  {"kind": {"cape"}, "limit": {"75"}, "cursor": {cursor}},
		"legacy offset": {"offset": {"100"}},
		"legacy page":   {"page": {"2"}},
		"unknown":       {"include": {"all"}},
		"duplicate":     {"limit": {"50", "75"}},
		"large limit":   {"limit": {"101"}},
		"bad kind":      {"kind": {"hat"}},
		"bad model":     {"model": {"wide"}},
	} {
		t.Run(name, func(t *testing.T) {
			userID := int64(42)
			if name == "foreign user" {
				userID = 43
			}
			if _, parseErr := parseSkinWardrobePageRequest(values, userID); parseErr == nil {
				t.Fatalf("expected rejection for %v", values)
			}
		})
	}
	unknown := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"scope":"` + request.Scope + `","addedAt":"2026-08-22T01:02:03Z","assetId":91,"extra":true}`))
	if _, err = parseSkinWardrobePageRequest(url.Values{"kind": {"skin"}, "model": {"slim"}, "limit": {"75"}, "cursor": {unknown}}, 42); err == nil {
		t.Fatal("unknown cursor field must be rejected")
	}
}

func TestSkinWardrobePageSQLUsesStableBoundedKeyset(t *testing.T) {
	request, err := parseSkinWardrobePageRequest(url.Values{"kind": {"skin"}, "limit": {"100"}}, 7)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &skinWardrobePageCursor{
		Version: skinWardrobePageCursorVersion, Scope: request.Scope,
		AddedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), AssetID: 500,
	}
	query, arguments := skinWardrobePageSQL(request)
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	for _, required := range []string{
		"wardrobe.user_id=$", "asset.kind=$", "wardrobe.added_at<$", "wardrobe.asset_id>$",
		"order by wardrobe.added_at desc,wardrobe.asset_id", "limit $",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("query missing %q:\n%s", required, query)
		}
	}
	for _, forbidden := range []string{" offset ", "limit 5000", "array_to_string", " ilike "} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("query contains forbidden %q:\n%s", forbidden, query)
		}
	}
	if got := arguments[len(arguments)-1]; got != 101 {
		t.Fatalf("limit argument=%v want 101", got)
	}
	countSQL, _ := skinWardrobeCountSQL(request)
	if strings.Contains(strings.ToLower(countSQL), "skin_texture_blobs") || !strings.Contains(strings.ToLower(countSQL), "count(*)") {
		t.Fatalf("count must remain a separate narrow same-scope query:\n%s", countSQL)
	}
}
