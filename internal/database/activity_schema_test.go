package database

import (
	"strings"
	"testing"
)

func TestActivityTableUsesLeanNumericStorage(t *testing.T) {
	t.Parallel()
	var definition string
	for _, statement := range communitySchemaStatements() {
		if strings.Contains(statement, "create table if not exists user_activity_events") {
			definition = strings.ToLower(statement)
			break
		}
	}
	if definition == "" {
		t.Fatal("user_activity_events schema was not found")
	}
	for _, required := range []string{"id bigserial primary key", "user_id bigint", "object_route_id bigint references public_routes(id)", "occurred_at timestamptz"} {
		if !strings.Contains(definition, required) {
			t.Fatalf("activity schema does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"metadata jsonb", "object_public_id", "user_public_id", "public_id text"} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("activity schema still contains high-volume field %q", forbidden)
		}
	}
}
