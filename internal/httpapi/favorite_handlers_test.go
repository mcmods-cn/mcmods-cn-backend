package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteReadsAndWritesKeepTargetVisibilityBoundary(t *testing.T) {
	handlers, err := os.ReadFile("favorite_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	handlerSource := string(handlers)
	for _, required := range []string{
		"beginFavoriteMembershipTx(r.Context(), claims.Subject)",
		"beginFavoriteMembershipTx(r.Context(), userID)",
		"resolveFavoriteMutationTargetWithQueryer(r.Context(), tx, request.EntityType, request.EntityPublicID, claims, len(request.AddCollectionIDs) == 0)",
		"resolveFavoriteMutationTargetWithQueryer(r.Context(), tx, request.EntityType, request.EntityPublicID, claims, len(request.CollectionIDs) == 0)",
		"writeFavoriteCollectionPage(w, r, identity.InternalID, false, currentClaims(r))",
		"parseFavoriteItemPageRequest(r.URL.Query(), ownerID, includePrivate, collectionPublicID, claims)",
		"item.entity_type in ('mod','modpack','blueprint')",
	} {
		if !strings.Contains(handlerSource, required) {
			t.Fatalf("favorite visibility boundary is missing %q", required)
		}
	}
	quota, err := os.ReadFile("favorite_quota.go")
	if err != nil {
		t.Fatal(err)
	}
	quotaSource := string(quota)
	lockAt := strings.Index(quotaSource, "select pg_advisory_lock(")
	beginAt := strings.Index(quotaSource, "connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})")
	if lockAt < 0 || beginAt < 0 || lockAt >= beginAt {
		t.Fatal("favorite membership must acquire its database quota lock before opening the repeatable-read visibility transaction")
	}

	exports, err := os.ReadFile("favorite_modpack_export.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"buildFavoriteModpackExportPreview(r.Context(), currentClaims(r),",
		`favoriteTargetVisibilitySQL("$2", "$3")`,
		"resolveSynchronizedMRPackLoaderVersion(ctx, s.db, request.MinecraftVersion, request.Loader)",
	} {
		if !strings.Contains(string(exports), required) {
			t.Fatalf("favorite MRPack export visibility boundary is missing %q", required)
		}
	}
	if strings.Contains(string(exports), "resolveMRPackLoaderVersion(ctx, request.MinecraftVersion, request.Loader)") {
		t.Fatal("favorite MRPack export still resolves loader artifacts from the live upstream source")
	}
}
