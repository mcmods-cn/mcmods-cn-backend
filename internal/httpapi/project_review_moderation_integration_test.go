package httpapi

import (
	"net/http"
	"testing"
)

func TestPendingProjectDecisionsPreserveModerationVisibilityIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create", "modpack.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "report.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "report.review", "report.action.delete")

	t.Run("mod approval", func(t *testing.T) {
		input := createModRequest{
			SiteID: "moderated-review-mod", PrimaryName: "Moderated review mod", DefaultLocale: "en-US",
			Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Moderated review mod", Summary: "Moderation fixture"}},
			Environment:   "bothRequired", PrimaryCategory: "utility", OfficialStatus: "active", SourceStatus: "open",
			License: "MIT", SubmissionMethod: "manual", ModIDs: []modIdentifierPayload{{Identifier: "moderated_review_mod", Primary: true}},
			Compatibilities: []modLoaderCompatibilityPayload{{Loader: "neoforge", Versions: []string{"1.21.1"}}},
		}
		project := decodeTEST022Data[modResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/mods", input, http.StatusCreated))
		projectPath := "/api/v1/mods/" + project.SiteID
		f.require(t, f.reviewer, http.MethodPatch, projectPath+"/revisions/"+project.SubmissionRevisionID, map[string]string{"status": "approved"}, http.StatusOK)
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+project.UniqueID)

		input.Localizations[0].Summary = "Pending metadata correction"
		candidate := decodeTEST022Data[modRevisionResponse](t, f.require(t, f.editor, http.MethodPost, projectPath+"/revisions",
			submitModRevisionRequest{Snapshot: input, BaseRevisionID: &project.SubmissionRevisionID, ChangeReason: "Edit description"}, http.StatusCreated))
		if candidate.Status != "pending" {
			t.Fatalf("metadata correction status=%s, want pending", candidate.Status)
		}
		assertPendingProjectDecisionPreservesModeration(t, f, "mod", project.UniqueID, projectPath,
			projectPath+"/revisions/"+candidate.ID, candidate.ID, project.SubmissionRevisionID, "approved",
			`select review_status,published_revision_id from mods where project_code=$1`)
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+project.UniqueID, "project.no-review."+project.UniqueID)
		f.require(t, f.otherEditor, http.MethodPost, projectPath+"/revisions",
			submitModRevisionRequest{Snapshot: input, BaseRevisionID: &project.SubmissionRevisionID, ChangeReason: "Automatic edit"}, http.StatusConflict)
		f.require(t, "", http.MethodGet, projectPath, nil, http.StatusNotFound)
		var revisions int
		if err := f.db.QueryRow(f.ctx, `select count(*)::int from change_requests where aggregate_type='mod' and aggregate_key=$1`, project.UniqueID).
			Scan(&revisions); err != nil || revisions != 2 {
			t.Fatalf("automatic approval conflict left %d changes, want 2: %v", revisions, err)
		}
	})

	t.Run("modpack rejection", func(t *testing.T) {
		input := perf020ModpackSnapshot("moderated-review-pack", 0)
		project := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", input, http.StatusCreated))
		projectPath := "/api/v1/modpacks/" + project.SiteID
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+project.SubmissionRevisionID, map[string]string{"status": "approved"}, http.StatusOK)
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+project.PublicID)

		candidate := decodeTEST022Data[struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}](t, f.require(t, f.editor, http.MethodPut, projectPath, map[string]any{
			"snapshot": input, "baseRevisionId": project.SubmissionRevisionID, "changeReason": "Review pack metadata",
		}, http.StatusCreated))
		if candidate.Status != "pending" {
			t.Fatalf("pack metadata correction status=%s, want pending", candidate.Status)
		}
		assertPendingProjectDecisionPreservesModeration(t, f, "modpack", project.PublicID, projectPath,
			"/api/v1/content-revisions/"+candidate.ID, candidate.ID, project.SubmissionRevisionID, "rejected",
			`select review_status,published_revision_id from modpacks where public_id=$1`)
	})
}

func assertPendingProjectDecisionPreservesModeration(t *testing.T, f test013Fixture, projectType, projectID, projectPath,
	reviewPath, proposedRevision, publishedRevision, decision, projectFactsQuery string,
) {
	t.Helper()
	f.require(t, "", http.MethodGet, projectPath, nil, http.StatusOK)
	report := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, f.denied, http.MethodPost, "/api/v1/reports", map[string]string{
		"targetType": projectType, "targetId": projectID, "reasonCode": "misleading_content", "detail": "Synthetic moderation fixture",
	}, http.StatusCreated))
	reportPath := "/api/v1/admin/reports/" + report.ID
	f.require(t, f.otherEditor, http.MethodPost, reportPath+"/claim", map[string]string{}, http.StatusOK)
	f.require(t, f.otherEditor, http.MethodPost, reportPath+"/resolve", map[string]any{
		"conclusion": "valid", "note": "Hide synthetic public project", "deleteTarget": true, "idempotencyKey": "hide-review-fixture",
	}, http.StatusOK)
	f.require(t, "", http.MethodGet, projectPath, nil, http.StatusNotFound)

	f.require(t, f.reviewer, http.MethodPatch, reviewPath, map[string]string{"status": decision}, http.StatusConflict)
	f.require(t, "", http.MethodGet, projectPath, nil, http.StatusNotFound)
	var targetStatus, revision string
	if err := f.db.QueryRow(f.ctx, `select project.review_status,revision.public_id from (`+projectFactsQuery+`) project
		join content_revisions revision on revision.id=project.published_revision_id`, projectID).
		Scan(&targetStatus, &revision); err != nil || targetStatus != "rejected" || revision != publishedRevision {
		t.Fatalf("governance visibility status=%q revision=%q want rejected/%q err=%v", targetStatus, revision, publishedRevision, err)
	}
	var requestStatus string
	var conflicts, invalidTerminalEvents int
	if err := f.db.QueryRow(f.ctx, `select request.status,
		count(*) filter (where event.event_type='conflicted')::int,
		count(*) filter (where event.event_type in ('approved','rejected','published'))::int
		from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id
		join review_events event on event.change_request_id=request.id where revision.public_id=$1
		group by request.id`, proposedRevision).Scan(&requestStatus, &conflicts, &invalidTerminalEvents); err != nil || requestStatus != "conflicted" || conflicts != 1 || invalidTerminalEvents != 0 {
		t.Fatalf("governance decision status=%q conflicts=%d publication events=%d err=%v",
			requestStatus, conflicts, invalidTerminalEvents, err)
	}
}

func TestProjectModerationDistinguishesFirstRejectionAndOfficialArchiveIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create")

	create := func(t *testing.T, input createModpackRequest) modpackResponse {
		t.Helper()
		return decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", input, http.StatusCreated))
	}
	decide := func(t *testing.T, revisionID, status string) {
		t.Helper()
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+revisionID, map[string]string{"status": status}, http.StatusOK)
	}

	t.Run("correct initial rejection", func(t *testing.T) {
		input := perf020ModpackSnapshot("ordinary-review-rejected-pack", 0)
		project := create(t, input)
		decide(t, project.SubmissionRevisionID, "rejected")
		projectPath := "/api/v1/modpacks/" + project.SiteID
		f.require(t, "", http.MethodGet, projectPath, nil, http.StatusNotFound)

		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+project.PublicID)
		input.Summary = "Correction submitted after a first-review rejection"
		candidate := decodeTEST022Data[struct {
			ID string `json:"id"`
		}](t, f.require(t, f.editor, http.MethodPut, projectPath, map[string]any{"snapshot": input}, http.StatusCreated))
		decide(t, candidate.ID, "approved")
		f.require(t, "", http.MethodGet, projectPath, nil, http.StatusOK)
	})

	t.Run("publish as officially archived", func(t *testing.T) {
		input := perf020ModpackSnapshot("ordinary-review-archived-pack", 0)
		input.OfficialStatus = "archived"
		project := create(t, input)
		decide(t, project.SubmissionRevisionID, "approved")
		public := decodeTEST022Data[modpackResponse](t, f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+project.SiteID, nil, http.StatusOK))
		if public.OfficialStatus != "archived" || public.ReviewStatus != "approved" {
			t.Fatalf("official and review statuses = %q,%q", public.OfficialStatus, public.ReviewStatus)
		}
	})
}
