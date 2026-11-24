package httpapi

import (
	"context"
	"net/http/httptest"
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

func TestRequestActivityTracksMatchedObjectAcrossMiddleware(t *testing.T) {
	for _, test := range []struct {
		name, pattern, path, want string
		internal bool
	}{
		{"community post", "GET /api/v1/community/posts/{id}", "/api/v1/community/posts/c00000001", "c00000001", false},
		{"another post", "GET /api/v1/community/posts/{id}", "/api/v1/community/posts/c00000002", "c00000002", false},
		{"resource detail", "GET /api/v1/catalog/resources/{publicId}", "/api/v1/catalog/resources/r00000001", "r00000001", false},
		{"collection view", "GET /api/v1/catalog/resources", "/api/v1/catalog/resources", "", false},
		{"explicit database identity", "GET /api/v1/community/posts/{id}", "/api/v1/community/posts/c00000001", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc(test.pattern, func(w http.ResponseWriter, r *http.Request) {
				markActivityUser(r, 42)
				if test.internal {
					annotateActivityID(r, activity.ActionEdit, activity.ObjectComment, "comment", 115, 0)
				}
				w.WriteHeader(http.StatusOK)
			})
			annotation := &requestActivity{}
			ctx := context.WithValue(t.Context(), requestActivityContextKey, annotation)
			outer := httptest.NewRequestWithContext(ctx, http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			// The anti-abuse middleware can derive a request before ServeMux fills path values.
			mux.ServeHTTP(response, outer.Clone(ctx))
			event, valid := requestActivityEvent(outer, annotation)
			if response.Code != http.StatusOK || !valid || event.ObjectPublicID != test.want ||
				(test.internal && (event.ObjectEntityType != "comment" || event.ObjectInternalID != 115)) {
				t.Fatalf("activity for %s = %+v, valid=%t, status=%d; want public ID %q", test.path, event, valid, response.Code, test.want)
			}
		})
	}
}
