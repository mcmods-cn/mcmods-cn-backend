package httpapi

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type catalogSort string

const (
	catalogSortRelevance catalogSort = "relevance"
	catalogSortHeat      catalogSort = "heat"
	catalogSortUpdated   catalogSort = "updated"
	catalogSortCollected catalogSort = "collected"
	catalogSortDownloads catalogSort = "downloads"
	catalogSortFavorites catalogSort = "favorites"
	catalogSortRating    catalogSort = "rating"
	catalogSortViews     catalogSort = "views"
	catalogSortComments  catalogSort = "comments"
	catalogSortNameAsc   catalogSort = "nameAsc"
	catalogSortNameDesc  catalogSort = "nameDesc"
)

func parseCatalogSort(value string) (catalogSort, bool) {
	if strings.TrimSpace(value) == "" {
		return catalogSortRelevance, true
	}
	sort := catalogSort(strings.TrimSpace(value))
	switch sort {
	case catalogSortRelevance, catalogSortHeat, catalogSortUpdated, catalogSortCollected,
		catalogSortDownloads, catalogSortFavorites, catalogSortRating, catalogSortViews,
		catalogSortComments, catalogSortNameAsc, catalogSortNameDesc:
		return sort, true
	default:
		return "", false
	}
}

func parseCatalogList(value string, maximum int) ([]string, bool) {
	parts := strings.Split(value, ",")
	result := make([]string, 0, min(len(parts), maximum))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(part) > 80 || len(result) >= maximum {
			return nil, false
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result, true
}

func parseCatalogQuery(value string) (string, bool) {
	value = strings.TrimSpace(value)
	return value, utf8.ValidString(value) && utf8.RuneCountInString(value) <= 200
}

func parseCatalogScalar(value string) (string, bool) {
	value = strings.TrimSpace(value)
	return value, utf8.ValidString(value) && len(value) <= 80 && !strings.Contains(value, ",")
}

func parseCatalogBoolean(value string) (string, bool) {
	value = strings.TrimSpace(value)
	return value, value == "" || value == "true" || value == "false"
}

var catalogFeatureKeys = stringSet("tutorials", "items", "gallery", "downloads", "reviewed", "claimed", "serverSupport", "modpackAllowed", "severeIssues")

func validCatalogFeatures(values []string) bool {
	for _, value := range values {
		if !catalogFeatureKeys[value] {
			return false
		}
	}
	return true
}

func everyCatalogValueAllowed(values []string, allowed map[string]bool) bool {
	for _, value := range values {
		if !allowed[value] {
			return false
		}
	}
	return true
}

func firstCatalogValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// parseCatalogUpdatedRange returns a day count. A negative count means older
// than the absolute value; zero means no time filter. Keeping this conversion
// here gives every catalog the same whitelist and boundary semantics.
func parseCatalogUpdatedRange(value string) (int, bool) {
	switch strings.TrimSpace(value) {
	case "", "all":
		return 0, true
	case "week":
		return 7, true
	case "month":
		return 30, true
	case "quarter":
		return 90, true
	case "year":
		return 365, true
	case "stale":
		return -365, true
	default:
		return 0, false
	}
}

func catalogSortUsesSearchIndex(sort catalogSort) bool {
	return sort == catalogSortRelevance
}

// catalogOrderSQL only receives server-owned SQL identifiers. User input is
// first reduced to catalogSort by parseCatalogSort, so no request text is ever
// interpolated into a query.
func catalogOrderSQL(sort catalogSort, indexed bool, indexedPosition int, updated, id, name string) string {
	if indexed {
		return fmt.Sprintf("array_position($%d::bigint[],%s),%s desc,%s desc", indexedPosition, id, updated, id)
	}
	stable := fmt.Sprintf("%s desc,%s desc", updated, id)
	switch sort {
	case catalogSortUpdated, catalogSortCollected:
		return stable
	case catalogSortDownloads:
		return "coalesce(popularity.download_count,0) desc," + stable
	case catalogSortFavorites:
		return "coalesce(popularity.favorite_count,0) desc," + stable
	case catalogSortRating:
		return "coalesce(popularity.bayesian_rating,0) desc,coalesce(popularity.rating_count,0) desc," + stable
	case catalogSortViews:
		return "coalesce(popularity.view_count,0) desc," + stable
	case catalogSortComments:
		return "coalesce(popularity.comment_count,0) desc," + stable
	case catalogSortNameAsc:
		return fmt.Sprintf("lower(%s) asc,%s asc", name, id)
	case catalogSortNameDesc:
		return fmt.Sprintf("lower(%s) desc,%s desc", name, id)
	default:
		return "coalesce(popularity.heat_score,0) desc," + stable
	}
}
