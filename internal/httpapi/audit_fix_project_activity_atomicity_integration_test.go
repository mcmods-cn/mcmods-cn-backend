package httpapi

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func auditFixProjectActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	result := f.facts(t)
	rows, err := f.db.Query(f.ctx, `select tablename from pg_tables where schemaname=current_schema() and
 (tablename like 'simple_project_%' or tablename like 'modpack_%' or tablename in ('simple_projects','modpacks','user_permissions','user_role_bindings')) order by tablename`)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		var raw string
		if err = f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{name}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		result[name] = raw
	}
	return result
}
func auditFixRequireProjectFactsUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		for key, value := range before {
			if value != after[key] {
				t.Errorf("failed activity enqueue changed %s", key)
			}
		}
		t.FailNow()
	}
}

func TestAuditFixCMP003FourCommunityKindsCreateEditActivityIsAtomic(t *testing.T) {
	f := newTEST042Fixture(t)
	for _, kind := range []string{"tutorial", "issue", "news", "discussion"} {
		t.Run(kind, func(t *testing.T) {
			snapshot := f.snapshot(kind, "Atomic "+kind)
			request := communityPostMutationRequest{communityPostSnapshot: snapshot}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before := f.facts(t)
			raw := f.require(t, f.editor, http.MethodPost, "/api/v1/community/posts", request, 500)
			auditFixRequireSpecificActivityError(t, raw, "failed to persist project activity")
			f.unchanged(t, before)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			created := f.create(t, f.editor, snapshot)
			f.review(t, created, "approved")
			base := f.detail(t, f.editor, created.ID).PublishedRevisionID
			snapshot.Title = "Edited atomic " + kind
			request = communityPostMutationRequest{communityPostSnapshot: snapshot, BaseRevisionID: &base}
			path := "/api/v1/community/posts/" + created.ID
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before = f.facts(t)
			raw = f.require(t, f.editor, http.MethodPut, path, request, 500)
			auditFixRequireSpecificActivityError(t, raw, "failed to persist project activity")
			f.unchanged(t, before)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			f.require(t, f.editor, http.MethodPut, path, request, 200)
			if detail := f.detail(t, "", created.ID); detail.Title != snapshot.Title {
				t.Fatalf("recovered %s title=%q", kind, detail.Title)
			}
		})
	}
	var creates, edits int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where action_id=$2),count(*) filter(where action_id=$3) from activity_event_outbox where user_id=$1 and object_type_id=$4`, f.userIDs[f.editor], activity.ActionCreate, activity.ActionEdit, activity.ObjectCommunityPost).Scan(&creates, &edits); err != nil || creates != 4 || edits != 4 {
		t.Fatalf("community activity create/edit=%d/%d want4/4 err=%v", creates, edits, err)
	}
}

func TestAuditFixCMP003SixSimpleProjectKindsCreateEditActivityIsAtomic(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
	for _, kind := range []string{"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"} {
		t.Run(kind, func(t *testing.T) {
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create."+kind)
			snapshot := test014Snapshot(f, kind, "atomic-"+strings.ReplaceAll(kind, "_", "-"), "Atomic "+kind, "1.21.1")
			path := "/api/v1/content-projects/" + kind
			auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
			before := auditFixProjectActivityFacts(t, f)
			raw := f.require(t, f.editor, http.MethodPost, path, snapshot, 500)
			auditFixRequireSpecificActivityError(t, raw, "failed to persist project activity")
			auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectActivityFacts(t, f))
			auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
			created := test014Create(t, f, snapshot)
			test014Approve(t, f, created.SubmissionRevisionID, f.editor)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+created.PublicID)
			current := decodeTEST022Data[simpleProjectResponse](t, f.require(t, f.editor, http.MethodGet, path+"/"+created.SiteID+"/editor", nil, 200))
			snapshot.Localizations[0].Summary = "Updated atomic summary"
			update := map[string]any{"snapshot": snapshot, "baseRevisionId": current.PublishedRevisionID, "changeReason": "owned atomic activity fixture"}
			auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
			before = auditFixProjectActivityFacts(t, f)
			raw = f.require(t, f.editor, http.MethodPut, path+"/"+created.SiteID, update, 500)
			auditFixRequireSpecificActivityError(t, raw, "failed to persist project activity")
			auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectActivityFacts(t, f))
			auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
			result := decodeTEST022Data[struct {
				ID string `json:"id"`
			}](t, f.require(t, f.editor, http.MethodPut, path+"/"+created.SiteID, update, 201))
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+result.ID, map[string]string{"status": "approved"}, 200)
			public := f.require(t, "", http.MethodGet, path+"/"+created.SiteID, nil, 200)
			if !strings.Contains(string(public), "Updated atomic summary") {
				t.Fatalf("recovered %s not published", kind)
			}
		})
	}
	var creates, edits int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where action_id=$2),count(*) filter(where action_id=$3) from activity_event_outbox where user_id=$1`, f.userIDs[f.editor], activity.ActionCreate, activity.ActionEdit).Scan(&creates, &edits); err != nil || creates != 6 || edits != 6 {
		t.Fatalf("six project activity create/edit=%d/%d want6/6 err=%v", creates, edits, err)
	}
}

func TestAuditFixCMP003ModAndModpackCreationAndPackEditActivityIsAtomic(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create", "modpack.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
	mod := createModRequest{SiteID: "atomic-created-mod", PrimaryName: "Atomic created mod", DefaultLocale: "en-US", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Atomic created mod", Summary: "owned fixture", ContentMarkdown: "owned body"}}, Environment: "bothRequired", PrimaryCategory: "utility", ModIDs: []modIdentifierPayload{{Identifier: "atomic_created_mod", Primary: true}}, Compatibilities: []modLoaderCompatibilityPayload{{Loader: "neoforge", Versions: []string{"1.21.1"}}}, OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual"}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	before := auditFixProjectActivityFacts(t, f)
	raw := f.require(t, f.editor, http.MethodPost, "/api/v1/mods", mod, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist project activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectActivityFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	createdMod := decodeTEST022Data[modResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/mods", mod, 201))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/mods/"+createdMod.SiteID+"/revisions/"+createdMod.SubmissionRevisionID, map[string]string{"status": "approved"}, 200)
	pack := perf020ModpackSnapshot("atomic-created-pack", 0)
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	before = auditFixProjectActivityFacts(t, f)
	raw = f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", pack, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist project activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectActivityFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	createdPack := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", pack, 201))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+createdPack.SubmissionRevisionID, map[string]string{"status": "approved"}, 200)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+createdPack.PublicID)
	editor := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodGet, "/api/v1/modpacks/"+createdPack.SiteID+"/editor", nil, 200))
	pack.Summary = "Atomic edited pack"
	update := map[string]any{"snapshot": pack, "baseRevisionId": editor.PublishedRevisionID, "changeReason": "owned fixture"}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	before = auditFixProjectActivityFacts(t, f)
	raw = f.require(t, f.editor, http.MethodPut, "/api/v1/modpacks/"+createdPack.SiteID, update, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist project activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectActivityFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	result := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, f.editor, http.MethodPut, "/api/v1/modpacks/"+createdPack.SiteID, update, 201))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+result.ID, map[string]string{"status": "approved"}, 200)
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1`, f.userIDs[f.editor]).Scan(&count); err != nil || count != 3 {
		t.Fatalf("mod/pack activity intents=%d want3 err=%v", count, err)
	}
}
