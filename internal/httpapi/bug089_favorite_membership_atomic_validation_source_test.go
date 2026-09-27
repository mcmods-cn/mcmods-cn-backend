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
	if strings.Contains(handler, "for _, collectionPublicID := range request.CollectionIDs") {
		t.Fatal("replacement must not silently ignore individual collection insert misses")
	}
	if !strings.Contains(handler, "tag.RowsAffected() != int64(len(request.CollectionIDs))") {
		t.Fatal("bulk replacement must verify the exact inserted relation count")
	}
}
