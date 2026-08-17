package httpapi

import (
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestProjectScopedReviewerCannotReviewOwnSubmission(t *testing.T) {
	claims := security.Claims{Subject: 42, PermissionRules: []security.PermissionRule{
		{Code: "project.review.abc123def", Allow: true, Priority: 100},
	}}
	if !canReviewProjectSubmission(claims, "abc123def", 41) {
		t.Fatal("project reviewer could not review another contributor's submission")
	}
	if canReviewProjectSubmission(claims, "abc123def", 42) {
		t.Fatal("project-scoped reviewer was allowed to approve their own submission")
	}
	if canReviewProjectSubmission(claims, "other1234", 41) {
		t.Fatal("project-scoped reviewer was allowed to review another project")
	}
}

func TestGlobalReviewerRetainsQueueAccess(t *testing.T) {
	for _, permission := range []string{"content.review", "project.review", "admin.*"} {
		claims := security.Claims{Subject: 42, PermissionRules: []security.PermissionRule{{Code: permission, Allow: true, Priority: 100}}}
		if !canReviewProjectSubmission(claims, "abc123def", 42) {
			t.Fatalf("global reviewer %q could not resolve a project submission", permission)
		}
	}
}

func TestReviewQueueFiltersAndFacets(t *testing.T) {
	items := []modContentReviewItem{
		{Category: "mod_content_version", Operation: "create", ProjectType: "mod", ModName: "Alpha", Username: "alice"},
		{Category: "community_tutorial", Operation: "edit", ModName: "Guide", Username: "bob"},
		{Category: "mod_content_version", Operation: "edit", ProjectType: "mod", ModName: "Beta", Username: "carol"},
	}
	request := httptest.NewRequest("GET", "/api/v1/reviews/content?category=mod_content_version&operation=edit&q=beta", nil)
	filtered := filterReviewQueue(items, request)
	if len(filtered) != 1 || filtered[0].ModName != "Beta" {
		t.Fatalf("unexpected filtered queue: %#v", filtered)
	}
	facets := reviewQueueFacets(items, func(item modContentReviewItem) string { return item.Category })
	if len(facets) != 2 || facets[1].Value != "mod_content_version" || facets[1].Count != 2 {
		t.Fatalf("unexpected review facets: %#v", facets)
	}
}

func TestReviewQueueItemRequiresProjectPermission(t *testing.T) {
	claims := security.Claims{Subject: 7, PermissionRules: []security.PermissionRule{{Code: "project.review.abc123def", Allow: true, Priority: 10}}}
	projectItem := modContentReviewItem{ProjectID: "abc123def", SubmittedBy: 8}
	if !reviewQueueItemAllowed(claims, projectItem) {
		t.Fatal("project-scoped queue item was hidden from its reviewer")
	}
	if reviewQueueItemAllowed(claims, modContentReviewItem{RequiresGlobal: true, SubmittedBy: 8}) {
		t.Fatal("global queue item leaked to a project-scoped reviewer")
	}
}

func TestReviewQueueAccessRequiresResolvedReviewPermission(t *testing.T) {
	projectReviewer := security.Claims{PermissionRules: []security.PermissionRule{{Code: "project.review.abc123def", Allow: true, Priority: 10}}}
	if !canAccessAnyReviewQueue(projectReviewer) {
		t.Fatal("project-scoped reviewer could not access the queue")
	}
	templateOnly := security.Claims{PermissionRules: []security.PermissionRule{{Code: "project.review.<projectID>", Allow: true, Priority: 10}}}
	if canAccessAnyReviewQueue(templateOnly) {
		t.Fatal("unresolved permission template granted review queue access")
	}
	if canAccessAnyReviewQueue(security.Claims{}) {
		t.Fatal("user without review permission gained queue access")
	}
}

func TestProjectReviewIDsAreResolvedDeduplicatedAndSorted(t *testing.T) {
	claims := security.Claims{PermissionRules: []security.PermissionRule{
		{Code: "project.review.zeta12345", Allow: true, Priority: 10},
		{Code: "project.review.alpha1234", Allow: true, Priority: 10},
		{Code: "project.review.zeta12345", Allow: true, Priority: 5},
		{Code: "project.review.<projectID>", Allow: true, Priority: 100},
		{Code: "project.review.denied123", Allow: false, Priority: 100},
	}}
	projectIDs := projectReviewIDs(claims)
	if len(projectIDs) != 2 || projectIDs[0] != "alpha1234" || projectIDs[1] != "zeta12345" {
		t.Fatalf("unexpected project review IDs: %#v", projectIDs)
	}
}
