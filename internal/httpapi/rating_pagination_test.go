package httpapi

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRatingReviewPageCursorIsStrictAndTargetBound(t *testing.T) {
	target := rateableTarget{RouteID: 77, Type: "mod", PublicID: "abc234567"}
	request, err := parseRatingReviewPageRequest(url.Values{"limit": {"40"}}, target)
	if err != nil || request.Limit != 40 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	anchor := ratingReviewPageRow{UpdatedAt: time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC), ID: 91}
	cursor := ratingReviewNextCursor(request, anchor)
	withCursor := url.Values{"limit": {"40"}, "cursor": {cursor}}
	parsed, err := parseRatingReviewPageRequest(withCursor, target)
	if err != nil || parsed.Cursor == nil || parsed.Cursor.ID != 91 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	for name, testCase := range map[string]struct {
		values url.Values
		target rateableTarget
	}{
		"foreign target":   {withCursor, rateableTarget{RouteID: 78, Type: "mod", PublicID: "def234567"}},
		"foreign limit":    {url.Values{"limit": {"20"}, "cursor": {cursor}}, target},
		"legacy offset":    {url.Values{"offset": {"20"}}, target},
		"legacy page":      {url.Values{"page": {"2"}}, target},
		"unknown":          {url.Values{"sort": {"latest"}}, target},
		"duplicate limit":  {url.Values{"limit": {"20", "40"}}, target},
		"duplicate cursor": {url.Values{"cursor": {cursor, cursor}}, target},
		"zero limit":       {url.Values{"limit": {"0"}}, target},
		"large limit":      {url.Values{"limit": {"101"}}, target},
		"malformed limit":  {url.Values{"limit": {"many"}}, target},
		"malformed cursor": {url.Values{"cursor": {"not-base64"}}, target},
		"oversized cursor": {url.Values{"cursor": {strings.Repeat("a", 2049)}}, target},
	} {
		t.Run(name, func(t *testing.T) {
			if _, parseErr := parseRatingReviewPageRequest(testCase.values, testCase.target); parseErr == nil {
				t.Fatalf("expected rejection for %v", testCase.values)
			}
		})
	}
	unknown := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"scope":"` + request.Scope + `","updatedAt":"2026-08-22T01:02:03Z","id":91,"extra":true}`))
	if _, err = parseRatingReviewPageRequest(url.Values{"limit": {"40"}, "cursor": {unknown}}, target); err == nil {
		t.Fatal("unknown cursor field must be rejected")
	}
	for name, payload := range map[string]string{
		"wrong version": `{"v":2,"scope":"` + request.Scope + `","updatedAt":"2026-08-22T01:02:03Z","id":91}`,
		"zero time":     `{"v":1,"scope":"` + request.Scope + `","updatedAt":"0001-01-01T00:00:00Z","id":91}`,
		"zero id":       `{"v":1,"scope":"` + request.Scope + `","updatedAt":"2026-08-22T01:02:03Z","id":0}`,
		"trailing json": `{"v":1,"scope":"` + request.Scope + `","updatedAt":"2026-08-22T01:02:03Z","id":91}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
			if _, parseErr := parseRatingReviewPageRequest(url.Values{"limit": {"40"}, "cursor": {encoded}}, target); parseErr == nil {
				t.Fatal("invalid cursor must be rejected")
			}
		})
	}
}

func TestRatingReviewPageSQLUsesStableKeysetWithoutCountOrOffset(t *testing.T) {
	target := rateableTarget{RouteID: 77, Type: "mod", PublicID: "abc234567"}
	request, err := parseRatingReviewPageRequest(url.Values{"limit": {"100"}}, target)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &ratingReviewPageCursor{
		Version: ratingReviewPageCursorVersion, Scope: request.Scope,
		UpdatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), ID: 500,
	}
	query, arguments := ratingReviewPageSQL(request)
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	for _, required := range []string{
		"rating.object_route_id=$1", "rating.status='published'", "(rating.updated_at,rating.id)<($",
		"order by rating.updated_at desc,rating.id desc", "limit $",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("query missing %q:\n%s", required, query)
		}
	}
	for _, forbidden := range []string{" count(", " offset ", "limit 1000"} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("query contains forbidden %q:\n%s", forbidden, query)
		}
	}
	if got := arguments[len(arguments)-1]; got != 101 {
		t.Fatalf("limit argument=%v want=101", got)
	}
}
