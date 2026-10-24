package httpapi

import (
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func TestAuditFixCMP003ModRevisionSubmissionActivitySharesTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
	snapshot := createModRequest{SiteID: "atomic-submitted-mod", PrimaryName: "Atomic submitted mod", DefaultLocale: "en-US", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Atomic submitted mod", Summary: "owned fixture", ContentMarkdown: "owned body"}}, Environment: "bothRequired", PrimaryCategory: "utility", ModIDs: []modIdentifierPayload{{Identifier: "atomic_submitted_mod", Primary: true}}, Compatibilities: []modLoaderCompatibilityPayload{{Loader: "neoforge", Versions: []string{"1.21.1"}}}, OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual"}
	created := decodeTEST022Data[modResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/mods", snapshot, 201))
	path := "/api/v1/mods/" + created.SiteID + "/revisions"
	f.require(t, f.reviewer, http.MethodPatch, path+"/"+created.SubmissionRevisionID, map[string]string{"status": "approved"}, 200)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+created.PublicID)
	for _, mode := range []string{"required", "no-review"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "no-review" {
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.no-review."+created.PublicID)
			}
			var base string
			if err := f.db.QueryRow(f.ctx, `select revision.public_id from mods m join content_revisions revision on revision.id=m.published_revision_id where m.project_code=$1`, created.PublicID).Scan(&base); err != nil {
				t.Fatal(err)
			}
			snapshot.Summary = "Atomic mod edit " + mode
			request := submitModRevisionRequest{Snapshot: snapshot, BaseRevisionID: &base, ChangeReason: "owned atomic fixture"}
			auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
			t.Cleanup(func() {
				_, _ = f.db.Exec(f.ctx, `alter table activity_event_outbox drop constraint if exists audit_reject_activity_boundary`)
			})
			before := auditFixProjectActivityFacts(t, f)
			raw := f.require(t, f.editor, http.MethodPost, path, request, 500)
			auditFixRequireSpecificActivityError(t, raw, "failed to persist project activity")
			auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectActivityFacts(t, f))
			auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
			result := decodeTEST022Data[modRevisionResponse](t, f.require(t, f.editor, http.MethodPost, path, request, 201))
			if mode == "required" {
				if result.Status != "pending" {
					t.Fatalf("required review status=%s", result.Status)
				}
				f.require(t, f.reviewer, http.MethodPatch, path+"/"+result.ID, map[string]string{"status": "approved"}, 200)
			} else if result.Status != "approved" {
				t.Fatalf("no-review status=%s", result.Status)
			}
			// The revision writer annotates this as a Mod edit, preserving its
			// existing activity contract despite the HTTP POST /revisions path.
		})
	}
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionEdit, activity.ObjectMod).Scan(&count); err != nil || count != 2 {
		t.Fatalf("revision intents=%d want2 err=%v", count, err)
	}
}

func TestAuditFixCMP003BlueprintMetadataActivitySharesTransaction(t *testing.T) {
	f := newTEST036Fixture(t)
	raw, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "atomic-metadata.nbt", raw)
	if err = f.worker("atomic-metadata").processJob(f.ctx, upload.jobID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/blueprints/" + upload.publicID
	for _, stage := range []string{"initial", "edit"} {
		t.Run(stage, func(t *testing.T) {
			request := blueprintUpdateRequest{Title: "Atomic metadata " + stage, Description: "owned fixture text"}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before := f.facts(t)
			response := f.require(t, f.editor, http.MethodPut, path, request, 500)
			auditFixRequireSpecificActivityError(t, response, "failed to persist project activity")
			f.unchanged(t, before)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			f.require(t, f.editor, http.MethodPut, path, request, 200)
			f.approve(t, upload.publicID)
			f.require(t, "", http.MethodGet, path, nil, 200)
		})
	}
	var creates, edits int
	if err = f.db.QueryRow(f.ctx, `select count(*) filter(where action_id=$2),count(*) filter(where action_id=$3) from activity_event_outbox where user_id=$1 and object_type_id=$4`, f.userIDs[f.editor], activity.ActionCreate, activity.ActionEdit, activity.ObjectBlueprint).Scan(&creates, &edits); err != nil || creates != 1 || edits != 1 {
		t.Fatalf("metadata create/edit intents=%d/%d want1/1 err=%v", creates, edits, err)
	}
}
