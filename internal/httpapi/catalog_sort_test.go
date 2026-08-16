package httpapi

import (
	"strings"
	"testing"
)

func TestParseCatalogSortRejectsUnknownValues(t *testing.T) {
	t.Parallel()
	if _, ok := parseCatalogSort("heat desc; drop table users"); ok {
		t.Fatal("untrusted sort expression was accepted")
	}
	if sort, ok := parseCatalogSort("heat"); !ok || sort != catalogSortHeat {
		t.Fatalf("heat sort was not accepted: %q %v", sort, ok)
	}
}

func TestCatalogHeatOrderIsDescendingNullSafeAndStable(t *testing.T) {
	t.Parallel()
	order := catalogOrderSQL(catalogSortHeat, false, 0, "project.updated_at", "project.id", "project.primary_name")
	for _, expected := range []string{"coalesce(popularity.heat_score,0) desc", "project.updated_at desc", "project.id desc"} {
		if !strings.Contains(order, expected) {
			t.Fatalf("heat order %q does not contain %q", order, expected)
		}
	}
}

func TestCatalogIndexedOrderRetainsStableTieBreakers(t *testing.T) {
	t.Parallel()
	order := catalogOrderSQL(catalogSortRelevance, true, 4, "project.updated_at", "project.id", "project.primary_name")
	if order != "array_position($4::bigint[],project.id),project.updated_at desc,project.id desc" {
		t.Fatalf("unexpected indexed order: %q", order)
	}
}

func TestParseCatalogUpdatedRangeUsesWhitelist(t *testing.T) {
	t.Parallel()
	if days, ok := parseCatalogUpdatedRange("quarter"); !ok || days != 90 {
		t.Fatalf("unexpected quarter range: %d %v", days, ok)
	}
	if days, ok := parseCatalogUpdatedRange("stale"); !ok || days != -365 {
		t.Fatalf("unexpected stale range: %d %v", days, ok)
	}
	if _, ok := parseCatalogUpdatedRange("1 day; delete from mods"); ok {
		t.Fatal("arbitrary updated range was accepted")
	}
}

func TestCatalogFeatureFilterUsesWhitelist(t *testing.T) {
	t.Parallel()
	if !validCatalogFeatures([]string{"gallery", "downloads"}) {
		t.Fatal("known feature filters were rejected")
	}
	if validCatalogFeatures([]string{"reviewed') or true --"}) {
		t.Fatal("arbitrary feature filter was accepted")
	}
}

func TestCatalogHeatOrderAcrossProjectTypes(t *testing.T) {
	t.Parallel()
	for name, alias := range map[string]string{
		"mod": "m", "modpack": "pack", "plugin_or_map": "project", "server": "server",
	} {
		t.Run(name, func(t *testing.T) {
			order := catalogOrderSQL(catalogSortHeat, false, 0, alias+".updated_at", alias+".id", alias+".primary_name")
			for _, fragment := range []string{"coalesce(popularity.heat_score,0) desc", alias + ".updated_at desc", alias + ".id desc"} {
				if !strings.Contains(order, fragment) {
					t.Fatalf("heat ordering %q is missing %q", order, fragment)
				}
			}
		})
	}
}

func TestCatalogDatabaseFiltersCoverVersionAndCategoryBeforePagination(t *testing.T) {
	t.Parallel()
	for name, filter := range map[string]string{
		"mod":            publicModCatalogFilter,
		"modpack":        publicModpackCatalogFilter,
		"simple_project": simpleProjectCatalogFilter,
	} {
		normalized := strings.ToLower(filter)
		if !strings.Contains(normalized, "version") || !strings.Contains(normalized, "categor") {
			t.Fatalf("%s database filter does not include both category and version filtering", name)
		}
	}
}

func TestSimpleProjectVersionFilterSupportsAnyAndAllMatching(t *testing.T) {
	t.Parallel()
	for _, operator := range []string{"project.minecraft_versions && $5::text[]", "project.minecraft_versions @> $5::text[]"} {
		if !strings.Contains(simpleProjectCatalogFilter, operator) {
			t.Fatalf("simple project version filter is missing %q", operator)
		}
	}
}

func TestCatalogTextAndBooleanParametersAreBounded(t *testing.T) {
	t.Parallel()
	if _, ok := parseCatalogQuery(strings.Repeat("a", 201)); ok {
		t.Fatal("overlong catalog query was accepted")
	}
	if _, ok := parseCatalogScalar("1.20.1,1.21"); ok {
		t.Fatal("multi-value input was accepted by a scalar parameter")
	}
	if _, ok := parseCatalogBoolean("yes"); ok {
		t.Fatal("invalid boolean enum was accepted")
	}
	if everyCatalogValueAllowed([]string{"active", "dropped'); --"}, allowedModStatuses) {
		t.Fatal("unknown catalog enum was accepted")
	}
}

func TestParseCatalogVersionModeUsesWhitelist(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "any", "all"} {
		if _, ok := parseCatalogVersionMode(value); !ok {
			t.Fatalf("valid version mode %q was rejected", value)
		}
	}
	if _, ok := parseCatalogVersionMode("all') or true --"); ok {
		t.Fatal("arbitrary version mode was accepted")
	}
}
