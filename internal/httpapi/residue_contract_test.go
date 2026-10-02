package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestDevelopmentResidueRoutesAndWrappersStayRemoved(t *testing.T) {
	tests := []struct {
		file      string
		forbidden []string
		required  []string
	}{
		{
			file: "server.go",
			forbidden: []string{
				`GET /api/v1/notifications/unread`,
				`GET /api/v1/admin/nav`,
				`s.adminDashboard)`,
			},
			required: []string{
				`GET /api/v1/me/unread-summary`,
				`s.loadAdminDashboard)`,
			},
		},
		{
			file: "admin_handlers.go",
			forbidden: []string{
				`func (s *Server) adminDashboard(`,
				`func (s *Server) adminNav(`,
				`func adminNavigation(`,
			},
		},
		{
			file:      "log_share_handlers.go",
			forbidden: []string{`jsonUnmarshal`},
		},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			raw, err := os.ReadFile(test.file)
			if err != nil {
				t.Fatal(err)
			}
			source := string(raw)
			for _, fragment := range test.forbidden {
				if strings.Contains(source, fragment) {
					t.Fatalf("%s restored obsolete fragment %q", test.file, fragment)
				}
			}
			for _, fragment := range test.required {
				if !strings.Contains(source, fragment) {
					t.Fatalf("%s lost canonical fragment %q", test.file, fragment)
				}
			}
		})
	}
}

func TestPermissionRuntimeTestsContainOnlyPermissionDomain(t *testing.T) {
	raw, err := os.ReadFile("permission_runtime_test.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"TestOSSFileRecordExposesConversionDetails",
		"TestReadAPNGAnimationControl",
		"TestShouldPersistMarkdownImageAsWebP",
		"TestShiftRoleTrackRoles",
	} {
		if strings.Contains(source, fragment) {
			t.Fatalf("permission_runtime_test.go contains unrelated test %s", fragment)
		}
	}
}

func TestAdminDashboardIntegrationTestsStayInTheirDomains(t *testing.T) {
	tests := map[string]string{
		"TestStickerMarkdownUsageQueryIntegration":               "sticker_markdown_usage_integration_test.go",
		"TestFavoriteExportExhaustedLeaseRecoveryIntegration":    "favorite_export_lease_recovery_integration_test.go",
		"TestSystemNotificationTranslationIsRejectedIntegration": "system_notification_translation_rejection_integration_test.go",
	}
	dashboard, err := os.ReadFile("admin_dashboard_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for testName, fileName := range tests {
		if strings.Contains(string(dashboard), testName) {
			t.Fatalf("admin dashboard integration file contains unrelated test %s", testName)
		}
		domain, readErr := os.ReadFile(fileName)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(domain), "func "+testName+"(") {
			t.Fatalf("%s does not own %s", fileName, testName)
		}
	}
}
