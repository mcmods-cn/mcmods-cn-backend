package database

import (
	"strings"
	"testing"
)

func TestCatalogImportArtifactsHaveDurablePreUploadLineage(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("catalog import artifact lineage requires schema generation 168, got %d", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(catalogImportArtifactSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table catalog_import_job_artifacts",
		"job_id text not null references catalog_import_jobs(id) on delete restrict",
		"primary key(job_id,run_token,object_key)",
		"oss_file_id bigint not null unique references oss_files(id) on delete restrict",
		"check(status in ('planned','active','abandoned'))",
		"where status='planned'",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("catalog import artifact schema is missing %q", required)
		}
	}
}
