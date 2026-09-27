package database

import (
	"strings"
	"testing"
)

func TestPopularityUsesTheCompleteEffectiveDeveloperSet(t *testing.T) {
	t.Parallel()
	schema := strings.ToLower(strings.Join(ratingSchemaStatements(), "\n"))
	for _, required := range []string{
		"content_target_user_is_developer",
		"join effective_project_access access",
		"access.access_level='developer'",
		"access.user_id=target_user_id",
		"not content_target_user_is_developer",
		"not exists(select 1 from effective_project_access access",
		"route_developers as",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("popularity developer-set contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"content_target_owner_id",
		"order by access.user_id limit 1",
		"min(access.user_id)",
		"route_owners",
	} {
		if strings.Contains(schema, forbidden) {
			t.Errorf("popularity schema retains lossy developer mapping %q", forbidden)
		}
	}
}
