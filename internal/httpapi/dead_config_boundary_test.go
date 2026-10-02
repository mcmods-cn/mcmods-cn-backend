package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestDEAD003ProjectFollowPreferenceHasOneMutableOwnerBoundary(t *testing.T) {
	handlers := readDEADConfigSource(t, "project_follow_handlers.go")
	routes := readDEADConfigSource(t, "server.go")
	for _, required := range []string{
		"updateProjectFollowNotifications",
		"notifications_enabled=$3",
		"returning follow.notifications_enabled",
	} {
		if !strings.Contains(handlers, required) {
			t.Fatalf("project follow preference boundary is missing %q", required)
		}
	}
	if !strings.Contains(routes, `PATCH /api/v1/projects/{publicId}/follow", s.requirePermission("project.follow", s.updateProjectFollowNotifications)`) {
		t.Fatal("project follow notification preference is not routed through project.follow")
	}
	if strings.Contains(handlers, `"notificationsEnabled": true`) {
		t.Fatal("project follow response still hard-codes the notification preference")
	}
}

func TestDEAD012StickerCatalogUsesTTLInvalidationWithoutDeadVersionState(t *testing.T) {
	handlers := readDEADConfigSource(t, "sticker_handlers.go")
	schema := readDEADConfigSource(t, "../database/engagement_export_schema.go")
	for _, forbidden := range []string{
		"sticker_catalog_state",
		"bumpStickerCatalogVersion",
		`"version": version`,
		"failed to load sticker catalog version",
	} {
		if strings.Contains(handlers, forbidden) || strings.Contains(schema, forbidden) {
			t.Fatalf("sticker catalog retains dead version authority %q", forbidden)
		}
	}
}

func readDEADConfigSource(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
