package database

import (
	"strings"
	"testing"
)

func TestAdminDashboardUsesPersistedCompactProjections(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(adminDashboardSchemaStatements(), "\n"))
	for _, expected := range []string{
		"create view top_level_project_catalog",
		"create table site_daily_metrics",
		"create table site_view_daily",
		"create table site_daily_active_users",
		"create table site_monthly_active_users",
		"record_site_activity_batch",
		"refresh_site_current_counters",
		"idx_change_requests_submitted_at",
	} {
		if !strings.Contains(definition, expected) {
			t.Fatalf("admin dashboard schema is missing %q", expected)
		}
	}
	if strings.Contains(definition, "jsonb") {
		t.Fatal("dashboard projections should not duplicate high-volume event metadata")
	}
}
