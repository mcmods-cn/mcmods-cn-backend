package database

import (
	"os"
	"strings"
	"testing"
)

func TestOSSQuotaBucketsUseGeneration130TriggersAndReconciliation(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation = %d, want 168", schemaGeneration)
	}
	sourceBytes, err := os.ReadFile("migrations.go")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(sourceBytes))
	for _, fragment := range []string{
		"create table oss_user_quota_usage",
		"create table oss_user_daily_quota_usage",
		"create table oss_user_upload_quota_reservations",
		"trg_oss_files_quota_usage",
		"rebuild_oss_user_quota_usage",
		"rebuild_oss_user_daily_quota_usage",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("Generation 133 OSS quota schema is missing %q", fragment)
		}
	}
}
