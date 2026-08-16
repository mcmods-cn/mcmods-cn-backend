package httpapi

import "testing"

func TestCommunityPostCategoriesAreScopedByKind(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"tutorial", "issue", "news", "discussion"} {
		categories := communityPostCategories(kind)
		if len(categories) == 0 || defaultCommunityPostCategory(kind) != categories[0] {
			t.Fatalf("%s has no usable category configuration", kind)
		}
		for _, category := range categories {
			if !communityPostCategoryAllowed(kind, category) {
				t.Fatalf("%s category %s was not accepted by its own whitelist", kind, category)
			}
		}
	}
	if communityPostCategoryAllowed("news", "crash") {
		t.Fatal("an issue-only category was accepted by the news catalog")
	}
	if communityPostCategoryAllowed("issue", "site") {
		t.Fatal("a news-only category was accepted by the issue catalog")
	}
}

func TestCommunityPostProjectTypesUsePublicWhitelist(t *testing.T) {
	t.Parallel()
	for _, projectType := range []string{"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"} {
		if !communityPostProjectTypeAllowed(projectType) {
			t.Fatalf("public project type %q was rejected", projectType)
		}
	}
	for _, projectType := range []string{"", "minecraft_server", "community_post", "mod') or true --"} {
		if communityPostProjectTypeAllowed(projectType) {
			t.Fatalf("unsupported project type %q was accepted", projectType)
		}
	}
}

func TestCommunityProjectFiltersUseTypedExistingReferences(t *testing.T) {
	t.Parallel()
	filters, ok := parseCommunityProjectFilters("mod:ABC123xyz,plugin:paper-tools")
	if !ok || len(filters) != 2 || filters[0] != "mod:abc123xyz" || filters[1] != "plugin:paper-tools" {
		t.Fatalf("unexpected normalized project filters: %#v %v", filters, ok)
	}
	for _, value := range []string{"plugin", "unknown:abc123xyz", "mod:abc') or true --"} {
		if _, valid := parseCommunityProjectFilters(value); valid {
			t.Fatalf("invalid project filter %q was accepted", value)
		}
	}
}
