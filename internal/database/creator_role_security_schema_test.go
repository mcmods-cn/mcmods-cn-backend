package database

import (
	"strings"
	"testing"
)

func TestCreatorImportRolesUseExplicitGeneration106GrantWhitelist(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	schema := strings.Join(communitySchemaStatements(), "\n")
	for _, required := range []string{
		"(1,'owner','Owner','Project owner or primary maintainer',false,true)",
		"(2,'developer','Developer','Software developer',false,true)",
		"(9,'leader','Leader','Team leader',false,false)",
		"(10,'contributor','Contributor','General contribution without project access',false,false)",
		"(11,'maintainer','Maintainer','Software maintainer with project access',false,true)",
		"greatest(11,coalesce((select max(id) from creator_role_definitions),11))",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("creator role baseline is missing %q", required)
		}
	}
}
