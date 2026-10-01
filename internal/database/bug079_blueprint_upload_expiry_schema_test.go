package database

import (
	"os"
	"strings"
	"testing"
)

func TestBlueprintUploadsHaveAnExplicitGeneration155Expiry(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation = %d, want 168", schemaGeneration)
	}
	source, err := os.ReadFile("migrations.go")
	if err != nil {
		t.Fatal(err)
	}
	migrations := string(source)
	for _, required := range []string{
		"upload_expires_at timestamptz",
		"idx_blueprints_upload_expiry",
		"where status='uploading' and upload_expires_at is not null",
	} {
		if !strings.Contains(migrations, required) {
			t.Fatalf("blueprint upload expiry schema is missing %q", required)
		}
	}
}
