package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestModpackSiteIDStopsOnDatabaseFailure(t *testing.T) {
	want := errors.New("site ID lookup unavailable")
	query := &revisionPublicIDQueryStub{err: want}
	id, err := availableModpackSiteID(t.Context(), query, "Example pack")
	if !query.called || id != "" || !errors.Is(err, want) {
		t.Fatalf("failed lookup returned %q, %v (queried=%t)", id, err, query.called)
	}
}

// Verify the ID in each creation receipt can also pass its next edit's syntax
// checks, including when a duplicate name reaches the shared 100-byte limit.
func TestProjectCreationReturnsValidBoundedSiteIDsOnNameCollisionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	s := &Server{db: pool, cfg: config.Load(), cache: querycache.New(config.RedisConfig{})}
	actor := newCurrentWriteTestActor(t, ctx, pool, "site-id-owner", "project.create", "modpack.create", "project.create.plugin")
	name := strings.Repeat("a", 100)
	mod := createModRequest{PrimaryName: name, Environment: "bothRequired", PrimaryCategory: "technology", OfficialStatus: "active",
		SourceStatus: "open", License: "MIT", SubmissionMethod: "manual"}
	pack := perf020ModpackSnapshot("", 0)
	pack.PrimaryName = name
	plugin := projectCreationTestSnapshot("plugin", "", "manual")
	plugin.Localizations[0].Name = name
	for _, tc := range []struct {
		kind    string
		input   any
		handler http.HandlerFunc
	}{
		{"mod", mod, s.createMod},
		{"modpack", pack, s.createModpack},
		{"plugin", plugin, func(w http.ResponseWriter, r *http.Request) { s.createSimpleProject(w, r, "plugin") }},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			for _, want := range []string{name, strings.Repeat("a", 98) + "_2"} {
				r := newProjectHandlerRequest(t, ctx, actor, http.MethodPost, "/projects", tc.input)
				w := httptest.NewRecorder()
				tc.handler(w, r)
				if w.Code != http.StatusCreated {
					t.Fatalf("project creation returned HTTP %d: %s", w.Code, w.Body.String())
				}
				got := decodeTEST022Data[struct {
					SiteID string `json:"siteId"`
				}](t, w.Body.Bytes()).SiteID
				if got != want || !validModSiteID(got) {
					t.Fatalf("created site ID %q does not match editable ID %q", got, want)
				}
			}
		})
	}
}
