package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteMembershipReplacementValidatesBeforeMutationSource(t *testing.T) {
	raw, err := os.ReadFile("favorite_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *Server) setFavoriteMembership(")
	end := strings.Index(source, "func (s *Server) favoriteCollectionItems(")
	if start < 0 || end <= start {
		t.Fatal("setFavoriteMembership source bounds were not found")
	}
	handler := source[start:end]
	normalizeAt := strings.Index(handler, "normalizeFavoriteCollectionIDs(request.CollectionIDs, true)")
	beginAt := strings.Index(handler, "s.beginFavoriteMembershipTx(")
	validateAt := strings.Index(handler, "validateFavoriteCollectionsTx(")
	defaultAt := strings.Index(handler, "ensureDefaultFavoriteCollectionWithQueryer(")
	deleteAt := strings.Index(handler, "delete from favorite_collection_items")
	if normalizeAt < 0 || beginAt < 0 || normalizeAt > beginAt {
		t.Fatal("replacement collection IDs must be normalized before opening the transaction")
	}
	if validateAt < 0 || defaultAt < 0 || deleteAt < 0 || validateAt > defaultAt || validateAt > deleteAt {
		t.Fatal("every requested collection must be locked and validated before any replacement side effect")
	}
	for _, required := range []string{
		"for _, id := range request.CollectionIDs",
		"if existing[id] {",
		"tag.RowsAffected() != 1",
	} {
		if !strings.Contains(handler, required) {
			t.Fatalf("replacement must retain existing relations and strictly verify every new insert: missing %q", required)
		}
	}
}
