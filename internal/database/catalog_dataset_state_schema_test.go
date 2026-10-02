package database

import (
	"strings"
	"testing"
)

func TestCatalogDatasetStateIsConstantTimeAuthority(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(catalogEditorSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table catalog_dataset_state",
		"singleton boolean primary key default true check(singleton)",
		"version bigint not null default 1 check(version>0)",
		"insert into catalog_dataset_state(singleton,version) values(true,1)",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("catalog dataset schema is missing %q", required)
		}
	}
}
