package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The release synchronization fixture calls the real transaction worker; all
// user edits, reads and independent reviews traverse the full HTTP server and
// actual session/RBAC store. It does not claim live provider fetch/delivery.
type test017Fixture struct{ test013Fixture }

func newTEST017Fixture(t *testing.T) test017Fixture {
	t.Helper()
	f := test017Fixture{newTEST013Fixture(t)}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "project.review."+f.modCode)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.review."+f.modCode)
	review := defaultReviewConfig()
	review.ChangelogCreate, review.ChangelogEdit = true, true
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$2::jsonb where key=$1`, reviewConfigSettingKey, raw); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f test017Fixture) facts(t *testing.T) map[string]string {
	t.Helper()
	result := f.test013Fixture.facts(t)
	for _, table := range []string{"project_changelogs", "project_changelog_localizations", "project_changelog_categories", "project_changelog_category_localizations", "external_release_bindings", "content_popularity_events_daily", "content_popularity_lifetime_facts", "content_stats_refresh_queue", "search_index_queue", "review_completion_subscriptions"} {
		var value string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact_row) order by to_jsonb(fact_row)::text),'[]'::jsonb)::text from `+table+` fact_row`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		result[table] = value
	}
	return result
}

func (f test017Fixture) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	if !reflect.DeepEqual(before, after) {
		for table, value := range before {
			if after[table] != value {
				t.Errorf("failed/denied changelog operation mutated %s", table)
			}
		}
		t.FailNow()
	}
}

func test017Snapshot(body string) projectChangelogSnapshot {
	return projectChangelogSnapshot{EventAt: time.Now().UTC(), MinecraftVersions: []string{"1.21.1"}, ProjectVersion: "manual-v1", DefaultLocale: "en-US", Localizations: []projectChangelogLocalization{{Locale: "en-US", BodyMarkdown: body}}, Reason: "TEST017 actual user proposal"}
}

func (f test017Fixture) managed(t *testing.T, other bool) (string, *ProjectAutomationWorker, projectAutomationJob, projectAutomationRelease) {
	t.Helper()
	internalID, publicID, actor := f.modID, f.modCode, f.editor
	if other {
		internalID, publicID, actor = f.otherModID, f.otherModCode, f.otherEditor
	}
	var routeID int64
	if err := f.db.QueryRow(f.ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, internalID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	worker := &ProjectAutomationWorker{server: f.origin.Config.Handler.(*Server)}
	job := projectAutomationJob{RouteID: routeID, InternalID: internalID, ProjectType: "mod", ProjectPublicID: publicID, SourceType: "modrinth", ActorID: f.userIDs[actor]}
	release := projectAutomationRelease{ID: "test017-managed-" + publicID, Version: "upstream-v1", PublishedAt: time.Now().UTC(), GameVersions: []string{"1.21.1"}, Body: "Original upstream " + publicID, URL: "https://example.invalid/release/" + publicID}
	result, err := worker.syncChangelogs(f.ctx, job, []projectAutomationRelease{release})
	if err != nil || result["created"] != 1 {
		t.Fatalf("real managed release fixture result=%v err=%v", result, err)
	}
	var id string
	if err := f.db.QueryRow(f.ctx, `select changelog_public_id from external_release_bindings where source_type='modrinth' and external_release_id=$1 and project_route_id=$2`, release.ID, routeID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, worker, job, release
}

type test017Mutation struct{ ID, Status, RequestID, RevisionID string }

func (f test017Fixture) submit(t *testing.T, token, method, path string, snapshot projectChangelogSnapshot, want int) test017Mutation {
	t.Helper()
	raw := f.require(t, token, method, path, snapshot, want)
	var response struct {
		Data struct {
			ID        string `json:"id"`
			Status    string `json:"reviewStatus"`
			RequestID string `json:"changeRequestId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	result := test017Mutation{ID: response.Data.ID, Status: response.Data.Status, RequestID: response.Data.RequestID}
	if result.ID == "" || result.RequestID == "" || result.Status != "pending" {
		t.Fatalf("proposal bypassed normal review: %s", raw)
	}
	if err := f.db.QueryRow(f.ctx, `select revision.public_id from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id where request.public_id=$1 and request.aggregate_type=$2 and request.aggregate_key=$3 and request.submitted_by=$4 and request.status='pending' and revision.source='user'`, result.RequestID, projectChangelogAggregate, result.ID, f.userIDs[token]).Scan(&result.RevisionID); err != nil {
		t.Fatal(err)
	}
	return result
}

func (f test017Fixture) review(t *testing.T, token string, proposal test017Mutation, status string, want int) {
	t.Helper()
	f.require(t, token, http.MethodPatch, "/api/v1/content-revisions/"+proposal.RevisionID, map[string]any{"status": status, "note": "TEST017 independent " + status}, want)
}

func (f test017Fixture) detail(t *testing.T, token, id string, want int) projectChangelogResponse {
	t.Helper()
	raw := f.require(t, token, http.MethodGet, "/api/v1/changelogs/"+id, nil, want)
	if want != http.StatusOK {
		return projectChangelogResponse{}
	}
	var response struct {
		Data struct {
			Item projectChangelogResponse `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response.Data.Item
}

func (f test017Fixture) history(t *testing.T, token, id string) []contentHistoryItem {
	t.Helper()
	raw := f.require(t, token, http.MethodGet, "/api/v1/changelogs/"+id+"/history", nil, http.StatusOK)
	var response struct {
		Data contentHistoryPage `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"snapshot"`)) || bytes.Contains(raw, []byte(`"localizations"`)) {
		t.Fatalf("history leaked full draft data: %s", raw)
	}
	return response.Data.Items
}

func (f test017Fixture) assertResolution(t *testing.T, proposal test017Mutation, token, status string) {
	t.Helper()
	var stored string
	var submitted, resolved, published int
	if err := f.db.QueryRow(f.ctx, `select request.status,
		(select count(*) from review_events where change_request_id=request.id and event_type='submitted'),
		(select count(*) from review_events where change_request_id=request.id and event_type=$2 and actor_id=$3),
		(select count(*) from review_events where change_request_id=request.id and event_type='published' and actor_id=$3)
		from change_requests request where request.public_id=$1`, proposal.RequestID, status, f.userIDs[token]).Scan(&stored, &submitted, &resolved, &published); err != nil {
		t.Fatal(err)
	}
	wantPublished := 0
	if status == "approved" {
		wantPublished = 1
	}
	if stored != status || submitted != 1 || resolved != 1 || published != wantPublished {
		t.Fatalf("review facts status=%s submitted=%d resolved=%d published=%d", stored, submitted, resolved, published)
	}
}

func TestTEST017ManagedChangelogManualReviewPrivacyAndOverrideHTTPIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	id, worker, job, release := f.managed(t, false)
	otherID, _, _, _ := f.managed(t, true)
	proposal := f.submit(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+id, test017Snapshot("Private rejected candidate"), http.StatusOK)
	other := f.submit(t, f.otherEditor, http.MethodPut, "/api/v1/changelogs/"+otherID, test017Snapshot("Other project's pending edit"), http.StatusOK)
	rawQueue := f.require(t, f.denied, http.MethodGet, "/api/v1/reviews/content?category=project_changelog", nil, http.StatusOK)
	var queueResponse struct {
		Data struct {
			Items []modContentReviewItem `json:"items"`
			Total int                    `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rawQueue, &queueResponse); err != nil {
		t.Fatal(err)
	}
	if queueResponse.Data.Total != 1 || len(queueResponse.Data.Items) != 1 || queueResponse.Data.Items[0].ID != proposal.RevisionID || queueResponse.Data.Items[0].ProjectID != f.modCode || queueResponse.Data.Items[0].ReviewerScope != "project" {
		t.Fatalf("normal scoped queue leaked another target or lost proposal: %s", rawQueue)
	}
	selfQueue := f.require(t, f.editor, http.MethodGet, "/api/v1/reviews/content?category=project_changelog", nil, http.StatusOK)
	if err := json.Unmarshal(selfQueue, &queueResponse); err != nil {
		t.Fatal(err)
	}
	if queueResponse.Data.Total != 0 || len(queueResponse.Data.Items) != 0 {
		t.Fatalf("scoped reviewer could queue their own proposal: %s", selfQueue)
	}
	assertBUG034ManualOverride(t, f.ctx, f.db, id, false)
	if item := f.detail(t, "", id, http.StatusOK); item.BodyMarkdown != release.Body || !item.PendingChange || item.CanEdit {
		t.Fatalf("public detail leaked draft/edit permission: %+v", item)
	}
	for _, token := range []string{"", f.otherEditor} {
		for _, item := range f.history(t, token, id) {
			if item.Status != "approved" {
				t.Fatalf("unprivileged history exposed %s", item.Status)
			}
		}
	}
	foundPending := false
	for _, item := range f.history(t, f.denied, id) {
		if item.ID == proposal.RevisionID && item.Status == "pending" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatal("normal exact-project reviewer could not read another user's pending history")
	}
	for _, token := range []string{f.editor, f.otherEditor} {
		before := f.facts(t)
		f.review(t, token, proposal, "approved", http.StatusForbidden)
		f.unchanged(t, before)
	}
	before := f.facts(t)
	f.require(t, f.otherEditor, http.MethodPut, "/api/v1/changelogs/"+id, test017Snapshot("foreign project edit"), http.StatusForbidden)
	f.unchanged(t, before)
	f.review(t, f.denied, proposal, "rejected", http.StatusOK)
	f.assertResolution(t, proposal, f.denied, "rejected")
	assertBUG034ManualOverride(t, f.ctx, f.db, id, false)
	assertBUG034ChangelogBody(t, f.ctx, f.db, id, release.Body)
	release.Version, release.Body = "upstream-v2", "Upstream after real HTTP rejection"
	result, err := worker.syncChangelogs(f.ctx, job, []projectAutomationRelease{release})
	if err != nil || result["updated"] != 1 || result["manualOverrideConflicts"] != 0 {
		t.Fatalf("rejection wrongly disabled sync: %v %v", result, err)
	}
	assertBUG034ChangelogBody(t, f.ctx, f.db, id, release.Body)
	proposal = f.submit(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+id, test017Snapshot("Approved manual body"), http.StatusOK)
	assertBUG034ManualOverride(t, f.ctx, f.db, id, false)
	f.review(t, f.denied, proposal, "approved", http.StatusOK)
	f.assertResolution(t, proposal, f.denied, "approved")
	var validOrigin bool
	if err := f.db.QueryRow(f.ctx, `select binding.manual_override and binding.manual_override_source='user' and binding.manual_override_revision_id=revision.id and request.status='approved' from external_release_bindings binding join content_revisions revision on revision.public_id=$2 join change_requests request on request.proposed_revision_id=revision.id where binding.changelog_public_id=$1`, id, proposal.RevisionID).Scan(&validOrigin); err != nil || !validOrigin {
		t.Fatalf("manual override lacks actual approved user origin: %t %v", validOrigin, err)
	}
	release.Version, release.Body = "upstream-v3", "Must not overwrite approved manual"
	result, err = worker.syncChangelogs(f.ctx, job, []projectAutomationRelease{release})
	if err != nil || result["updated"] != 0 || result["manualOverrideConflicts"] != 1 {
		t.Fatalf("approved override did not protect body: %v %v", result, err)
	}
	assertBUG034ChangelogBody(t, f.ctx, f.db, id, "Approved manual body")
	before = f.facts(t)
	f.review(t, f.denied, other, "approved", http.StatusForbidden)
	f.unchanged(t, before)
}

func TestTEST017NewChangelogPendingRejectionAndApprovalHTTPIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	path := "/api/v1/changelogs?targetType=mod&targetId=" + f.modCode
	before := f.facts(t)
	f.require(t, "", http.MethodPost, path, test017Snapshot("anonymous"), http.StatusUnauthorized)
	f.require(t, f.otherEditor, http.MethodPost, path, test017Snapshot("foreign"), http.StatusForbidden)
	unknown := test017Snapshot("unknown Minecraft version")
	unknown.MinecraftVersions = []string{"not-a-known-minecraft-version"}
	f.require(t, f.editor, http.MethodPost, path, unknown, http.StatusBadRequest)
	f.unchanged(t, before)
	proposal := f.submit(t, f.editor, http.MethodPost, path, test017Snapshot("Never published rejected body"), http.StatusCreated)
	f.detail(t, "", proposal.ID, http.StatusNotFound)
	f.detail(t, f.otherEditor, proposal.ID, http.StatusNotFound)
	f.detail(t, f.denied, proposal.ID, http.StatusOK)
	f.review(t, f.denied, proposal, "rejected", http.StatusOK)
	f.assertResolution(t, proposal, f.denied, "rejected")
	f.detail(t, "", proposal.ID, http.StatusNotFound)
	var published int
	if err := f.db.QueryRow(f.ctx, `select count(*) from project_changelog_localizations`).Scan(&published); err != nil || published != 0 {
		t.Fatalf("rejection applied a body: %d %v", published, err)
	}
	proposal = f.submit(t, f.editor, http.MethodPost, path, test017Snapshot("New approved public body"), http.StatusCreated)
	f.review(t, f.denied, proposal, "approved", http.StatusOK)
	f.assertResolution(t, proposal, f.denied, "approved")
	if item := f.detail(t, "", proposal.ID, http.StatusOK); item.BodyMarkdown != "New approved public body" {
		t.Fatalf("approved body missing: %+v", item)
	}
}

func TestTEST017CategoryMustBelongToTheTargetBeforeProposalHTTPIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	id, _, _, _ := f.managed(t, false)
	var category string
	if err := f.db.QueryRow(f.ctx, `insert into project_changelog_categories(object_route_id,default_locale,created_by) select id,'en-US',$2 from public_routes where entity_type='mod' and internal_id=$1 returning public_id`, f.otherModID, f.userIDs[f.otherEditor]).Scan(&category); err != nil {
		t.Fatal(err)
	}
	for _, categoryID := range []string{category, randomCatalogPublicID()} {
		for _, mutation := range []struct {
			method, path string
			want         int
		}{
			{http.MethodPost, "/api/v1/changelogs?targetType=mod&targetId=" + f.modCode, http.StatusBadRequest},
			{http.MethodPut, "/api/v1/changelogs/" + id, http.StatusBadRequest},
		} {
			snapshot := test017Snapshot("cannot submit cross-target category")
			snapshot.CategoryID = categoryID
			before := f.facts(t)
			f.require(t, f.editor, mutation.method, mutation.path, snapshot, mutation.want)
			f.unchanged(t, before)
		}
	}
	snapshot := test017Snapshot("Category created through normal review")
	snapshot.NewCategory = &projectChangelogNewCategory{DefaultLocale: "en-US", Localizations: []projectChangelogCategoryLocalization{{Locale: "en-US", Name: "Release"}}}
	proposal := f.submit(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+id, snapshot, http.StatusOK)
	f.review(t, f.denied, proposal, "approved", http.StatusOK)
	item := f.detail(t, "", id, http.StatusOK)
	if item.Category == nil || item.Category.Name != "Release" {
		t.Fatalf("approved category missing: %+v", item)
	}
	snapshot = test017Snapshot("Reuse only this project's valid category")
	snapshot.CategoryID = item.Category.ID
	proposal = f.submit(t, f.editor, http.MethodPost, "/api/v1/changelogs?targetType=mod&targetId="+f.modCode, snapshot, http.StatusCreated)
	f.review(t, f.denied, proposal, "approved", http.StatusOK)
	if reused := f.detail(t, "", proposal.ID, http.StatusOK); reused.Category == nil || reused.Category.ID != item.Category.ID {
		t.Fatalf("legitimate category reuse rejected/lost: %+v", reused)
	}
}

func TestTEST017ConcurrentReviewCommitsOneResolutionAndNotificationHTTPIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	id, _, job, release := f.managed(t, false)
	proposal := f.submit(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+id, test017Snapshot("Concurrent manual body"), http.StatusOK)
	type result struct {
		token, status string
		code          int
		err           error
		body          []byte
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, input := range []result{{token: f.denied, status: "approved"}, {token: f.reviewer, status: "rejected"}} {
		go func(input result) {
			<-start
			payload, err := json.Marshal(map[string]string{"status": input.status, "note": "TEST017 concurrent resolution"})
			if err != nil {
				input.err = err
				results <- input
				return
			}
			request, err := http.NewRequestWithContext(f.ctx, http.MethodPatch, f.origin.URL+"/api/v1/content-revisions/"+proposal.RevisionID, bytes.NewReader(payload))
			if err != nil {
				input.err = err
				results <- input
				return
			}
			request.Header.Set("Authorization", "Bearer "+input.token)
			request.Header.Set("Content-Type", "application/json")
			response, err := f.origin.Client().Do(request)
			if err != nil {
				input.err = err
				results <- input
				return
			}
			defer response.Body.Close()
			input.code = response.StatusCode
			input.body, input.err = io.ReadAll(io.LimitReader(response.Body, 2048))
			results <- input
		}(input)
	}
	close(start)
	winner := result{}
	conflicts := 0
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		switch got.code {
		case http.StatusOK:
			if winner.code != 0 {
				t.Fatal("two independent reviewers both committed")
			}
			winner = got
		case http.StatusConflict:
			conflicts++
			if bytes.Contains(got.body, []byte(`"data"`)) {
				t.Fatalf("losing review returned partial success: %s", got.body)
			}
		default:
			t.Fatalf("concurrent review status=%d body=%s", got.code, got.body)
		}
	}
	if winner.code != http.StatusOK || conflicts != 1 {
		t.Fatalf("winner=%+v conflicts=%d", winner, conflicts)
	}
	f.assertResolution(t, proposal, winner.token, winner.status)
	var intents, updates int
	if err := f.db.QueryRow(f.ctx, `select
		(select count(*) from nats_outbox where payload->'data'->>'changelogId'=$1 and payload->>'templateKey' in ('review_approved','review_rejected')),
		(select count(*) from project_update_events where project_route_id=$2 and actor_user_id=$3)`, id, job.RouteID, f.userIDs[winner.token]).Scan(&intents, &updates); err != nil {
		t.Fatal(err)
	}
	wantUpdates, body := 0, release.Body
	if winner.status == "approved" {
		wantUpdates, body = 1, "Concurrent manual body"
	}
	if intents != 1 || updates != wantUpdates {
		t.Fatalf("duplicate/lost durable intent: notifications=%d updates=%d want=%d", intents, updates, wantUpdates)
	}
	assertBUG034ManualOverride(t, f.ctx, f.db, id, winner.status == "approved")
	assertBUG034ChangelogBody(t, f.ctx, f.db, id, body)
}

func TestTEST017PublicationOutboxFailureRollsBackAllChangelogFactsHTTPIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	id, _, _, release := f.managed(t, false)
	snapshot := test017Snapshot("Atomic category and body")
	snapshot.NewCategory = &projectChangelogNewCategory{DefaultLocale: "en-US", Localizations: []projectChangelogCategoryLocalization{{Locale: "en-US", Name: "Changes"}, {Locale: "zh-CN", Name: "变更"}}}
	proposal := f.submit(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+id, snapshot, http.StatusOK)
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox add constraint test017_reject_update check(event_type<>'project.updated') not valid`); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	f.review(t, f.denied, proposal, "approved", http.StatusInternalServerError)
	f.unchanged(t, before)
	assertBUG034ManualOverride(t, f.ctx, f.db, id, false)
	assertBUG034ChangelogBody(t, f.ctx, f.db, id, release.Body)
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox drop constraint test017_reject_update`); err != nil {
		t.Fatal(err)
	}
	f.review(t, f.denied, proposal, "approved", http.StatusOK)
	f.assertResolution(t, proposal, f.denied, "approved")
	if item := f.detail(t, "", id, http.StatusOK); item.Category == nil || item.Category.Names["zh-CN"] != "变更" || item.BodyMarkdown != snapshot.Localizations[0].BodyMarkdown {
		t.Fatalf("retry did not atomically publish category/body: %+v", item)
	}
}

func TestTEST017ReviewNotificationFailureIsAtomicHTTPIntegration(t *testing.T) {
	for _, status := range []string{"approved", "rejected"} {
		t.Run(status, func(t *testing.T) {
			f := newTEST017Fixture(t)
			id, _, _, _ := f.managed(t, false)
			proposal := f.submit(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+id, test017Snapshot("Review notification must be durable"), http.StatusOK)
			if _, err := f.db.Exec(f.ctx, `alter table nats_outbox add constraint test017_reject_review_notification check(coalesce(payload->>'templateKey','') not in ('review_approved','review_rejected')) not valid`); err != nil {
				t.Fatal(err)
			}
			before := f.facts(t)
			f.review(t, f.denied, proposal, status, http.StatusInternalServerError)
			f.unchanged(t, before)
			if _, err := f.db.Exec(f.ctx, `alter table nats_outbox drop constraint test017_reject_review_notification`); err != nil {
				t.Fatal(err)
			}
			f.review(t, f.denied, proposal, status, http.StatusOK)
			f.assertResolution(t, proposal, f.denied, status)
			var intents int
			if err := f.db.QueryRow(f.ctx, `select count(*) from nats_outbox where payload->'data'->>'changelogId'=$1 and payload->>'templateKey'=$2`, id, "review_"+status).Scan(&intents); err != nil || intents != 1 {
				t.Fatalf("review has no exactly-once durable intent: %d %v", intents, err)
			}
		})
	}
}

func TestTEST017PendingManualReviewBlocksUpstreamAndKeepsBaseHTTPIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	id, worker, job, release := f.managed(t, false)
	proposal := f.submit(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+id, test017Snapshot("Stale manual proposal"), http.StatusOK)
	before := f.facts(t)
	release.Body, release.Version = "New approved upstream", "upstream-new"
	if result, err := worker.syncChangelogs(f.ctx, job, []projectAutomationRelease{release}); !errors.Is(err, errReviewInProgress) {
		t.Fatalf("shared pending-review guard did not block upstream: %v %v", result, err)
	}
	f.unchanged(t, before)
	assertBUG034ManualOverride(t, f.ctx, f.db, id, false)
	f.review(t, f.denied, proposal, "approved", http.StatusOK)
	f.assertResolution(t, proposal, f.denied, "approved")
	assertBUG034ChangelogBody(t, f.ctx, f.db, id, "Stale manual proposal")
}

func TestTEST017ChangelogsResolveAllApprovedTargetTypesHTTPIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	for _, kind := range []string{"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon", "minecraft_server"} {
		t.Run(kind, func(t *testing.T) {
			code := f.modCode
			if kind == "modpack" {
				if err := f.db.QueryRow(f.ctx, `insert into modpacks(slug,primary_name,review_status,submitted_by) values('test017-pack','TEST017 Pack','approved',$1) returning public_id`, f.userIDs[f.editor]).Scan(&code); err != nil {
					t.Fatal(err)
				}
			} else if kind == "minecraft_server" {
				if err := f.db.QueryRow(f.ctx, `insert into minecraft_servers(slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,review_status,submitted_by) values('test017-server','example.invalid:25565','example.invalid:25565','example.invalid','203.0.113.17',25565,'TEST017 Server','survival','approved',$1) returning public_id`, f.userIDs[f.editor]).Scan(&code); err != nil {
					t.Fatal(err)
				}
			} else if kind != "mod" {
				if err := f.db.QueryRow(f.ctx, `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by) values($1,$2,$2,'approved',$3) returning public_id`, kind, "test017-"+kind, f.userIDs[f.editor]).Scan(&code); err != nil {
					t.Fatal(err)
				}
			}
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+code)
			queryKind := strings.ReplaceAll(kind, "_", "-")
			if kind == "minecraft_server" {
				queryKind = "server"
			}
			path := "/api/v1/changelogs?targetType=" + queryKind + "&targetId=" + code
			proposal := f.submit(t, f.editor, http.MethodPost, path, test017Snapshot("Full HTTP "+kind), http.StatusCreated)
			f.detail(t, "", proposal.ID, http.StatusNotFound)
			f.review(t, f.reviewer, proposal, "approved", http.StatusOK)
			f.assertResolution(t, proposal, f.reviewer, "approved")
			if item := f.detail(t, "", proposal.ID, http.StatusOK); item.BodyMarkdown != "Full HTTP "+kind || item.CanEdit {
				t.Fatalf("typed target publication mismatch: %+v", item)
			}
			f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
		})
	}
}

func TestTEST017DatabaseErrorsAreNotReportedAsMissingChangelogsHTTPIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	id, _, _, _ := f.managed(t, false)
	before := f.facts(t)
	if _, err := f.db.Exec(f.ctx, `alter table project_changelogs rename to test017_owned_changelog_fault`); err != nil {
		t.Fatal(err)
	}
	restored := false
	defer func() {
		if !restored {
			if _, err := f.db.Exec(f.ctx, `alter table test017_owned_changelog_fault rename to project_changelogs`); err != nil {
				t.Errorf("restore owned table: %v", err)
			}
		}
	}()
	f.require(t, f.editor, http.MethodGet, "/api/v1/changelogs/"+id, nil, http.StatusInternalServerError)
	f.require(t, f.editor, http.MethodGet, "/api/v1/changelogs/"+id+"/history", nil, http.StatusInternalServerError)
	if _, err := f.db.Exec(f.ctx, `alter table test017_owned_changelog_fault rename to project_changelogs`); err != nil {
		t.Fatal(err)
	}
	restored = true
	f.unchanged(t, before)
	f.detail(t, "", id, http.StatusOK)
}

func TestTEST017LargeMultilingualChangelogsUseBoundedFullHTTPCursors(t *testing.T) {
	f := newTEST017Fixture(t)
	base := "/api/v1/changelogs?targetType=mod&targetId=" + f.modCode
	locales := supportedContentLocaleList()
	if len(locales) != projectChangelogMaximumLocalizations {
		t.Fatalf("eight-language production contract changed: %v", locales)
	}
	ids := map[string]bool{}
	for index := 0; index < 3; index++ {
		snapshot := test017Snapshot("")
		snapshot.ProjectVersion = fmt.Sprintf("large-%d", index)
		snapshot.Localizations = nil
		for _, locale := range locales {
			snapshot.Localizations = append(snapshot.Localizations, projectChangelogLocalization{Locale: locale, BodyMarkdown: strings.Repeat("x", 180000) + "SECRET-TAIL-" + locale})
		}
		proposal := f.submit(t, f.editor, http.MethodPost, base, snapshot, http.StatusCreated)
		f.review(t, f.denied, proposal, "approved", http.StatusOK)
		ids[proposal.ID] = true
	}
	cursor := ""
	firstCursor := ""
	seen := map[string]bool{}
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		path := base + "&locale=ja-JP&limit=1&cursor=" + url.QueryEscape(cursor)
		raw := f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
		var response struct {
			Data struct {
				Items      []projectChangelogSummary `json:"items"`
				HasMore    bool                      `json:"hasMore"`
				NextCursor string                    `json:"nextCursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if len(raw) > 32<<10 || bytes.Contains(raw, []byte("SECRET-TAIL")) || bytes.Contains(raw, []byte(`"localizations"`)) || len(response.Data.Items) != 1 {
			t.Fatalf("public summary budget/fidelity mismatch: bytes=%d items=%d", len(raw), len(response.Data.Items))
		}
		item := response.Data.Items[0]
		if !ids[item.ID] || seen[item.ID] || item.Locale != "ja-JP" || !item.BodyTruncated || len(item.BodyExcerpt) != 4000 || len(item.AvailableLocales) != 8 {
			t.Fatalf("duplicate/lost/truncated metadata: %+v", item)
		}
		seen[item.ID] = true
		if response.Data.HasMore != (pageNumber < 2) || (response.Data.NextCursor != "") != response.Data.HasMore {
			t.Fatalf("invalid cursor end at page %d", pageNumber)
		}
		cursor = response.Data.NextCursor
		if pageNumber == 0 {
			firstCursor = cursor
		}
	}
	for _, path := range []string{base + "&locale=en-US&limit=1&cursor=" + url.QueryEscape(firstCursor), base + "&locale=ja-JP&limit=2&cursor=" + url.QueryEscape(firstCursor), "/api/v1/changelogs?targetType=mod&targetId=" + f.otherModCode + "&locale=ja-JP&limit=1&cursor=" + url.QueryEscape(firstCursor)} {
		f.require(t, "", http.MethodGet, path, nil, http.StatusBadRequest)
	}
	if len(seen) != 3 {
		t.Fatal("full cursor traversal lost a changelog")
	}
}
