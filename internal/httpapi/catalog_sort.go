package httpapi

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type catalogSort string
type catalogSortDirection string

const (
	catalogSortRelevance catalogSort = "relevance"
	catalogSortPublished catalogSort = "published"
	catalogSortHeat      catalogSort = "heat"
	catalogSortUpdated   catalogSort = "updated"
	catalogSortCollected catalogSort = "collected"
	catalogSortDownloads catalogSort = "downloads"
	catalogSortFavorites catalogSort = "favorites"
	catalogSortRating    catalogSort = "rating"
	catalogSortViews     catalogSort = "views"
	catalogSortComments  catalogSort = "comments"
	catalogSortName      catalogSort = "name"

	catalogSortAscending  catalogSortDirection = "asc"
	catalogSortDescending catalogSortDirection = "desc"
)

func parseCatalogSort(value string) (catalogSort, bool) {
	if strings.TrimSpace(value) == "" {
		return catalogSortRelevance, true
	}
	sort := catalogSort(strings.TrimSpace(value))
	switch sort {
	case catalogSortRelevance, catalogSortPublished, catalogSortHeat, catalogSortUpdated, catalogSortCollected,
		catalogSortDownloads, catalogSortFavorites, catalogSortRating, catalogSortViews,
		catalogSortComments, catalogSortName:
		return sort, true
	default:
		return "", false
	}
}

func parseCatalogSortDirection(value string, sort catalogSort) (catalogSortDirection, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "default":
		if sort == catalogSortName {
			return catalogSortAscending, true
		}
		return catalogSortDescending, true
	case string(catalogSortAscending):
		return catalogSortAscending, true
	case string(catalogSortDescending):
		return catalogSortDescending, true
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

func parseCatalogModFilters(value string) []string {
	result := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	for _, candidate := range strings.Split(value, ",") {
		normalized := normalizeCatalogModIdentifier(candidate)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
		if len(result) == 32 {
			break
		}
	}
	return result
}

func normalizeCatalogModIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '_' && character != '-' && character != '.' {
			return ""
		}
	}
	return value
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

func parseCatalogVersionMode(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case "", "any":
		return "any", true
	case "all":
		return "all", true
	default:
		return "", false
	}
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
func catalogOrderSQL(sort catalogSort, direction catalogSortDirection, indexed bool, indexedPosition int, published, updated, id, name string) string {
	if indexed {
		return fmt.Sprintf("array_position($%d::bigint[],%s),%s desc,%s desc", indexedPosition, id, updated, id)
	}
	directionSQL := string(direction)
	if directionSQL != string(catalogSortAscending) {
		directionSQL = string(catalogSortDescending)
	}
	stable := fmt.Sprintf("%s %s,%s %s", updated, directionSQL, id, directionSQL)
	withStable := func(expression string) string {
		return fmt.Sprintf("%s %s,%s", expression, directionSQL, stable)
	}
	switch sort {
	case catalogSortPublished:
		return withStable(published)
	case catalogSortUpdated, catalogSortCollected:
		return stable
	case catalogSortDownloads:
		return withStable("coalesce(popularity.download_count,0)")
	case catalogSortFavorites:
		return withStable("coalesce(popularity.favorite_count,0)")
	case catalogSortRating:
		return fmt.Sprintf("coalesce(popularity.bayesian_rating,0) %s,coalesce(popularity.rating_count,0) %s,%s", directionSQL, directionSQL, stable)
	case catalogSortViews:
		return withStable("coalesce(popularity.view_count,0)")
	case catalogSortComments:
		return withStable("coalesce(popularity.comment_count,0)")
	case catalogSortName:
		return fmt.Sprintf("lower(%s) %s,%s %s", name, directionSQL, id, directionSQL)
	default:
		return withStable("coalesce(popularity.heat_score,0)")
	}
}
