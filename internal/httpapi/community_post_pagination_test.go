package httpapi

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestCommunityPostPageCursorBindsEveryFilterAndStableSort(t *testing.T) {
	claims := security.Claims{Subject: 42}
	request, err := parseCommunityPostPageRequest(url.Values{
		"kind": {"tutorial"}, "q": {" red stone "}, "category": {"general"},
		"version": {"1.21.1"}, "versionMode": {"all"}, "project": {"mod:abc234567"},
		"sort": {"published"}, "order": {"desc"}, "limit": {"40"},
	}, claims)
	if err != nil || request.Limit != 40 || request.Query != "red stone" {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	cursor := communityPostNextCursor(request, communityPostPageRow{
		ID: 91, PublishedAt: time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 22, 1, 2, 4, 0, time.UTC),
	})
	values := url.Values{
		"kind": {"tutorial"}, "q": {"red stone"}, "category": {"general"},
		"version": {"1.21.1"}, "versionMode": {"all"}, "project": {"mod:abc234567"},
		"sort": {"published"}, "order": {"desc"}, "limit": {"40"}, "cursor": {cursor},
	}
	parsed, err := parseCommunityPostPageRequest(values, claims)
	if err != nil || parsed.Cursor == nil || parsed.Cursor.ID != 91 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	for name, mutate := range map[string]func(url.Values){
		"query":         func(v url.Values) { v.Set("q", "different") },
		"kind":          func(v url.Values) { v.Set("kind", "news") },
		"viewer":        func(v url.Values) {},
		"legacy page":   func(v url.Values) { v.Set("page", "2") },
		"legacy offset": func(v url.Values) { v.Set("offset", "40") },
		"unknown":       func(v url.Values) { v.Set("extra", "1") },
	} {
		t.Run(name, func(t *testing.T) {
			foreign := values
			copyValues := make(url.Values, len(foreign))
			for key, items := range foreign {
				copyValues[key] = append([]string{}, items...)
			}
			mutate(copyValues)
			candidateClaims := claims
			if name == "viewer" {
				candidateClaims.Subject = 43
			}
			if _, parseErr := parseCommunityPostPageRequest(copyValues, candidateClaims); parseErr == nil {
				t.Fatal("expected request rejection")
			}
		})
	}
}

func TestCommunityPostPageSQLUsesNarrowProjectionFullTextAndKeyset(t *testing.T) {
	request, err := parseCommunityPostPageRequest(url.Values{
		"kind": {"tutorial"}, "q": {"redstone"}, "limit": {"100"},
		"sort": {"published"}, "order": {"desc"},
	}, security.Claims{})
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &communityPostPageCursor{
		Version: communityPostPageCursorVersion, Scope: request.Scope, Sort: request.Sort,
		Direction: request.Direction, ID: 500000,
		PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	query, arguments := communityPostPageSQL(request)
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	for _, required := range []string{
		"from community_post_catalog post", "post.search_document @@ websearch_to_tsquery",
		"post.body_summary", "post.published_at", "post.id", "limit $",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("query missing %q:\n%s", required, query)
		}
	}
	for _, forbidden := range []string{"body_markdown", " offset ", "count(*)", " like "} {
		if strings.Contains(normalized, forbidden) {
			t.Errorf("query contains %q:\n%s", forbidden, query)
		}
	}
	if arguments[len(arguments)-1] != 101 {
		t.Fatalf("limit=%v", arguments[len(arguments)-1])
	}
	if !strings.Contains(normalized, "where post.kind=$1 and post.review_status='approved'") {
		t.Fatalf("anonymous catalog did not select the approved-only index path:\n%s", query)
	}
	request.ViewerID = 42
	viewerQuery, _ := communityPostPageSQL(request)
	if !strings.Contains(viewerQuery, "(post.review_status='approved' or post.author_id=$2)") {
		t.Fatalf("author-visible catalog lost its private-post boundary:\n%s", viewerQuery)
	}
	request.Moderator = true
	moderatorQuery, _ := communityPostPageSQL(request)
	if !strings.Contains(moderatorQuery, "where post.kind=$1 and true") {
		t.Fatalf("moderator catalog did not retain all review states:\n%s", moderatorQuery)
	}
}
