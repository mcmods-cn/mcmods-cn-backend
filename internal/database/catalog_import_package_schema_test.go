package database

import (
	"strings"
	"testing"
)

func TestCatalogImportPackageSourceIsImmutableAndNotGloballyDeduplicated(t *testing.T) {
	definition := strings.ToLower(strings.Join(baselineSchemaStatements(), "\n"))
	for _, forbidden := range []string{
		"sha256 text not null unique",
		"archive_file_id bigint references oss_files(id) on delete set null",
	} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("catalog package schema retains unsafe source ownership %q", forbidden)
		}
	}
	for _, required := range []string{
		"archive_file_id bigint not null unique references oss_files(id) on delete restrict",
		"uploaded_by bigint not null references users(id) on delete restrict",
		"content_verified_at timestamptz",
		"prevent_catalog_import_package_source_mutation",
		"new.archive_file_id is distinct from old.archive_file_id",
		"idx_catalog_import_packages_verified_sha",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("catalog package source invariant is missing %q", required)
		}
	}
}
