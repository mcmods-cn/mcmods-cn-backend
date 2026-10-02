package httpapi

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestFavoriteCollectionPageCursorIsStrictAndScopeBound(t *testing.T) {
	claims := security.Claims{Subject: 42}
	request, err := parseFavoriteCollectionPageRequest(url.Values{"limit": {"40"}}, 42, true, claims)
	if err != nil || request.Limit != 40 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	anchor := favoriteCollectionPageRow{
		ID: 91, CreatedAt: time.Date(2026, 8, 22, 2, 3, 4, 0, time.UTC),
		Summary: favoriteCollectionSummary{IsDefault: false},
	}
	cursor := favoriteCollectionNextCursor(request, anchor)
	values := url.Values{"limit": {"40"}, "cursor": {cursor}}
	parsed, err := parseFavoriteCollectionPageRequest(values, 42, true, claims)
	if err != nil || parsed.Cursor == nil || parsed.Cursor.ID != 91 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	for name, testCase := range map[string]struct {
		values         url.Values
		ownerID        int64
		includePrivate bool
		claims         security.Claims
	}{
		"foreign owner":  {values, 43, true, claims},
		"public scope":   {values, 42, false, claims},
		"foreign viewer": {values, 42, true, security.Claims{Subject: 43}},
		"legacy page":    {url.Values{"page": {"2"}}, 42, true, claims},
		"legacy offset":  {url.Values{"offset": {"20"}}, 42, true, claims},
		"unknown":        {url.Values{"sort": {"name"}}, 42, true, claims},
		"duplicate":      {url.Values{"limit": {"20", "40"}}, 42, true, claims},
		"large limit":    {url.Values{"limit": {"101"}}, 42, true, claims},
	} {
		t.Run(name, func(t *testing.T) {
			if _, parseErr := parseFavoriteCollectionPageRequest(testCase.values, testCase.ownerID, testCase.includePrivate, testCase.claims); parseErr == nil {
				t.Fatal("expected request rejection")
			}
		})
	}
}

func TestFavoriteCollectionAndItemSQLUseBoundedStableKeysets(t *testing.T) {
	claims := security.Claims{Subject: 42}
	collectionPage, err := parseFavoriteCollectionPageRequest(url.Values{"limit": {"100"}}, 42, true, claims)
	if err != nil {
		t.Fatal(err)
	}
	collectionPage.Cursor = &favoriteCollectionPageCursor{
		Version: favoritePageCursorVersion, Scope: collectionPage.Scope,
		IsDefault: false, CreatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), ID: 500,
	}
	collectionSQL, collectionArguments := favoriteCollectionPageSQL(collectionPage)
	assertFavoriteBoundedSQL(t, collectionSQL, collectionArguments, []string{
		"collection.user_id=$1", "collection.is_default<$", "collection.created_at>$",
		"order by collection.is_default desc,collection.created_at,collection.id", "limit $",
	})

	itemPage, err := parseFavoriteItemPageRequest(url.Values{"limit": {"100"}}, 42, true, "abc234567", claims)
	if err != nil {
		t.Fatal(err)
	}
	itemPage.Cursor = &favoriteItemPageCursor{
		Version: favoritePageCursorVersion, Scope: itemPage.Scope,
		CreatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), ID: 500,
	}
	itemSQL, itemArguments := favoriteItemPageSQL(itemPage)
	assertFavoriteBoundedSQL(t, itemSQL, itemArguments, []string{
		"collection.public_id=$5", "item.created_at<$", "item.id<$",
		"order by item.created_at desc,item.id desc", "limit $",
	})
}

func TestFavoriteMembershipSummaryIsOneBoundedMetadataFreeQuery(t *testing.T) {
	request, err := normalizeFavoriteMembershipSummaryRequest(favoriteMembershipSummaryRequest{
		EntityType: " MOD ", EntityPublicIDs: []string{"abc234567", "def234567", "abc234567"},
	})
	if err != nil || request.EntityType != "mod" || len(request.EntityPublicIDs) != 2 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	query, arguments := favoriteMembershipSummarySQL(request, 42, false)
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	for _, required := range []string{
		"target.entity_type=$2", "target.public_id=any($3::text[])",
		"join lateral (select 1 from favorite_collection_items item", "collection.user_id=$1",
		"item.entity_type=target.entity_type", "item.entity_id=target.internal_id",
		"limit 1) membership on true",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("summary query missing %q:\n%s", required, query)
		}
	}
	for _, forbidden := range []string{"primary_name", "secondary_name", "icon_url", " offset ", " limit 5000"} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("summary query contains forbidden %q:\n%s", forbidden, query)
		}
	}
	if len(arguments) != 4 {
		t.Fatalf("arguments=%v", arguments)
	}
	if _, err = normalizeFavoriteMembershipSummaryRequest(favoriteMembershipSummaryRequest{
		EntityType: "mod", EntityPublicIDs: make([]string, 101),
	}); err == nil {
		t.Fatal("more than 100 membership targets must be rejected")
	}
}

func TestFavoriteMembershipDeltaIsBoundedNormalizedAndDisjoint(t *testing.T) {
	request, err := normalizeFavoriteMembershipDeltaRequest(favoriteMembershipDeltaRequest{
		EntityType: " MOD ", EntityPublicID: " ABC234567 ",
		AddCollectionIDs:    []string{"DEF234567", "def234567"},
		RemoveCollectionIDs: []string{"ghi234567"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.EntityType != "mod" || request.EntityPublicID != "abc234567" ||
		len(request.AddCollectionIDs) != 1 || request.AddCollectionIDs[0] != "def234567" ||
		len(request.RemoveCollectionIDs) != 1 || request.RemoveCollectionIDs[0] != "ghi234567" {
		t.Fatalf("request=%+v", request)
	}
	if _, err = normalizeFavoriteMembershipDeltaRequest(favoriteMembershipDeltaRequest{
		EntityType: "mod", EntityPublicID: "abc234567",
		AddCollectionIDs: []string{"def234567"}, RemoveCollectionIDs: []string{"DEF234567"},
	}); err == nil {
		t.Fatal("overlapping add/remove sets must be rejected")
	}
	tooMany := make([]string, maximumFavoritePageLimit+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("%09d", index+1)
	}
	if _, err = normalizeFavoriteMembershipDeltaRequest(favoriteMembershipDeltaRequest{
		EntityType: "mod", EntityPublicID: "abc234567", AddCollectionIDs: tooMany,
	}); err == nil {
		t.Fatal("more than 100 collection changes must be rejected")
	}
}

func assertFavoriteBoundedSQL(t *testing.T, query string, arguments []any, required []string) {
	t.Helper()
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	for _, fragment := range required {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("query missing %q:\n%s", fragment, query)
		}
	}
	for _, forbidden := range []string{" offset ", "limit 5000"} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("query contains forbidden %q:\n%s", forbidden, query)
		}
	}
	if got := arguments[len(arguments)-1]; got != 101 {
		t.Fatalf("limit argument=%v want=101", got)
	}
}
