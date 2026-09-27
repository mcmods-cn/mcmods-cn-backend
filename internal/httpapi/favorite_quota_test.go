package httpapi

import (
	"errors"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestFavoriteQuotaPolicyDefaultsAndHardBounds(t *testing.T) {
	defaults := normalizedFavoriteQuotaPolicy(config.FavoriteConfig{})
	if defaults.MaximumCollections != defaultFavoriteCollectionsPerUser || defaults.MaximumItems != defaultFavoriteItemsPerUser {
		t.Fatalf("favorite quota defaults = %+v", defaults)
	}
	overrides := normalizedFavoriteQuotaPolicy(config.FavoriteConfig{MaxCollectionsPerUser: 7, MaxItemsPerUser: 321})
	if overrides.MaximumCollections != 7 || overrides.MaximumItems != 321 {
		t.Fatalf("favorite quota overrides = %+v", overrides)
	}
	bounded := normalizedFavoriteQuotaPolicy(config.FavoriteConfig{MaxCollectionsPerUser: 100_000, MaxItemsPerUser: 2_000_000})
	if bounded.MaximumCollections != hardFavoriteCollectionsPerUser || bounded.MaximumItems != hardFavoriteItemsPerUser {
		t.Fatalf("favorite quota hard bounds = %+v", bounded)
	}
	if err := evaluateFavoriteItemGrowth(10, 0, 10); err != nil {
		t.Fatalf("idempotent quota evaluation failed: %v", err)
	}
	if err := evaluateFavoriteItemGrowth(11, -1, 10); err != nil {
		t.Fatalf("over-limit cleanup failed: %v", err)
	}
	if err := evaluateFavoriteItemGrowth(10, 1, 10); !errors.Is(err, errFavoriteItemQuota) {
		t.Fatalf("positive growth at quota = %v", err)
	}
}
