package database

import (
	"strings"
	"testing"
)

func TestLogPaginationHasGeneration125SearchAndPageSchema(t *testing.T) {
	if schemaGeneration != 167 {
		t.Fatalf("schema generation=%d want 167", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	for _, required := range []string{
		"search_document tsvector not null default ''::tsvector",
		"trg_app_logs_search_document",
		"trg_permission_audit_logs_search_document",
		"trg_user_login_logs_search_document",
		"trg_oss_upload_logs_search_document",
		"idx_app_logs_search_document",
		"idx_permission_audit_logs_search_document",
		"idx_user_login_logs_search_document",
		"idx_oss_upload_logs_search_document",
		"idx_app_logs_category_created_at on app_logs (category, created_at desc, id desc)",
		"idx_permission_audit_logs_created_at on permission_audit_logs (created_at desc, id desc)",
		"idx_user_login_logs_created_at on user_login_logs (created_at desc, id desc)",
		"idx_oss_upload_logs_created_at on oss_upload_logs (created_at desc, id desc)",
		"idx_oss_scan_logs_created_at on oss_scan_logs (created_at desc, id desc)",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("log schema is missing %q", required)
		}
	}
}
