package httpapi

import (
	"net/http"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func TestInferredActivityObjectCoversPublicFeatureRoutes(t *testing.T) {
	t.Parallel()
	tests := map[string]int16{
		"/api/mods/example":                                  activity.ObjectMod,
		"/api/modpacks/example":                              activity.ObjectModpack,
		"/api/servers/example":                               activity.ObjectServer,
		"/api/plugins/example":                               activity.ObjectPlugin,
		"/api/maps/example":                                  activity.ObjectMap,
		"/api/resource-packs/example":                        activity.ObjectResourcePack,
		"/api/shaders/example":                               activity.ObjectShaderPack,
		"/api/datapacks/example":                             activity.ObjectDatapack,
		"/api/addons/example":                                activity.ObjectAddon,
		"/api/content-projects/plugin/example":               activity.ObjectPlugin,
		"/api/content-projects/map/example":                  activity.ObjectMap,
		"/api/content-project-imports/resource_pack/example": activity.ObjectResourcePack,
		"/api/news/example":                                  activity.ObjectCommunityPost,
		"/api/tutorials/example":                             activity.ObjectCommunityPost,
		"/api/issues/example":                                activity.ObjectCommunityPost,
		"/api/discussions/example":                           activity.ObjectCommunityPost,
		"/api/community/posts/example":                       activity.ObjectCommunityPost,
		"/api/change-requests/example":                       activity.ObjectReview,
		"/api/v1/content-revisions/example":                  activity.ObjectReview,
		"/api/v1/admin/server-reviews/example":               activity.ObjectReview,
		"/api/v1/admin/reports/example":                      activity.ObjectReview,
		"/api/skins/example":                                 activity.ObjectSkin,
		"/api/player-profiles/example":                       activity.ObjectPlayerProfile,
	}
	for path, want := range tests {
		path, want := path, want
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if got := inferredActivityObject(path); got != want {
				t.Fatalf("inferredActivityObject(%q) = %d, want %d", path, got, want)
			}
		})
	}
}

func TestInferredActivityAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		method string
		path   string
		want   int16
	}{
		{http.MethodGet, "/api/mods", activity.ActionView},
		{http.MethodPost, "/api/mods", activity.ActionCreate},
		{http.MethodPatch, "/api/mods/abc", activity.ActionEdit},
		{http.MethodDelete, "/api/mods/abc", activity.ActionDelete},
		{http.MethodPost, "/api/files/abc/download", activity.ActionDownload},
		{http.MethodPost, "/api/creators/abc/claim", activity.ActionClaim},
	}
	for _, test := range tests {
		request, err := http.NewRequest(test.method, test.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := inferredActivityAction(request); got != test.want {
			t.Fatalf("inferredActivityAction(%s %s) = %d, want %d", test.method, test.path, got, test.want)
		}
	}
}

func TestInferredActivityActionUsesWholePathSegments(t *testing.T) {
	tests := []struct {
		name, method, path string
		want               int16
	}{
		{name: "users is a view not use", method: http.MethodGet, path: "/api/v1/users/me/statistics", want: activity.ActionView},
		{name: "user card is a view", method: http.MethodGet, path: "/api/v1/users/abc123xyz/card", want: activity.ActionView},
		{name: "explicit use action", method: http.MethodPost, path: "/api/v1/shop/items/abc123xyz/use", want: activity.ActionUse},
		{name: "explicit upload", method: http.MethodPost, path: "/api/v1/oss/uploads", want: activity.ActionUpload},
		{name: "ordinary create", method: http.MethodPost, path: "/api/v1/users/me/settings", want: activity.ActionCreate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := inferredActivityAction(request); got != test.want {
				t.Fatalf("inferredActivityAction(%s %s)=%d, want %d", test.method, test.path, got, test.want)
			}
		})
	}
}

func TestHighFrequencyAutosavesAreExcluded(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/api/v1/users/me/drafts",
		"/api/v1/users/me/drafts/complete",
		"/api/v1/users/me/markdown-playground",
		"/api/v1/messages/conversations/abc/presence",
	} {
		request, err := http.NewRequest(http.MethodPost, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !isHighFrequencyActivityExcluded(request) {
			t.Fatalf("expected %q to be excluded from activity storage", path)
		}
	}
	for _, path := range []string{
		"/api/v1/mods/example/icon",
		"/api/v1/mod-imports/job123",
		"/api/v1/community/translations/job123",
	} {
		request, err := http.NewRequest(http.MethodGet, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !isHighFrequencyActivityExcluded(request) {
			t.Fatalf("expected automatic GET %q to be excluded from activity storage", path)
		}
	}
}

func TestActivityObjectTypeForRevisionEntities(t *testing.T) {
	t.Parallel()
	tests := map[string]int16{
		"mod":               activity.ObjectMod,
		"modpack":           activity.ObjectModpack,
		"plugin":            activity.ObjectPlugin,
		"map":               activity.ObjectMap,
		"resource_pack":     activity.ObjectResourcePack,
		"shader_pack":       activity.ObjectShaderPack,
		"datapack":          activity.ObjectDatapack,
		"addon":             activity.ObjectAddon,
		"community_post":    activity.ObjectCommunityPost,
		"minecraft_server":  activity.ObjectServer,
		"skin":              activity.ObjectSkin,
		"project_changelog": activity.ObjectChangelog,
		"rating":            activity.ObjectRating,
	}
	for entityType, want := range tests {
		if got := activityObjectTypeForEntityType(entityType); got != want {
			t.Fatalf("activityObjectTypeForEntityType(%q) = %d, want %d", entityType, got, want)
		}
	}
}
