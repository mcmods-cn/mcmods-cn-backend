package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestProjectMutationBypassesReviewUsesExistingPermissionSystem(t *testing.T) {
	for _, code := range []string{"admin.*", "project.no-review", "content.no-review"} {
		claims := security.Claims{PermissionRules: []security.PermissionRule{{Code: code, Allow: true, Priority: 10}}}
		if !projectMutationBypassesReview(claims) {
			t.Fatalf("permission %q should bypass project review", code)
		}
	}
	if projectMutationBypassesReview(security.Claims{}) {
		t.Fatal("a user without an explicit bypass permission must enter review")
	}
}
