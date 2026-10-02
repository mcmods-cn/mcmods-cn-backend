package httpapi

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCreatorSQLCursorRoundTripBindsScopeAndStableKey(t *testing.T) {
	request, err := parseCreatorPageRequest(url.Values{
		"kind": {"author"}, "query": {"build"}, "limit": {"2"}, "sort": {"name"}, "order": {"asc"},
	}, 42, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded := creatorSQLPageCursor(request, creatorPageRow{
		InternalID: 7, SortName: "alice", CreatedAt: time.Unix(10, 0), UpdatedAt: time.Unix(20, 0),
	})
	values := url.Values{
		"kind": {"author"}, "query": {"build"}, "limit": {"2"}, "sort": {"name"}, "order": {"asc"}, "cursor": {encoded},
	}
	next, err := parseCreatorPageRequest(values, 42, false)
	if err != nil {
		t.Fatal(err)
	}
	predicate, arguments := creatorCursorPredicateSQL(next.Cursor, 7)
	if predicate != "and (lower(creator.name),creator.id) > ($7,$8)" {
		t.Fatalf("predicate=%q", predicate)
	}
	if len(arguments) != 2 || arguments[0] != "alice" || arguments[1] != int64(7) {
		t.Fatalf("arguments=%#v", arguments)
	}
	values.Set("query", "different")
	if _, err = parseCreatorPageRequest(values, 42, false); err == nil {
		t.Fatal("creator cursor was reusable across queries")
	}
	values.Set("query", "build")
	if _, err = parseCreatorPageRequest(values, 99, false); err == nil {
		t.Fatal("creator cursor was reusable across visibility identities")
	}
}

func TestCreatorMetricAndIndexCursorsAreBoundedAndValidated(t *testing.T) {
	request, err := parseCreatorPageRequest(url.Values{
		"limit": {"20"}, "sort": {"rating"}, "order": {"desc"},
	}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded := creatorSQLPageCursor(request, creatorPageRow{
		InternalID: 9, UpdatedAt: time.Unix(30, 0), Rating: "4.1250", RatingCount: 17,
	})
	cursor, err := decodeCreatorPageCursor(encoded, request.Scope, request.Sort, request.Direction, request.Limit)
	if err != nil {
		t.Fatal(err)
	}
	predicate, arguments := creatorCursorPredicateSQL(cursor, 7)
	if !strings.Contains(predicate, "bayesian_rating") || !strings.Contains(predicate, ") < (") || len(arguments) != 4 {
		t.Fatalf("rating predicate=%q arguments=%#v", predicate, arguments)
	}
	cursor.Metric = "1/2"
	if _, err = decodeCreatorPageCursor(encodeCreatorPageCursor(*cursor), request.Scope, request.Sort, request.Direction, request.Limit); err == nil {
		t.Fatal("non-decimal metric cursor was accepted")
	}

	indexedRequest, err := parseCreatorPageRequest(url.Values{
		"query": {"forge"}, "limit": {"40"}, "sort": {"relevance"},
	}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	indexed := creatorIndexPageCursor(indexedRequest, 40)
	values := url.Values{"query": {"forge"}, "limit": {"40"}, "sort": {"relevance"}, "cursor": {indexed}}
	parsed, err := parseCreatorPageRequest(values, 0, false)
	if err != nil || parsed.Cursor.Offset != 40 {
		t.Fatalf("index cursor=%+v err=%v", parsed.Cursor, err)
	}
	values.Set("limit", "20")
	if _, err = parseCreatorPageRequest(values, 0, false); err == nil {
		t.Fatal("index cursor was reusable with a different page size")
	}
}
