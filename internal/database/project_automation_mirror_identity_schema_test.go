package database

import (
	"strings"
	"testing"
)

func TestMirroredProjectFilesAreUniqueByProviderIdentityNotContent(t *testing.T) {
	definition := strings.ToLower(strings.Join(governanceAutomationSchemaStatements(), "\n"))
	if !strings.Contains(definition, "unique(source_type,external_file_id)") {
		t.Fatal("provider file identity constraint is missing")
	}
	if strings.Contains(definition, "unique(source_type,file_sha256,byte_size)") {
		t.Fatal("content bytes still prevent distinct provider files from being mirrored")
	}
}
