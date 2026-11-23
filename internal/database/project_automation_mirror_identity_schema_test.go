package database

import (
	"strings"
	"testing"
)

func TestMirroredProjectFilesAreUniqueByProviderIdentityNotContent(t *testing.T) {
	definition := strings.ToLower(strings.Join(governanceAutomationSchemaStatements(), "\n"))
	if !strings.Contains(definition, "unique(project_route_id,source_type,external_file_id)") {
		t.Fatal("project-scoped provider file identity constraint is missing")
	}
	if strings.Contains(definition, "unique(source_type,external_file_id)") || strings.Contains(definition, "unique(source_type,file_sha256,byte_size)") {
		t.Fatal("project mirrors still have a global file or content uniqueness constraint")
	}
}
