package httpapi

import (
	"strings"
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

func TestReviewQueueFilteringPaginationAndFacetsStayInDatabase(t *testing.T) {
	for _, required := range []string{
		"visible as materialized",
		"filtered as materialized",
		"strpos(lower(project_name",
		"count(*)::bigint from filtered",
		"from visible where category<>'' group by category",
		"order by created_at,id,source limit $9 offset $10",
	} {
		if !strings.Contains(contentReviewQueueQuery, required) {
			t.Errorf("review queue query is missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(contentReviewQueueQuery), "limit 2000") {
		t.Fatal("fixed review queue truncation remains")
	}
	if reviewQueueOffset("9223372036854775807") != int64(9223372036854775807) || reviewQueueOffset("-1") != 0 {
		t.Fatal("review queue offset does not preserve the reachable int64 range")
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

func TestPendingReviewVisibilityUsesExactProjectScopeAndSubmitter(t *testing.T) {
	projectClaims := security.Claims{Subject: 42, PermissionRules: []security.PermissionRule{
		{Code: "project.review.abc123def", Allow: true, Priority: 100},
		{Code: "project.review.<projectID>", Allow: true, Priority: 100},
	}}
	visibility := projectPendingReviewVisibility(projectClaims, "abc123def", false)
	if !visibility.allows("approved", 42) || !visibility.allows("pending", 41) {
		t.Fatal("exact project reviewer could not read approved or another submitter's pending revision")
	}
	if visibility.allows("pending", 42) || visibility.allows("rejected", 42) {
		t.Fatal("project reviewer could read their own non-approved revision")
	}
	if projectPendingReviewVisibility(projectClaims, "other1234", false).allows("pending", 41) {
		t.Fatal("project review scope leaked to another project")
	}
	if projectPendingReviewVisibility(projectClaims, "abc123def", false).allows("pending", 0) {
		t.Fatal("project reviewer could read a pending revision without an attributable submitter")
	}
	if !projectPendingReviewVisibility(projectClaims, "other1234", true).allows("pending", 42) {
		t.Fatal("project editor could not read pending history")
	}
	globalClaims := security.Claims{Subject: 42, PermissionRules: []security.PermissionRule{{Code: "project.review", Allow: true, Priority: 100}}}
	if !projectPendingReviewVisibility(globalClaims, "abc123def", false).allows("pending", 42) {
		t.Fatal("site-wide project reviewer could not read a self-submitted pending revision")
	}
}
