package httpapi

import (
	"net/http"
	"testing"
)

// Both actors read the same published revision through the editor API. A
// subsequent automatic publication must not overwrite the newer projection.
func TestAutomaticProjectEditsRejectStalePublishedBaseIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create", "project.create.plugin")
	prepareEditors := func(t *testing.T, projectID string) {
		t.Helper()
		for _, token := range []string{f.otherEditor, f.denied} {
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "project.edit."+projectID, "content.no-review")
		}
	}
	type published struct {
		RevisionID *string `json:"publishedRevisionId"`
	}
	type revision struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	writeConcurrentEdits := func(t *testing.T, path string, first, stale any) string {
		t.Helper()
		readBase := func(token string) string {
			t.Helper()
			data := decodeTEST022Data[published](t, f.require(t, token, http.MethodGet, path+"/editor", nil, http.StatusOK))
			if data.RevisionID == nil || *data.RevisionID == "" {
				t.Fatal("published project editor response must have a base revision")
			}
			return *data.RevisionID
		}
		firstBase, staleBase := readBase(f.otherEditor), readBase(f.denied)
		if firstBase != staleBase {
			t.Fatal("the two editors did not open the same revision")
		}
		newRevision := decodeTEST022Data[revision](t, f.require(t, f.otherEditor, http.MethodPut, path,
			map[string]any{"snapshot": first, "baseRevisionId": firstBase}, http.StatusCreated))
		if newRevision.ID == firstBase || newRevision.Status != "approved" {
			t.Fatalf("first editor did not publish: %#v", newRevision)
		}
		f.require(t, f.denied, http.MethodPut, path,
			map[string]any{"snapshot": stale, "baseRevisionId": staleBase}, http.StatusConflict)
		f.require(t, f.denied, http.MethodPut, path, map[string]any{"snapshot": stale}, http.StatusConflict)
		return newRevision.ID
	}

	t.Run("modpack", func(t *testing.T) {
		input := perf020ModpackSnapshot("review-base-modpack", 0)
		project := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", input, http.StatusCreated))
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+project.SubmissionRevisionID,
			map[string]string{"status": "approved"}, http.StatusOK)
		prepareEditors(t, project.PublicID)

		first, stale := input, input
		first.Summary, stale.PrimaryName = "Newly published summary", "Edit based on prior summary"
		path := "/api/v1/modpacks/" + project.SiteID
		publishedRevision := writeConcurrentEdits(t, path, first, stale)
		current := decodeTEST022Data[modpackResponse](t, f.require(t, "", http.MethodGet, path, nil, http.StatusOK))
		if current.PublishedRevisionID == nil || *current.PublishedRevisionID != publishedRevision ||
			current.Summary != first.Summary || current.PrimaryName != input.PrimaryName {
			t.Fatalf("stale modpack edit changed the published revision or fields: %#v", current)
		}
		assertTwoProjectRevisions(t, f, modpackAggregate, project.PublicID)
	})

	t.Run("plugin", func(t *testing.T) {
		input := test014Snapshot(f, "plugin", "review-base-plugin", "Review base plugin", "1.21.1")
		project := test014Create(t, f, input)
		test014Approve(t, f, project.SubmissionRevisionID, f.editor)
		prepareEditors(t, project.PublicID)

		first, stale := input, input
		first.Localizations = append([]simpleProjectLocalization{}, input.Localizations...)
		stale.Localizations = append([]simpleProjectLocalization{}, input.Localizations...)
		first.Localizations[0].Summary, stale.Localizations[0].Name = "Newly published summary", "Edit based on prior summary"
		path := "/api/v1/content-projects/plugin/" + project.SiteID
		publishedRevision := writeConcurrentEdits(t, path, first, stale)
		current := decodeTEST022Data[simpleProjectResponse](t, f.require(t, "", http.MethodGet, path, nil, http.StatusOK))
		if current.PublishedRevisionID == nil || *current.PublishedRevisionID != publishedRevision ||
			len(current.Localizations) != 1 || current.Localizations[0].Summary != first.Localizations[0].Summary ||
			current.Localizations[0].Name != input.Localizations[0].Name {
			t.Fatalf("stale plugin edit changed the published revision or fields: %#v", current)
		}
		assertTwoProjectRevisions(t, f, simpleProjectAggregate, project.PublicID)
	})
}

func assertTwoProjectRevisions(t *testing.T, f test013Fixture, aggregate, projectID string) {
	t.Helper()
	var revisions int
	if err := f.db.QueryRow(f.ctx, `select count(*)::int from content_revisions where aggregate_type=$1 and aggregate_key=$2`,
		aggregate, projectID).Scan(&revisions); err != nil || revisions != 2 {
		t.Fatalf("project revision count=%d, want 2: %v", revisions, err)
	}
}
