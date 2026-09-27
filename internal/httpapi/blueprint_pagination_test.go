package httpapi

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBlueprintCursorRoundTripBindsFiltersVisibilityAndStableKey(t *testing.T) {
	request, err := parseBlueprintPageRequest(url.Values{
		"q": {"  castle  "}, "limit": {"2"}, "sort": {"name"}, "order": {"asc"},
	}, 42, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded := blueprintSQLPageCursor(request, blueprintPageRow{
		InternalID: 7, SortName: "castle", CreatedAt: time.Unix(10, 0), UpdatedAt: time.Unix(20, 0),
	})
	values := url.Values{
		"q": {"castle"}, "limit": {"2"}, "sort": {"name"}, "order": {"asc"}, "cursor": {encoded},
	}
	next, err := parseBlueprintPageRequest(values, 42, false)
	if err != nil {
		t.Fatal(err)
	}
	predicate, arguments := blueprintCursorPredicateSQL(next.Cursor, 7)
	if predicate != "and (lower(b.title),b.id) > ($7,$8)" {
		t.Fatalf("predicate=%q", predicate)
	}
	if len(arguments) != 2 || arguments[0] != "castle" || arguments[1] != int64(7) {
		t.Fatalf("arguments=%#v", arguments)
	}
	values.Set("q", "different")
	if _, err = parseBlueprintPageRequest(values, 42, false); err == nil {
		t.Fatal("blueprint cursor was reusable across queries")
	}
	values.Set("q", "castle")
	if _, err = parseBlueprintPageRequest(values, 99, false); err == nil {
		t.Fatal("blueprint cursor was reusable across visibility identities")
	}
}

func TestBlueprintMetricCursorIsStrictAndLegacyOffsetCannotMixWithCursor(t *testing.T) {
	request, err := parseBlueprintPageRequest(url.Values{
		"limit": {"20"}, "sort": {"rating"}, "order": {"desc"},
	}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded := blueprintSQLPageCursor(request, blueprintPageRow{
		InternalID: 9, UpdatedAt: time.Unix(30, 0), Rating: "4.1250", RatingCount: 17,
	})
	cursor, err := decodeBlueprintPageCursor(encoded, request.Scope, request.Sort, request.Direction)
	if err != nil {
		t.Fatal(err)
	}
	predicate, arguments := blueprintCursorPredicateSQL(cursor, 7)
	if !strings.Contains(predicate, "bayesian_rating") || !strings.Contains(predicate, ") < (") || len(arguments) != 4 {
		t.Fatalf("rating predicate=%q arguments=%#v", predicate, arguments)
	}
	cursor.Metric = "1/2"
	if _, err = decodeBlueprintPageCursor(encodeBlueprintPageCursor(*cursor), request.Scope, request.Sort, request.Direction); err == nil {
		t.Fatal("non-decimal metric cursor was accepted")
	}
	values := url.Values{"limit": {"20"}, "sort": {"rating"}, "order": {"desc"}, "cursor": {encoded}, "offset": {"20"}}
	if _, err = parseBlueprintPageRequest(values, 0, false); err == nil {
		t.Fatal("cursor and legacy offset were accepted together")
	}
}
