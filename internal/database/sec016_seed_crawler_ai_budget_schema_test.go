package database

import (
	"strings"
	"testing"
)

func TestSEC016SeedCrawlerTranslationBudgetSchema(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("seed crawler AI budget reservations require schema generation 167, got %d", schemaGeneration)
	}
	statements := strings.Join(schemaInstallationStatements(), "\n")
	for _, required := range []string{
		"seed_crawler_translation_tasks",
		"usage_date date not null default current_date",
		"quota_reserved_tokens bigint not null default 0",
		"check(input_tokens>=0)",
		"check(output_tokens>=0)",
		"check(quota_reserved_tokens>=0)",
	} {
		if !strings.Contains(statements, required) {
			t.Fatalf("seed crawler translation budget schema is missing %q", required)
		}
	}
}
