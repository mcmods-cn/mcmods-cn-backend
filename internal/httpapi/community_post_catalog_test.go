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
