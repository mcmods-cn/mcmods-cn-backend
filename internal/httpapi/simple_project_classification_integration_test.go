package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestSimpleProjectClassificationSurvivesReviewAndUnrelatedEditsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create.*")
	for _, tc := range []struct {
		kind, known string
		field       func(*simpleProjectSnapshot) *string
	}{
		{"resource_pack", "64x", func(s *simpleProjectSnapshot) *string { return &s.Resolution }},
		{"shader_pack", "low", func(s *simpleProjectSnapshot) *string { return &s.Performance }},
		{"map", "large", func(s *simpleProjectSnapshot) *string { return &s.MapSize }},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			site := "classification-" + strings.ReplaceAll(tc.kind, "_", "-")
			path := "/api/v1/content-projects/" + tc.kind
			snapshot := test014Snapshot(f, tc.kind, site, "Classification example", "1.20.1")
			*tc.field(&snapshot) = "unrecognized"
			f.require(t, f.editor, http.MethodPost, path, snapshot, http.StatusBadRequest)
			*tc.field(&snapshot) = ""
			created := test014Create(t, f, snapshot)
			test014Approve(t, f, created.SubmissionRevisionID, f.editor)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+created.PublicID)
			path += "/" + site
			read := func() simpleProjectResponse {
				return decodeTEST022Data[simpleProjectResponse](t, f.require(t, "", http.MethodGet, path, nil, http.StatusOK))
			}
			item := read()
			if got := *tc.field(&item.simpleProjectSnapshot); got != "" {
				t.Fatalf("unclassified submission published %q", got)
			}
			for edit := 0; edit < 2; edit++ {
				next := item.simpleProjectSnapshot
				if edit == 0 {
					*tc.field(&next) = tc.known
				} else {
					next.Localizations[0].Summary = "Only the summary changed"
				}
				raw := f.require(t, f.editor, http.MethodPut, path, map[string]any{
					"snapshot": next, "baseRevisionId": item.PublishedRevisionID, "changeReason": "Update project facts",
				}, http.StatusCreated)
				revision := decodeTEST022Data[struct{ ID string }](t, raw)
				test014Approve(t, f, revision.ID, f.editor)
				item = read()
				if got := *tc.field(&item.simpleProjectSnapshot); got != tc.known {
					t.Fatalf("edit %d changed the visible classification to %q", edit, got)
				}
			}
		})
	}
}
