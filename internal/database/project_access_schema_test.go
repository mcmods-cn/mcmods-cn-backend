package database

import (
	"strings"
	"testing"
)

func TestProjectAccessSchemaHasOnlyEditorAndVerifiedAuthorPaths(t *testing.T) {
	schema := strings.ToLower(strings.Join(append(communitySchemaStatements(), projectAccessSchemaStatements()...), "\n"))
	for _, required := range []string{
		"effective_project_access",
		"'editor_assignment'::text",
		"'author_claim'::text",
		"'author_team_relation'::text",
		"claim.status='approved'",
		"binding.status='approved'",
		"binding.permission_granting",
		"membership.status='approved'",
		"uq_creator_claims_approved_author",
		"enforce_personal_author_claim",
		"enforce_team_author_membership",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("project access schema is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"team_claims",
		"claimed_by_user_id",
		"owner_user_id",
		"mod_membership_applications",
		"mod_memberships",
	} {
		if strings.Contains(schema, forbidden) {
			t.Fatalf("project access schema still contains forbidden legacy path %q", forbidden)
		}
	}
}

func TestPendingIdentityAndEditorRequestsDoNotChangePermissionVersion(t *testing.T) {
	schema := strings.ToLower(strings.Join(projectAccessSchemaStatements(), "\n"))
	for _, condition := range []string{
		"tg_table_name='creator_claims'",
		"new.status<>'approved'",
		"old.status<>'approved'",
		"tg_table_name='project_editor_assignments'",
		"new.status<>'active'",
		"old.status<>'active'",
	} {
		if !strings.Contains(schema, condition) {
			t.Fatalf("permission-version trigger is missing status guard %q", condition)
		}
	}
}

func TestProjectAuthorshipReviewQueueHasStablePageIndex(t *testing.T) {
	schema := strings.ToLower(strings.Join(baselineSchemaStatements(), "\n"))
	if !strings.Contains(schema, "idx_content_creator_bindings_review_page") ||
		!strings.Contains(schema, "content_creator_bindings(status,created_at,id)") {
		t.Fatal("project authorship review queue is missing its status/time/id keyset index")
	}
}
