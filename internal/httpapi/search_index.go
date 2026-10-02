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
	filters := []string{"entity_type:=" + typesenseFilterValue(projectType), projectVisibilityFilter(claims.Subject)}
	if category != "" {
		filters = append(filters, "categories:="+typesenseFilterValue(category))
	}
	if version != "" {
		filters = append(filters, "minecraft_versions:="+typesenseFilterValue(version))
	}
	if loader != "" {
		filters = append(filters, "loaders:="+typesenseFilterValue(loader))
	}
	result, err := s.search.Search(ctx, searchindex.SearchRequest{
		Collection: "projects", Query: query,
		QueryBy:  []string{"names", "identifiers", "slug", "public_id", "creators", "keywords", "text"},
		Infix:    []string{"fallback", "always", "always", "always", "fallback", "fallback", "off"},
		FilterBy: strings.Join(filters, " && "), SortBy: "_text_match:desc,updated_at:desc",
		Page: offset/limit + 1, PerPage: limit,
	})
	if err != nil {
		return indexedSearchPage{}
	}
	return indexedSearchPage{IDs: result.IDs, Total: result.Found, Used: true}
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
	filters := []string{"status:=active"}
	if kindCode != "" {
		filters = append(filters, "kind_code:="+typesenseFilterValue(kindCode))
	}
	if namespace != "" {
		filters = append(filters, "namespace:="+typesenseFilterValue(namespace))
	}
	result, err := s.search.Search(ctx, searchindex.SearchRequest{
		Collection: "resources", Query: query, QueryBy: []string{"canonical_id", "names", "public_id"},
		Infix:    []string{"always", "fallback", "always"},
		FilterBy: strings.Join(filters, " && "), SortBy: "_text_match:desc,updated_at:desc",
		Page: offset/limit + 1, PerPage: limit,
	})
	if err != nil {
		return indexedSearchPage{}
	}
	return indexedSearchPage{IDs: result.IDs, Total: result.Found, Used: true}
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
