package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reuse the owned full-server/session fixture, not handcrafted claims or a
// development database. Its independent approved mod also supplies add-on refs.
func test014Snapshot(f test013Fixture, projectType, siteID, name string, versions ...string) simpleProjectSnapshot {
	s := simpleProjectSnapshot{ProjectType: projectType, SiteID: siteID, DefaultLocale: "en-US",
		MinecraftVersions: versions, OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual",
		Localizations: []simpleProjectLocalization{{Locale: "en-US", Name: name, Summary: "TEST014 catalog summary", BodyMarkdown: "full detail"}},
		Links:         []modLinkPayload{{Type: "official", URL: "https://example.invalid/project"}}}
	switch projectType {
	case "plugin":
		s.Loaders, s.Categories = []string{"paper"}, []string{"utility"}
	case "map":
		s.MapSize, s.Categories = "medium", []string{"adventure"}
	case "resource_pack":
		s.Resolution, s.Categories, s.Features = "16x", []string{"vanilla_like"}, []string{"blocks"}
	case "shader_pack":
		s.Performance, s.Loaders, s.Categories = "medium", []string{"iris"}, []string{"vanilla"}
	case "datapack":
		s.Loaders, s.Categories = []string{"vanilla"}, []string{"utility"}
	case "addon":
		s.ParentProjects = []simpleProjectParent{{Type: "mod", PublicID: f.modCode}}
	}
	return s
}

func test014Create(t *testing.T, f test013Fixture, snapshot simpleProjectSnapshot) simpleProjectResponse {
	t.Helper()
	raw := f.require(t, f.editor, http.MethodPost, "/api/v1/content-projects/"+snapshot.ProjectType, snapshot, http.StatusCreated)
	var envelope struct {
		Data simpleProjectResponse `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	item := envelope.Data
	if item.ReviewStatus != "pending" || item.SubmissionRevisionID == "" || item.ChangeRequestID == "" || item.PublicID == "" || item.SiteID == "" || item.CanEdit {
		t.Fatalf("creation bypassed review or lacks identity: %s", raw)
	}
	return item
}

func test014Approve(t *testing.T, f test013Fixture, revisionID string, actor string) {
	t.Helper()
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+revisionID,
		map[string]any{"status": "approved", "note": "TEST014 independent review"}, http.StatusOK)
	var projectStatus string
	var creatorID, reviewerID int64
	var publishedEvents int
	if err := f.db.QueryRow(f.ctx, `select project.review_status,revision.created_by,
		(select actor_id from review_events where change_request_id=request.id and event_type='approved'),
		(select count(*) from review_events where change_request_id=request.id and event_type='published' and actor_id=$2)
		from content_revisions revision join change_requests request on request.proposed_revision_id=revision.id
		join simple_projects project on project.id=revision.entity_id and project.project_type=revision.entity_type
		and project.published_revision_id=revision.id where revision.public_id=$1`, revisionID, f.userIDs[f.reviewer]).Scan(&projectStatus, &creatorID, &reviewerID, &publishedEvents); err != nil {
		t.Fatal(err)
	}
	if projectStatus != "approved" || creatorID != f.userIDs[actor] || reviewerID != f.userIDs[f.reviewer] || publishedEvents != 1 {
		t.Fatalf("review publication facts mismatch: status=%s actor=%d reviewer=%d events=%d", projectStatus, creatorID, reviewerID, publishedEvents)
	}
}

type test014Page struct {
	Items []simpleProjectCatalogCard `json:"items"`
	Total int                        `json:"total"`
}

func test014Catalog(t *testing.T, f test013Fixture, token, kind string, parameters url.Values) (test014Page, []byte) {
	t.Helper()
	if parameters == nil {
		parameters = url.Values{}
	}
	if !parameters.Has("sort") {
		parameters.Set("sort", "name")
		parameters.Set("order", "asc")
	}
	raw := f.require(t, token, http.MethodGet, "/api/v1/content-projects/"+kind+"?"+parameters.Encode(), nil, http.StatusOK)
	var envelope struct {
		Data test014Page `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	assertCatalogResponseBudget(t, len(raw))
	return envelope.Data, raw
}

func test014AssertSites(t *testing.T, page test014Page, total int, sites ...string) {
	t.Helper()
	actual := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		actual = append(actual, item.SiteID)
	}
	if page.Total != total || !reflect.DeepEqual(actual, sites) {
		t.Fatalf("catalog total=%d sites=%v want total=%d sites=%v", page.Total, actual, total, sites)
	}
}

func runTEST014CatalogMatrix(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create.*")
	for _, kind := range simpleProjectTypeValues() {
		t.Run(kind, func(t *testing.T) {
			path := "/api/v1/content-projects/" + kind
			snapshot := test014Snapshot(f, kind, "test014-"+kind+"-a", "A TEST014 "+kind, "1.21.1")
			f.require(t, "", http.MethodPost, path, snapshot, http.StatusUnauthorized)
			before := test014Facts(t, f)
			f.require(t, f.denied, http.MethodPost, path, snapshot, http.StatusForbidden)
			if !reflect.DeepEqual(before, test014Facts(t, f)) {
				t.Fatal("denied creation changed durable business facts")
			}
			a := test014Create(t, f, snapshot)
			test014Approve(t, f, a.SubmissionRevisionID, f.editor)
			snapshot.SiteID, snapshot.Localizations[0].Name, snapshot.MinecraftVersions = "test014-"+kind+"-b", "B TEST014 "+kind, []string{"1.21.1", "1.21.2"}
			b := test014Create(t, f, snapshot)
			test014Approve(t, f, b.SubmissionRevisionID, f.editor)
			snapshot.SiteID, snapshot.Localizations[0].Name, snapshot.MinecraftVersions = "test014-"+kind+"-c", "C TEST014 "+kind, []string{"1.21.2"}
			c := test014Create(t, f, snapshot)
			test014Approve(t, f, c.SubmissionRevisionID, f.editor)
			snapshot.SiteID, snapshot.Localizations[0].Name, snapshot.MinecraftVersions = "test014-"+kind+"-pending", "Z TEST014 private "+kind, []string{"1.21.1", "1.21.2"}
			pending := test014Create(t, f, snapshot)
			for _, token := range []string{"", f.otherEditor, f.reviewer} {
				page, _ := test014Catalog(t, f, token, kind, nil)
				test014AssertSites(t, page, 3, a.SiteID, b.SiteID, c.SiteID)
				f.require(t, token, http.MethodGet, path+"/"+pending.SiteID, nil, http.StatusNotFound)
			}
			page, raw := test014Catalog(t, f, f.editor, kind, nil)
			test014AssertSites(t, page, 4, a.SiteID, b.SiteID, c.SiteID, pending.SiteID)
			first := page.Items[0]
			if !reflect.DeepEqual(first.Loaders, snapshot.Loaders) && len(first.Loaders)+len(snapshot.Loaders) != 0 ||
				!reflect.DeepEqual(first.Categories, snapshot.Categories) && len(first.Categories)+len(snapshot.Categories) != 0 ||
				!reflect.DeepEqual(first.Features, snapshot.Features) && len(first.Features)+len(snapshot.Features) != 0 ||
				first.Resolution != snapshot.Resolution || first.Performance != snapshot.Performance || first.MapSize != snapshot.MapSize {
				t.Fatalf("catalog classification drift: card=%+v snapshot=%+v", first, snapshot)
			}
			if len(first.Localizations) != 1 || first.Localizations[0].Name != "A TEST014 "+kind {
				t.Fatalf("catalog association mismatch: %+v", first.Localizations)
			}
			if len(snapshot.Categories) > 0 {
				filtered, _ := test014Catalog(t, f, "", kind, url.Values{"category": {snapshot.Categories[0]}})
				test014AssertSites(t, filtered, 3, a.SiteID, b.SiteID, c.SiteID)
			}
			if len(snapshot.Loaders) > 0 {
				filtered, _ := test014Catalog(t, f, "", kind, url.Values{"loader": {snapshot.Loaders[0]}})
				test014AssertSites(t, filtered, 3, a.SiteID, b.SiteID, c.SiteID)
			}
			if kind == "addon" {
				if len(first.ParentProjects) != 1 || first.ParentProjects[0].PublicID != f.modCode || first.ParentProjects[0].SiteID != "test013-mod" || first.ParentProjects[0].Unresolved {
					t.Fatalf("addon parent projection mismatch: %+v", first.ParentProjects)
				}
				filtered, _ := test014Catalog(t, f, "", kind, url.Values{"parent": {"mod:test013-mod"}})
				test014AssertSites(t, filtered, 3, a.SiteID, b.SiteID, c.SiteID)
				filtered, _ = test014Catalog(t, f, "", kind, url.Values{"parent": {"mod:test013-other"}})
				test014AssertSites(t, filtered, 0, []string{}...)
			}
			if bytes.Contains(raw, []byte("bodyMarkdown")) || bytes.Contains(raw, []byte("full detail")) {
				t.Fatalf("catalog leaked detail projection: %s", raw)
			}
			for _, tc := range []struct {
				mode  string
				sites []string
			}{{"all", []string{b.SiteID}}, {"any", []string{a.SiteID, b.SiteID, c.SiteID}}} {
				page, _ = test014Catalog(t, f, "", kind, url.Values{"version": {"1.21.1,1.21.2"}, "versionMode": {tc.mode}})
				test014AssertSites(t, page, len(tc.sites), tc.sites...)
			}
			page, _ = test014Catalog(t, f, f.editor, kind, url.Values{"version": {"1.21.1,1.21.2"}, "versionMode": {"all"}})
			test014AssertSites(t, page, 2, b.SiteID, pending.SiteID)
			page, _ = test014Catalog(t, f, "", kind, url.Values{"excludeSiteId": {b.SiteID}, "limit": {"1"}, "offset": {"0"}})
			test014AssertSites(t, page, 2, a.SiteID)
			page, _ = test014Catalog(t, f, "", kind, url.Values{"excludeSiteId": {b.SiteID}, "limit": {"1"}, "offset": {"1"}})
			test014AssertSites(t, page, 2, c.SiteID)
			f.require(t, "", http.MethodGet, path+"?versionMode=invalid", nil, http.StatusBadRequest)
			f.require(t, "", http.MethodGet, path+"?loader=invalid", nil, http.StatusBadRequest)
			f.require(t, f.denied, http.MethodGet, path+"/"+a.SiteID+"/editor", nil, http.StatusForbidden)
		})
	}
}

func TestTEST014DeepOffsetAndFullDetailHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `insert into simple_projects(project_type,slug,default_locale,primary_name,minecraft_versions,loaders,review_status)
		select 'plugin','deep-'||lpad(value::text,5,'0'),'en-US','DEEP-'||lpad(value::text,5,'0'),array['1.21.1'],array['paper'],'approved'
		from generate_series(1,10025) value;
		insert into simple_project_localizations(project_id,locale,name,summary)
		select id,'en-US',primary_name,'deep page summary' from simple_projects where slug like 'deep-%'`); err != nil {
		t.Fatal(err)
	}
	page, _ := test014Catalog(t, f, "", "plugin", url.Values{"q": {"DEEP-"}, "offset": {"10000"}, "limit": {"24"}})
	want := make([]string, 24)
	for i := range want {
		want[i] = fmt.Sprintf("deep-%05d", 10001+i)
	}
	test014AssertSites(t, page, 10025, want...)
	page, _ = test014Catalog(t, f, "", "plugin", url.Values{"q": {"DEEP-"}, "offset": {"10024"}, "limit": {"24"}})
	test014AssertSites(t, page, 10025, "deep-10025")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create.plugin")
	snapshot := test014Snapshot(f, "plugin", "test014-large", "Large TEST014", "1.21.1")
	body := strings.Repeat("DETAIL-ONLY-", (1<<20)/len("DETAIL-ONLY-"))
	snapshot.Localizations[0].BodyMarkdown = body
	snapshot.Localizations = append(snapshot.Localizations, simpleProjectLocalization{Locale: "zh-CN", Name: "TEST014 完整详情", BodyMarkdown: body}, simpleProjectLocalization{Locale: "ja-JP", Name: "TEST014 日本語", BodyMarkdown: body})
	item := test014Create(t, f, snapshot)
	test014Approve(t, f, item.SubmissionRevisionID, f.editor)
	page, raw := test014Catalog(t, f, "", "plugin", url.Values{"q": {"Large TEST014"}, "locale": {"zh-CN"}})
	test014AssertSites(t, page, 1, item.SiteID)
	if len(page.Items[0].Localizations) != 2 || bytes.Contains(raw, []byte("bodyMarkdown")) || bytes.Contains(raw, []byte("DETAIL-ONLY-")) {
		t.Fatalf("large body leaked to catalog bytes=%d", len(raw))
	}
	detailRaw := f.require(t, "", http.MethodGet, "/api/v1/content-projects/plugin/"+item.SiteID, nil, http.StatusOK)
	var detail struct {
		Data simpleProjectResponse `json:"data"`
	}
	if err := json.Unmarshal(detailRaw, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Data.Localizations) != 3 {
		t.Fatalf("detail locales=%d want3", len(detail.Data.Localizations))
	}
	for _, locale := range detail.Data.Localizations {
		if locale.BodyMarkdown != body {
			t.Fatalf("detail body truncated for %s", locale.Locale)
		}
	}
}

func TestTEST014AuthorApprovalMetadataSurvivesReviewedEditsHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create.plugin")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.authorship.manage")
	var role string
	if err := f.db.QueryRow(f.ctx, `select public_id from creator_role_definitions where code='developer'`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	snapshot := test014Snapshot(f, "plugin", "test014-authors", "Authors TEST014", "1.21.1")
	for i := 0; i < 2; i++ {
		var creator string
		if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values('author',$1,$1,'approved') returning public_id`, fmt.Sprintf("test014-author-%d", i)).Scan(&creator); err != nil {
			t.Fatal(err)
		}
		snapshot.Authors = append(snapshot.Authors, modAuthorPayload{CreatorID: creator, RoleID: &role})
	}
	item := test014Create(t, f, snapshot)
	test014Approve(t, f, item.SubmissionRevisionID, f.editor)
	var projectID int64
	if err := f.db.QueryRow(f.ctx, `select id from simple_projects where public_id=$1`, item.PublicID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.Query(f.ctx, `select public_id from content_creator_bindings where subject_type='plugin' and subject_id=$1 order by id`, projectID)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 {
		t.Fatalf("bindings=%v", bindings)
	}
	for _, id := range bindings {
		before := test014Facts(t, f)
		f.require(t, f.denied, http.MethodPatch, "/api/v1/admin/project-authorship-relations/"+id, map[string]any{"status": "approved"}, http.StatusForbidden)
		if !reflect.DeepEqual(before, test014Facts(t, f)) {
			t.Fatal("denied author approval changed durable facts")
		}
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/project-authorship-relations/"+id, map[string]any{"status": "approved"}, http.StatusOK)
	}
	var approved int
	if err := f.db.QueryRow(f.ctx, `select count(*) from content_creator_bindings where subject_type='plugin' and subject_id=$1 and status='approved' and approved_by=$2 and approved_at is not null`, projectID, f.userIDs[f.reviewer]).Scan(&approved); err != nil {
		t.Fatal(err)
	}
	if approved != 2 {
		t.Fatalf("actual independently approved relationships=%d want2", approved)
	}
	approvalFacts := func() string {
		var facts string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(jsonb_build_object('id',id,'publicId',public_id,'creator',creator_id,'role',role_id,'status',status,'approvedBy',approved_by,'approvedAt',approved_at,'createdAt',created_at) order by id),'[]')::text from content_creator_bindings where subject_type='plugin' and subject_id=$1`, projectID).Scan(&facts); err != nil {
			t.Fatal(err)
		}
		return facts
	}
	before := approvalFacts()
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+item.PublicID)
	for round := 0; round < 2; round++ {
		snapshot.Localizations[0].Summary = fmt.Sprintf("changed metadata %d", round)
		if round == 1 {
			snapshot.Authors[0], snapshot.Authors[1] = snapshot.Authors[1], snapshot.Authors[0]
		}
		current := f.require(t, f.editor, http.MethodGet, "/api/v1/content-projects/plugin/"+item.SiteID+"/editor", nil, http.StatusOK)
		var editor struct {
			Data simpleProjectResponse `json:"data"`
		}
		if err := json.Unmarshal(current, &editor); err != nil {
			t.Fatal(err)
		}
		raw := f.require(t, f.editor, http.MethodPut, "/api/v1/content-projects/plugin/"+item.SiteID, map[string]any{"snapshot": snapshot, "baseRevisionId": editor.Data.PublishedRevisionID, "changeReason": "TEST014 metadata"}, http.StatusCreated)
		var revision struct {
			Data struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &revision); err != nil {
			t.Fatal(err)
		}
		if revision.Data.Status != "pending" {
			t.Fatalf("edit bypassed review: %s", raw)
		}
		test014Approve(t, f, revision.Data.ID, f.editor)
		if after := approvalFacts(); after != before {
			t.Fatalf("ordinary reviewed edit changed approval identity/timestamps: before=%s after=%s", before, after)
		}
	}
}

func TestTEST014ConcurrentAutomaticSlugAllocationHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	original, ok := f.origin.Config.Handler.(*Server)
	if !ok {
		t.Fatal("full HTTP fixture is not the actual Server")
	}
	otherPool, err := pgxpool.NewWithConfig(f.ctx, f.db.Config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherPool.Close)
	otherServer := NewServer(f.ctx, original.cfg, otherPool, original.queue, nil, nil, nil)
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otherServer.Shutdown(shutdown); err != nil {
			t.Error(err)
		}
		otherServer.cache.Close()
	})
	otherOrigin := httptest.NewServer(otherServer)
	t.Cleanup(otherOrigin.Close)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create.plugin")
	snapshot := test014Snapshot(f, "plugin", "", "Concurrent TEST014", "1.21.1")
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		status int
		body   []byte
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		origin := f.origin.URL
		if i%2 != 0 {
			origin = otherOrigin.URL
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			request, requestErr := http.NewRequestWithContext(f.ctx, http.MethodPost, origin+"/api/v1/content-projects/plugin", bytes.NewReader(raw))
			if requestErr != nil {
				results <- result{err: requestErr}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+f.editor)
			response, sendErr := f.origin.Client().Do(request)
			if sendErr != nil {
				results <- result{err: sendErr}
				return
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			results <- result{status: response.StatusCode, body: body, err: readErr}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	sites := map[string]bool{}
	failures := []string{}
	for result := range results {
		if result.err != nil || result.status != http.StatusCreated {
			failures = append(failures, fmt.Sprintf("status=%d err=%v body=%s", result.status, result.err, result.body))
			continue
		}
		var item struct {
			Data simpleProjectResponse `json:"data"`
		}
		if err := json.Unmarshal(result.body, &item); err != nil {
			t.Fatal(err)
		}
		if sites[item.Data.SiteID] {
			t.Fatalf("duplicate slug %q", item.Data.SiteID)
		}
		sites[item.Data.SiteID] = true
	}
	if len(failures) != 0 {
		t.Fatalf("automatic allocation must resolve concurrent collisions without failed creates: successes=%d failures=%v", len(sites), failures)
	}
	if len(sites) != 12 {
		t.Fatalf("allocated %d slugs want12", len(sites))
	}
	var projects, revisions, requests int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from simple_projects where project_type='plugin'),(select count(*) from content_revisions where entity_type='plugin'),(select count(*) from change_requests where entity_type='plugin')`).Scan(&projects, &revisions, &requests); err != nil {
		t.Fatal(err)
	}
	if projects != 12 || revisions != 12 || requests != 12 {
		t.Fatalf("atomic allocation counts: projects=%d revisions=%d requests=%d", projects, revisions, requests)
	}
	// A failure after reservation/insert must roll back and release the database
	// lock, so a subsequent request can allocate the exact same available slug.
	before := test014Facts(t, f)
	if _, err = f.db.Exec(f.ctx, `alter table content_revisions add constraint test014_fail_create check(entity_type is distinct from 'plugin') not valid`); err != nil {
		t.Fatal(err)
	}
	snapshot.Localizations[0].Name = "Rollback TEST014"
	f.require(t, f.editor, http.MethodPost, "/api/v1/content-projects/plugin", snapshot, http.StatusInternalServerError)
	if !reflect.DeepEqual(before, test014Facts(t, f)) {
		t.Fatal("post-reservation revision failure left partial durable facts")
	}
	if _, err = f.db.Exec(f.ctx, `alter table content_revisions drop constraint test014_fail_create`); err != nil {
		t.Fatal(err)
	}
	recovered := test014Create(t, f, snapshot)
	if recovered.SiteID != modSiteIDBase(snapshot.Localizations[0].Name) {
		t.Fatalf("rollback leaked reservation, recovered slug=%s", recovered.SiteID)
	}
}

func test014Facts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := map[string]string{}
	for _, name := range []string{"simple_projects", "simple_project_localizations", "simple_project_links", "simple_project_parent_refs", "simple_project_gallery_images", "content_creator_bindings", "creators", "content_revisions", "content_change_items", "change_requests", "review_events", "audit_events", "permission_audit_logs", "public_routes", "nats_outbox", "activity_event_outbox", "project_update_events", "project_update_notification_tasks", "user_activity_events"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]')::text from `+pgx.Identifier{name}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[name] = raw
	}
	return facts
}

func TestSimpleProjectCatalogCountIntegration(t *testing.T) { runTEST014CatalogMatrix(t) }

func TestTEST014CatalogAssociationMidstreamErrorFailsClosedIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `insert into simple_projects(project_type,slug,primary_name,default_locale,minecraft_versions,loaders,review_status)
		select 'plugin','test014-stream-'||value,'TEST014 stream '||value,'en-US',array['1.21.1'],array['paper'],'approved'
		from generate_series(1,97) value;
		insert into simple_project_localizations(project_id,locale,name,summary)
		select id,'en-US',primary_name,case when slug='test014-stream-97' then 'test014-fault' else 'valid-stream-summary' end
		from simple_projects where project_type='plugin'`); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	if err := f.db.QueryRow(f.ctx, `select array_agg(id order by id) from simple_projects where project_type='plugin'`).Scan(&ids); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 97 {
		t.Fatalf("fault fixture ids=%d want97", len(ids))
	}
	if _, err := f.db.Exec(f.ctx, `alter table simple_project_localizations rename to test014_original_localizations;
		create function test014_stream_summary(input text) returns text language plpgsql immutable as $$
		begin if input='test014-fault' then raise exception 'actual TEST014 association stream failure'; end if;
		return repeat(input,512); end; $$;
		create view simple_project_localizations as select project_id,locale,name,test014_stream_summary(summary) summary,body_markdown
		from test014_original_localizations`); err != nil {
		t.Fatal(err)
	}
	// Settings belong only to this test's owned pool/database. The production SQL
	// constant stays unchanged; actual delivered rows, not a plan guess, prove the
	// failure occurs after successful row iteration.
	connections := make([]*pgxpool.Conn, 0, f.db.Config().MaxConns)
	var settingErr error
	for i := int32(0); i < f.db.Config().MaxConns; i++ {
		conn, err := f.db.Acquire(f.ctx)
		if err != nil {
			settingErr = err
			break
		}
		connections = append(connections, conn)
		if _, err = conn.Exec(f.ctx, `set enable_seqscan=off; set enable_sort=off; set enable_hashjoin=off; set enable_mergejoin=off; set jit=off`); err != nil {
			settingErr = err
			break
		}
	}
	for _, conn := range connections {
		conn.Release()
	}
	if settingErr != nil {
		t.Fatal(settingErr)
	}
	var plan []byte
	if err := f.db.QueryRow(f.ctx, `explain (format json) `+simpleProjectCatalogLocalizationsSQL, ids, "en-US").Scan(&plan); err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.Query(f.ctx, simpleProjectCatalogLocalizationsSQL, ids, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var id int64
		var locale, name, summary string
		if err = rows.Scan(&id, &locale, &name, &summary); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if id == 0 || locale != "en-US" || name == "" || !strings.HasPrefix(summary, "valid-stream-summary") {
			rows.Close()
			t.Fatalf("invalid row before injected failure: id=%d locale=%s name=%s", id, locale, name)
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if count == 0 || count >= len(ids) || err == nil || !strings.Contains(err.Error(), "actual TEST014 association stream failure") {
		t.Fatalf("fault fixture not an actual midstream association read: rows=%d err=%v plan=%s", count, err, plan)
	}
	t.Logf("actual production association SQL delivered %d valid rows before rows.Err: %v", count, err)
	f.require(t, "", http.MethodGet, "/api/v1/content-projects/plugin?sort=name&limit=100&locale=en-US", nil, http.StatusInternalServerError)
	if _, err = f.db.Exec(f.ctx, `drop view simple_project_localizations; alter table test014_original_localizations rename to simple_project_localizations; drop function test014_stream_summary(text)`); err != nil {
		t.Fatal(err)
	}
	page, _ := test014Catalog(t, f, "", "plugin", url.Values{"limit": {"100"}})
	if page.Total != 97 || len(page.Items) != 97 {
		t.Fatalf("restored catalog total=%d items=%d", page.Total, len(page.Items))
	}
}
