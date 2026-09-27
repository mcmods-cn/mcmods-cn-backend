package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteHandlersUseCursorPagesAndBatchMembershipSummary(t *testing.T) {
	raw, err := os.ReadFile("favorite_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"parseFavoriteCollectionPageRequest", "favoriteCollectionPageSQL",
		"parseFavoriteItemPageRequest", "favoriteItemPageSQL",
		`json:"hasMore"`, `json:"nextCursor"`,
		"favoriteMembershipSummarySQL", "favoriteMembershipSummary",
		"normalizeFavoriteMembershipDeltaRequest", "favoriteMembershipDelta",
		"delete from favorite_collection_items", "insert into favorite_collection_items",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("favorite handlers missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"loadFavoriteCollectionsWithQueryer", "loadVisibleFavoriteCollectionItems",
		"order by item.created_at desc`",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("favorite handlers retain unbounded path %q", forbidden)
		}
	}

	routes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(routes), `POST /api/v1/users/me/favorites/summary`) {
		t.Fatal("bounded favorite membership summary route is missing")
	}
	if !strings.Contains(string(routes), `PATCH /api/v1/users/me/favorites`) {
		t.Fatal("bounded favorite membership delta route is missing")
	}
}
