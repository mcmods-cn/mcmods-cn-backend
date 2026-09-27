package database

import (
	"strings"
	"testing"
)

func TestEconomySchemaEnforcesExecutableItemsAndAtomicPromotionSequences(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	economyDefinition := strings.ToLower(strings.Join(communitySchemaStatements(), "\n"))
	for _, required := range []string{
		"item_type text not null check(item_type in ('profile_background','project_heat_boost','server_heat_boost'))",
	} {
		if !strings.Contains(economyDefinition, required) {
			t.Fatalf("economy schema is missing %q", required)
		}
	}
	ratingDefinition := strings.ToLower(strings.Join(ratingSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table content_heat_promotion_counters",
		"object_route_id bigint primary key",
		"unique(object_route_id,sequence_no)",
	} {
		if !strings.Contains(ratingDefinition, required) {
			t.Fatalf("promotion schema is missing %q", required)
		}
	}
}
