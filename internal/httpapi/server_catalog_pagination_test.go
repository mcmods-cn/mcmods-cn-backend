package httpapi

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/searchindex"
)

func TestServerCatalogCursorBindsEveryFilterAndBuildsTypesenseKeyset(t *testing.T) {
	values := url.Values{
		"q": {"forge"}, "excludeSiteId": {"abc123def"}, "tag": {"technology"},
		"language": {"zh-CN"}, "version": {"1.21.1,1.20.1"}, "versionMode": {"all"},
		"mods": {"jei,create"}, "modded": {"true"}, "online": {"true"},
		"whitelist": {"false"}, "onlineMode": {"true"}, "sort": {"heat"}, "order": {"desc"}, "limit": {"20"},
	}
	request, err := parseServerCatalogPageRequest(values)
	if err != nil {
		t.Fatal(err)
	}
	filter := serverCatalogIndexFilter(request)
	for _, expected := range []string{
		"review_status:=approved", "public_id:!=`abc123def`", "primary_tag:=`technology`",
		"languages:=`zh-CN`", "minecraft_versions:=`1.20.1`", "minecraft_versions:=`1.21.1`",
		"mods:=`create`", "mods:=`jei`", "modded:=true", "online:=true", "whitelist:=false", "online_mode:=true",
	} {
		if !strings.Contains(filter, expected) {
			t.Fatalf("filter %q does not contain %q", filter, expected)
		}
	}
	hit := searchindex.SearchHit{InternalID: 91, UpdatedAt: 1_700_000_000, HeatSortDesc: 250_000_001}
	values.Set("cursor", serverCatalogNextCursor(request, hit))
	next, err := parseServerCatalogPageRequest(values)
	if err != nil {
		t.Fatal(err)
	}
	cursorFilter := serverCatalogCursorFilter(next)
	for _, expected := range []string{
		"heat_sort_desc:<250000001", "heat_sort_desc:=250000001 && updated_at:<1700000000",
		"updated_at:=1700000000 && internal_id:<91",
	} {
		if !strings.Contains(cursorFilter, expected) {
			t.Fatalf("cursor filter %q does not contain %q", cursorFilter, expected)
		}
	}
	values.Set("online", "false")
	if _, err = parseServerCatalogPageRequest(values); err == nil {
		t.Fatal("server cursor was reusable across a boolean filter")
	}
}

func TestServerCatalogRejectsLegacyDeepPagingAndUnstableRelevance(t *testing.T) {
	for _, values := range []url.Values{
		{"page": {"2"}},
		{"offset": {"60"}},
		{"q": {"forge"}, "sort": {"relevance"}},
	} {
		if _, err := parseServerCatalogPageRequest(values); err == nil {
			t.Fatalf("accepted unsupported pagination/sort: %v", values)
		}
	}
}

func TestServerCatalogIndexSortsAndCursorsCoverEveryAcceptedSort(t *testing.T) {
	hit := searchindex.SearchHit{
		InternalID: 7, CreatedAt: 100, UpdatedAt: 200,
		HeatSortAsc: 300, HeatSortDesc: 301, DownloadCount: 400, FavoriteCount: 500,
		RatingScore: 45_000, RatingCount: 60, ViewCount: 700, CommentCount: 800,
	}
	tests := []struct {
		sort     string
		order    string
		sortBy   string
		cursorIn string
	}{
		{"published", "asc", "created_at:asc,updated_at:asc,internal_id:asc", "created_at:>100"},
		{"updated", "desc", "updated_at:desc,internal_id:desc", "updated_at:<200"},
		{"collected", "asc", "updated_at:asc,internal_id:asc", "updated_at:>200"},
		{"heat", "asc", "heat_sort_asc:asc,updated_at:asc,internal_id:asc", "heat_sort_asc:>300"},
		{"downloads", "desc", "download_count:desc,updated_at:desc,internal_id:desc", "download_count:<400"},
		{"favorites", "asc", "favorite_count:asc,updated_at:asc,internal_id:asc", "favorite_count:>500"},
		{"rating", "desc", "rating_score:desc,rating_count:desc,internal_id:desc", "rating_score:<45000"},
		{"views", "asc", "view_count:asc,updated_at:asc,internal_id:asc", "view_count:>700"},
		{"comments", "desc", "comment_count:desc,updated_at:desc,internal_id:desc", "comment_count:<800"},
	}
	for _, test := range tests {
		t.Run(test.sort+"_"+test.order, func(t *testing.T) {
			values := url.Values{"sort": {test.sort}, "order": {test.order}, "limit": {"20"}}
			request, err := parseServerCatalogPageRequest(values)
			if err != nil {
				t.Fatal(err)
			}
			if got := serverCatalogIndexSort(request); got != test.sortBy {
				t.Fatalf("sort=%q want %q", got, test.sortBy)
			}
			values.Set("cursor", serverCatalogNextCursor(request, hit))
			next, err := parseServerCatalogPageRequest(values)
			if err != nil {
				t.Fatal(err)
			}
			if got := serverCatalogCursorFilter(next); !strings.Contains(got, test.cursorIn) {
				t.Fatalf("cursor filter=%q want fragment %q", got, test.cursorIn)
			}
		})
	}
}

func TestServerCatalogNameSortUsesStableDatabaseKeyset(t *testing.T) {
	values := url.Values{"sort": {"name"}, "order": {"asc"}, "limit": {"20"}}
	request, err := parseServerCatalogPageRequest(values)
	if err != nil {
		t.Fatal(err)
	}
	if got := serverCatalogDatabaseOrder(request); got != "lower(server.name) asc,server.id asc" {
		t.Fatalf("name sort=%q", got)
	}
	values.Set("cursor", serverCatalogDatabaseNextCursor(request, serverCatalogDatabaseRow{InternalID: 3, SortName: "alpha"}))
	next, err := parseServerCatalogPageRequest(values)
	if err != nil || next.Cursor.Mode != serverCatalogCursorSQL || serverCatalogCursorFilter(next) != "" {
		t.Fatalf("name cursor=%+v index filter=%q err=%v", next.Cursor, serverCatalogCursorFilter(next), err)
	}
	predicate, arguments := serverCatalogDatabaseCursorPredicate(next, 5)
	if predicate != "(lower(server.name),server.id) > ($5,$6)" || len(arguments) != 2 || arguments[0] != "alpha" {
		t.Fatalf("name predicate=%q arguments=%#v", predicate, arguments)
	}
}

func TestServerCatalogSQLFallbackCursorCoversEveryStableSort(t *testing.T) {
	row := serverCatalogDatabaseRow{
		InternalID: 17, CreatedAt: time.Unix(100, 123), UpdatedAt: time.Unix(200, 456), SortName: "alpha",
		HeatSortAsc: 300, HeatSortDesc: 301, DownloadCount: 400, FavoriteCount: 500,
		RatingScore: 45_000, RatingCount: 60, ViewCount: 700, CommentCount: 800,
	}
	for _, sortField := range []string{
		"published", "updated", "collected", "heat", "downloads", "favorites", "rating", "views", "comments", "name",
	} {
		t.Run(sortField, func(t *testing.T) {
			values := url.Values{"sort": {sortField}, "order": {"desc"}, "limit": {"20"}}
			request, err := parseServerCatalogPageRequest(values)
			if err != nil {
				t.Fatal(err)
			}
			values.Set("cursor", serverCatalogDatabaseNextCursor(request, row))
			next, err := parseServerCatalogPageRequest(values)
			if err != nil || next.Cursor.Mode != serverCatalogCursorSQL {
				t.Fatalf("cursor=%+v err=%v", next.Cursor, err)
			}
			predicate, arguments := serverCatalogDatabaseCursorPredicate(next, 7)
			if predicate == "" || len(arguments) < 2 || !strings.Contains(predicate, " < ") {
				t.Fatalf("predicate=%q arguments=%#v", predicate, arguments)
			}
		})
	}
}
