package httpapi

import (
	"strings"
	"testing"
)

func TestPublicContributionActivityUsesCurrentAnonymousVisibility(t *testing.T) {
	query := strings.ToLower(userContributionRecentActivityQuery())
	for _, required := range []string{
		"join public_routes route",
		"route.entity_type='mod'",
		"review_status='approved'",
		"route.entity_type='community_post'",
		"status='active'",
		"route.entity_type='skin'",
		"visibility='public'",
		"else false end",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("public contribution query is missing visibility policy %q", required)
		}
	}
	for _, forbidden := range []string{
		"left join public_routes",
		"revision.snapshot",
		"join content_revisions",
		"visibility in ('public','unlisted')",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("public contribution query retains historical disclosure source %q", forbidden)
		}
	}
}
