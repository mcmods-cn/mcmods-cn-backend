package httpapi

import (
	"context"
	"fmt"
	"strings"

	"mcmods-cn-backend/internal/searchindex"
	"mcmods-cn-backend/internal/security"
)

type indexedSearchPage struct {
	IDs   []int64
	Total int
	Used  bool
}

type indexedCreatorCounts struct {
	Author int
	Team   int
	Used   bool
}

func (s *Server) searchProjectPage(ctx context.Context, query, projectType, category, version, loader string,
	claims security.Claims, limit, offset int) indexedSearchPage {
	if strings.TrimSpace(query) == "" || !searchPageSupported(s.search, limit, offset) {
		return indexedSearchPage{}
	}
	filters := []string{"entity_type:=" + typesenseFilterValue(projectType)}
	if category != "" {
		filters = append(filters, "categories:="+typesenseFilterValue(category))
	}
	if version != "" {
		filters = append(filters, "minecraft_versions:="+typesenseFilterValue(version))
	}
	if loader != "" {
		filters = append(filters, "loaders:="+typesenseFilterValue(loader))
	}
	return s.currentVisibleIndexPage(ctx, searchindex.SearchRequest{
		Collection: "projects", Query: query,
		QueryBy:  []string{"names", "identifiers", "slug", "public_id", "creators", "keywords", "text"},
		Infix:    []string{"fallback", "always", "always", "always", "fallback", "fallback", "off"},
		FilterBy: strings.Join(filters, " && "), SortBy: "_text_match:desc,updated_at:desc",
		Page: 1, PerPage: 250,
	}, projectType, claims.Subject, limit, offset)
}

func (s *Server) searchCommunityPage(ctx context.Context, query, kind, projectID, resourceID string,
	claims security.Claims, moderator bool, limit, offset int) indexedSearchPage {
	if strings.TrimSpace(query) == "" || !searchPageSupported(s.search, limit, offset) {
		return indexedSearchPage{}
	}
	filters := []string{"kind:=" + typesenseFilterValue(kind), "status:=active"}
	if !moderator {
		filters = append(filters, projectVisibilityFilterWithField(claims.Subject, "author_id"))
	}
	if projectID != "" {
		filters = append(filters, "project_ids:="+typesenseFilterValue(projectID))
	}
	if resourceID != "" {
		filters = append(filters, "resource_ids:="+typesenseFilterValue(resourceID))
	}
	result, err := s.search.Search(ctx, searchindex.SearchRequest{
		Collection: "community", Query: query, QueryBy: []string{"title", "body"}, Infix: []string{"fallback", "off"},
		FilterBy: strings.Join(filters, " && "), SortBy: "_text_match:desc,published_at:desc",
		Page: offset/limit + 1, PerPage: limit,
	})
	if err != nil {
		return indexedSearchPage{}
	}
	return indexedSearchPage{IDs: result.IDs, Total: result.Found, Used: true}
}

func (s *Server) searchCreatorPage(ctx context.Context, query, kind string, claims security.Claims, admin bool, limit, offset int) indexedSearchPage {
	if strings.TrimSpace(query) == "" || !searchPageSupported(s.search, limit, offset) {
		return indexedSearchPage{}
	}
	filters := make([]string, 0, 2)
	if kind != "" {
		filters = append(filters, "kind:="+typesenseFilterValue(kind))
	}
	if !admin {
		visibility := "review_status:=approved"
		if claims.Subject > 0 {
			visibility = fmt.Sprintf("(review_status:=approved || created_by:=%d)", claims.Subject)
		}
		filters = append(filters, visibility)
	}
	result, err := s.search.Search(ctx, searchindex.SearchRequest{
		Collection: "creators", Query: query, QueryBy: []string{"name", "text"}, Infix: []string{"fallback", "off"}, FilterBy: strings.Join(filters, " && "),
		SortBy: "_text_match:desc,updated_at:desc", Page: offset/limit + 1, PerPage: limit,
	})
	if err != nil {
		return indexedSearchPage{}
	}
	return indexedSearchPage{IDs: result.IDs, Total: result.Found, Used: true}
}

func (s *Server) searchCreatorCounts(ctx context.Context, query string, claims security.Claims, admin bool) indexedCreatorCounts {
	if strings.TrimSpace(query) == "" || s.search == nil || !s.search.Ready() {
		return indexedCreatorCounts{}
	}
	filter := ""
	if !admin {
		filter = "review_status:=approved"
		if claims.Subject > 0 {
			filter = fmt.Sprintf("(review_status:=approved || created_by:=%d)", claims.Subject)
		}
	}
	result, err := s.search.Search(ctx, searchindex.SearchRequest{
		Collection: "creators", Query: query, QueryBy: []string{"name", "text"}, Infix: []string{"fallback", "off"}, FilterBy: filter,
		FacetBy: []string{"kind"}, Page: 1, PerPage: 1,
	})
	if err != nil {
		return indexedCreatorCounts{}
	}
	return indexedCreatorCounts{Author: result.Facets["kind"]["author"], Team: result.Facets["kind"]["team"], Used: true}
}

func (s *Server) searchResourcePage(ctx context.Context, query, kindCode, namespace string, limit, offset int) indexedSearchPage {
	if strings.TrimSpace(query) == "" || !searchPageSupported(s.search, limit, offset) {
		return indexedSearchPage{}
	}
	filters := []string{}
	if kindCode != "" {
		filters = append(filters, "kind_code:="+typesenseFilterValue(kindCode))
	}
	if namespace != "" {
		filters = append(filters, "namespace:="+typesenseFilterValue(namespace))
	}
	return s.currentVisibleIndexPage(ctx, searchindex.SearchRequest{
		Collection: "resources", Query: query, QueryBy: []string{"canonical_id", "names", "public_id"},
		Infix:    []string{"always", "fallback", "always"},
		FilterBy: strings.Join(filters, " && "), SortBy: "_text_match:desc,updated_at:desc",
		Page: 1, PerPage: 250,
	}, "resource", 0, limit, offset)
}

type indexedServerPage struct {
	Hits    []searchindex.SearchHit
	HasMore bool
	Used    bool
}

func (s *Server) searchServerPage(ctx context.Context, request serverCatalogPageRequest) indexedServerPage {
	if request.Sort == catalogSortName || request.Cursor != nil && request.Cursor.Mode == serverCatalogCursorSQL {
		return indexedServerPage{}
	}
	if !searchPageSupported(s.search, request.Limit, 0) {
		return indexedServerPage{}
	}
	query := request.Query
	if strings.TrimSpace(query) == "" {
		query = "*"
	}
	result, err := s.search.Search(ctx, searchindex.SearchRequest{
		Collection: "servers", Query: query, QueryBy: []string{"name", "text", "mods", "public_id"},
		Infix: []string{"fallback", "off", "fallback", "always"}, FilterBy: serverCatalogIndexFilter(request),
		SortBy: serverCatalogIndexSort(request), Page: 1, PerPage: request.Limit + 1,
	})
	if err != nil {
		return indexedServerPage{}
	}
	hits := result.Hits
	hasMore := false
	if len(hits) > request.Limit {
		hasMore = true
		hits = hits[:request.Limit]
	}
	return indexedServerPage{Hits: hits, HasMore: hasMore, Used: true}
}

func searchPageSupported(client *searchindex.Client, limit, offset int) bool {
	return client != nil && client.Ready() && limit > 0 && offset >= 0 && offset%limit == 0
}

func projectVisibilityFilter(userID int64) string {
	return projectVisibilityFilterWithField(userID, "submitted_by")
}

func projectVisibilityFilterWithField(userID int64, ownerField string) string {
	if userID <= 0 {
		return "review_status:=approved"
	}
	return fmt.Sprintf("(review_status:=approved || %s:=%d)", ownerField, userID)
}

func typesenseFilterValue(value string) string {
	return "`" + strings.ReplaceAll(strings.TrimSpace(value), "`", "\\`") + "`"
}

// A stale index ranks candidates; it never supplies an authorization count.
// Revalidate the complete bounded match set before counting and paging. If the
// set exceeds the fixed work budget, use the existing SQL search fallback.
const maximumVisibilitySearchCandidates = 5000

func (s *Server) currentVisibleIndexPage(ctx context.Context, request searchindex.SearchRequest, entityType string, viewerID int64, limit, offset int) indexedSearchPage {
	if s.db == nil {
		return indexedSearchPage{}
	}
	ids := make([]int64, 0)
	seen := map[int64]bool{}
	expected := -1
	for page := 1; page <= maximumVisibilitySearchCandidates/250; page++ {
		request.Page, request.PerPage = page, 250
		result, err := s.search.Search(ctx, request)
		if err != nil || result.Found > maximumVisibilitySearchCandidates {
			return indexedSearchPage{}
		}
		if expected < 0 {
			expected = result.Found
		} else if expected != result.Found {
			return indexedSearchPage{}
		}
		for _, id := range result.IDs {
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		if len(ids) >= expected {
			break
		}
		if len(result.IDs) == 0 {
			return indexedSearchPage{}
		}
	}
	if len(ids) != expected {
		return indexedSearchPage{}
	}
	var query string
	args := []any{ids, viewerID}
	switch entityType {
	case "mod":
		query = `select id from mods where id=any($1::bigint[]) and (review_status='approved' or submitted_by=$2)`
	case "modpack":
		query = `select id from modpacks where id=any($1::bigint[]) and (review_status='approved' or submitted_by=$2)`
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		query = `select id from simple_projects where id=any($1::bigint[]) and (review_status='approved' or submitted_by=$2) and project_type=$3`
		args = append(args, entityType)
	case "resource":
		query = `select entity.id from catalog_entities entity where entity.id=any($1::bigint[]) and entity.status='active' and entity.archived_at is null and ` + publicCatalogEntitySQL("entity", "resource") + ` and $2::bigint>=0`
	default:
		return indexedSearchPage{}
	}
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return indexedSearchPage{}
	}
	visible := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return indexedSearchPage{}
		}
		visible[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return indexedSearchPage{}
	}
	ordered := make([]int64, 0, len(visible))
	for _, id := range ids {
		if visible[id] {
			ordered = append(ordered, id)
		}
	}
	total := len(ordered)
	start := min(offset, total)
	end := min(start+limit, total)
	return indexedSearchPage{IDs: ordered[start:end], Total: total, Used: true}
}
