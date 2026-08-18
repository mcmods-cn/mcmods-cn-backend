package httpapi

import "testing"

func TestNormalizeLogConfigIncludesEveryCleanupCategory(t *testing.T) {
	config := normalizeLogConfig(logRetentionConfig{DefaultDays: 30})
	for _, category := range []string{
		"system", "user_interaction", "admin_operation", "permission_change", "login_security",
		"api_access", "file_upload", "file_scan", "ai_call", "download",
	} {
		if config.CategoryDays[category] <= 0 {
			t.Errorf("cleanup category %q has no retention policy", category)
		}
	}
}

func TestDownloadApplicationLogsParticipateInCleanup(t *testing.T) {
	for _, category := range appLogRetentionCategories {
		if category == "download" {
			return
		}
	}
	t.Fatal("download application logs are missing from cleanup categories")
}
