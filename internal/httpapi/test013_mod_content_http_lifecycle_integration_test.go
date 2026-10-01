package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

type test013Fixture struct {
	ctx                                   context.Context
	db                                    *pgxpool.Pool
	origin                                *httptest.Server
	editor, reviewer, otherEditor, denied string
	userIDs                               map[string]int64
	modID, otherModID                     int64
	modCode, otherModCode                 string
}

func newTEST013Fixture(t *testing.T) test013Fixture {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" && os.Getenv("MCMODS_TEST_DATABASE_URL") == "" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned loopback database to run the full HTTP lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	connection, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	if connection.ConnConfig.Host != "127.0.0.1" && connection.ConnConfig.Host != "localhost" && connection.ConnConfig.Host != "::1" {
		t.Fatal("TEST013 requires an explicitly configured owned loopback PostgreSQL server; refusing a non-loopback fallback")
	}
	db := newTEST018IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.JWTSecret = "test013-only-signing-secret-at-least-32-characters"
	cfg.SettingsEncryptionKey = "test013-only-settings-key-at-least-32-characters"
	cfg.Redis.Enabled, cfg.AntiAbuse.Enabled = false, false
	cfg.NATS, cfg.SMTP = config.NATSConfig{}, config.SMTPConfig{}
	q := queue.New(ctx, cfg.NATS)
	t.Cleanup(q.Close)
	server := NewServer(ctx, cfg, db, q, nil, nil, nil)
	t.Cleanup(func() {
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			t.Error(err)
		}
		server.cache.Close()
	})
	origin := httptest.NewServer(server)
	t.Cleanup(origin.Close)
	f := test013Fixture{ctx: ctx, db: db, origin: origin, userIDs: make(map[string]int64)}
	for _, user := range []struct {
		suffix string
		token  *string
	}{
		{"test013-editor", &f.editor}, {"test013-reviewer", &f.reviewer},
		{"test013-other-editor", &f.otherEditor}, {"test013-denied", &f.denied},
	} {
		id, _, token := createTEST044User(t, ctx, db, cfg, user.suffix, "UTC")
		*user.token = token
		f.userIDs[token] = id
	}
	f.modCode, f.otherModCode = randomCatalogPublicID(), randomCatalogPublicID()
	for _, mod := range []struct {
		slug, code, token string
		id                *int64
	}{
		{"test013-mod", f.modCode, f.editor, &f.modID},
		{"test013-other", f.otherModCode, f.otherEditor, &f.otherModID},
	} {
		if err := db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
			values($1,$2,$2,'approved',$3) returning id`, mod.code, mod.slug, f.userIDs[mod.token]).Scan(mod.id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `insert into mod_loader_compatibilities(mod_id,loader,minecraft_version) values($1,'neoforge','1.21.1')`, *mod.id); err != nil {
			t.Fatal(err)
		}
		grantTEST044Permissions(t, ctx, db, f.userIDs[mod.token], "project.edit."+mod.code)
	}
	grantTEST044Permissions(t, ctx, db, f.userIDs[f.reviewer], "content.review")
	review := defaultReviewConfig()
	review.ModContentSectionCreate = true
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into system_settings(key,value) values($1,$2)
		on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f test013Fixture) require(t *testing.T, token, method, path string, body any, want int) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(f.ctx, method, f.origin.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := f.origin.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode != want {
		t.Fatalf("%s %s status=%d want=%d err=%v body=%s", method, path, response.StatusCode, want, err, raw)
	}
	if want >= 400 && bytes.Contains(raw, []byte(`"data":`)) {
		t.Fatalf("failure contains partial success data: %s", raw)
	}
	return raw
}

func (f test013Fixture) submit(t *testing.T, token, method, path string, body any, want int) modContentMutationResult {
	t.Helper()
	raw := f.require(t, token, method, path, body, want)
	var response struct {
		Data modContentMutationResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	result := response.Data
	if result.PublicID == "" || result.RevisionID == "" || result.ChangeRequestID == "" || result.ActivityEventID == "" || result.ReviewStatus != "pending" {
		t.Fatalf("mutation bypassed actual pending review or lacks durable facts: %s", raw)
	}
	f.assertReviewFacts(t, result, token, "pending")
	return result
}

func (f test013Fixture) assertReviewFacts(t *testing.T, result modContentMutationResult, token, want string) {
	t.Helper()
	var status string
	var actorID, submittedBy int64
	var submitted, resolved, published, activity int
	if err := f.db.QueryRow(f.ctx, `select request.status,revision.created_by,request.submitted_by,
		(select count(*) from review_events where change_request_id=request.id and event_type='submitted' and actor_id=$4),
		(select count(*) from review_events where change_request_id=request.id and event_type=$5 and actor_id=$6),
		(select count(*) from review_events where change_request_id=request.id and event_type='published' and actor_id=$6),
		(select count(*) from user_activity_events event join public_routes route on route.id=event.object_route_id
		 where event.id::text=$3 and event.user_id=$4 and route.public_id=revision.snapshot->>'modId')
		from content_revisions revision join change_requests request on request.proposed_revision_id=revision.id
		where revision.public_id=$1 and request.public_id=$2`, result.RevisionID, result.ChangeRequestID, result.ActivityEventID,
		f.userIDs[token], want, f.userIDs[f.reviewer]).Scan(&status, &actorID, &submittedBy, &submitted, &resolved, &published, &activity); err != nil {
		t.Fatal(err)
	}
	wantResolved, wantPublished := 0, 0
	if want != "pending" {
		wantResolved = 1
	}
	if want == "approved" {
		wantPublished = 1
	}
	if status != want || actorID != f.userIDs[token] || submittedBy != actorID || submitted != 1 || resolved != wantResolved || published != wantPublished || activity != 1 {
		t.Fatalf("durable mutation facts status=%s actors=%d/%d submitted=%d resolved=%d published=%d activity=%d", status, actorID, submittedBy, submitted, resolved, published, activity)
	}
}

func (f test013Fixture) review(t *testing.T, result modContentMutationResult, token, status string) {
	t.Helper()
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+result.RevisionID, map[string]any{"status": status, "note": "test013 actual independent review"}, http.StatusOK)
	f.assertReviewFacts(t, result, token, status)
	if status != "approved" {
		return
	}
	var snapshot modContentSnapshot
	var raw []byte
	if err := f.db.QueryRow(f.ctx, `select snapshot from content_revisions where public_id=$1`, result.RevisionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	table := map[string]string{"version": "mod_content_versions", "template": "mod_content_templates", "section": "mod_content_sections", "layout": "mod_content_sections"}[snapshot.Kind]
	var publishedID string
	if snapshot.Kind == "resource" {
		if err := f.db.QueryRow(f.ctx, `select revision.public_id from mod_resource_version_details detail
			join catalog_entities entity on entity.id=detail.resource_id join mod_content_versions version on version.id=detail.version_id
			join content_revisions revision on revision.id=detail.published_revision_id where entity.public_id=$1 and version.public_id=$2`,
			result.PublicID, snapshot.Resource.VersionPublicID).Scan(&publishedID); err != nil {
			t.Fatal(err)
		}
	} else {
		if table == "" {
			t.Fatalf("unexpected reviewed kind %q", snapshot.Kind)
		}
		if err := f.db.QueryRow(f.ctx, "select revision.public_id from "+pgx.Identifier{table}.Sanitize()+" subject join content_revisions revision on revision.id=subject.published_revision_id where subject.public_id=$1", result.PublicID).Scan(&publishedID); err != nil {
			t.Fatal(err)
		}
	}
	if publishedID != result.RevisionID {
		t.Fatalf("published pointer=%s want actual reviewed %s", publishedID, result.RevisionID)
	}
}

// Snapshot complete business rows, not only counts: stable IDs, values, times and pointers.
// Request/access logs and auth last-seen are not business publication facts.
func (f test013Fixture) facts(t *testing.T) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{"mods", "mod_content_versions", "mod_content_templates", "mod_content_template_localizations",
		"mod_content_sections", "mod_content_section_localizations", "mod_content_section_resources", "mod_resource_bindings",
		"catalog_entities", "game_resources", "mod_resource_version_details", "mod_resource_version_detail_localizations",
		"content_revisions", "content_change_items", "change_requests", "review_events", "audit_events", "user_activity_events",
		"unresolved_references", "unresolved_resource_references", "project_update_events", "project_update_notification_tasks", "notifications",
		"nats_outbox", "activity_event_outbox", "public_routes"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(fact_row) order by to_jsonb(fact_row)::text),'[]'::jsonb)::text from "+pgx.Identifier{table}.Sanitize()+" fact_row").Scan(&raw); err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		result[table] = raw
	}
	return result
}

func (f test013Fixture) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	if !reflect.DeepEqual(before, after) {
		for table, value := range before {
			if after[table] != value {
				t.Errorf("rejected/failed operation partially changed %s", table)
			}
		}
		t.FailNow()
	}
}

func (f test013Fixture) version(t *testing.T, token, slug, label string) modContentMutationResult {
	t.Helper()
	result := f.submit(t, token, http.MethodPost, "/api/v1/mods/"+slug+"/content-versions", modContentVersionEdit{
		Label: label, MinecraftVersions: []string{"1.21.1"}, Loaders: []string{"neoforge"}, ModVersion: label, Reason: "test013 version"}, http.StatusCreated)
	f.review(t, result, token, "approved")
	return result
}

func (f test013Fixture) section(t *testing.T, version, template string) modContentMutationResult {
	t.Helper()
	result := f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-sections", modContentSectionEdit{
		VersionPublicID: version, TemplatePublicID: template, DefaultLocale: "en-US", DisplayMode: "compact",
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "TEST013 section"}}, Reason: "test013 section"}, http.StatusCreated)
	f.review(t, result, f.editor, "approved")
	return result
}

func (f test013Fixture) builtin(t *testing.T, code string) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(f.ctx, `select public_id from mod_content_templates where builtin and code=$1 and status='active'`, code).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f test013Fixture) resource(t *testing.T, version, section, canonical, kind, entryType string, definition map[string]any) modContentMutationResult {
	t.Helper()
	return f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-resources", modContentResourceEdit{
		VersionPublicID: version, SectionPublicID: &section, CanonicalID: canonical, KindCode: kind, EntryTypeCode: entryType,
		DefaultLocale: "en-US", Definition: definition, Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "TEST013 resource"}}, Reason: "test013 resource"}, http.StatusCreated)
}

func TestTEST013CustomTemplateDestructiveHTTPReviewLifecycle(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "template version")
	edit := modContentTemplateEdit{Code: "machine", DefaultLocale: "en-US", DefaultDisplayMode: "compact", Definition: templateSchemaGuardMap(false, "number", false),
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Machine"}}, Reason: "test013 template"}
	before := f.facts(t)
	f.require(t, "", http.MethodPost, "/api/v1/mods/test013-mod/content-templates", edit, http.StatusUnauthorized)
	f.require(t, f.denied, http.MethodPost, "/api/v1/mods/test013-mod/content-templates", edit, http.StatusForbidden)
	f.unchanged(t, before)
	template := f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-templates", edit, http.StatusCreated)
	f.review(t, template, f.editor, "approved")
	section := f.section(t, version.PublicID, template.PublicID)
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:machine", "minecraft.item", "machine", map[string]any{"power": 100})
	f.review(t, resource, f.editor, "approved")
	path := "/api/v1/mods/test013-mod/content-templates/" + template.PublicID
	for _, change := range []struct {
		name, method string
		body         any
	}{
		{"delete referenced template", http.MethodDelete, map[string]any{"reason": "test013", "baseRevisionId": template.RevisionID}},
		{"delete referenced entry type", http.MethodPut, modContentTemplateEdit{Code: edit.Code, DefaultLocale: edit.DefaultLocale, DefaultDisplayMode: edit.DefaultDisplayMode,
			Definition: templateSchemaGuardMap(false, "number", true), Localizations: edit.Localizations, BaseRevisionID: &template.RevisionID}},
		{"change referenced field storage", http.MethodPut, modContentTemplateEdit{Code: edit.Code, DefaultLocale: edit.DefaultLocale, DefaultDisplayMode: edit.DefaultDisplayMode,
			Definition: templateSchemaGuardMap(false, "text", false), Localizations: edit.Localizations, BaseRevisionID: &template.RevisionID}},
	} {
		t.Run(change.name, func(t *testing.T) {
			pending := f.submit(t, f.editor, change.method, path, change.body, http.StatusOK)
			beforeReview := f.facts(t)
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+pending.RevisionID, map[string]any{"status": "approved"}, http.StatusInternalServerError)
			f.unchanged(t, beforeReview)
			f.assertReviewFacts(t, pending, f.editor, "pending")
			f.review(t, pending, f.editor, "rejected")
			f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, nil, http.StatusOK)
		})
	}
	edit.Definition, edit.BaseRevisionID = templateSchemaGuardMap(true, "number", false), &template.RevisionID
	disabled := f.submit(t, f.editor, http.MethodPut, path, edit, http.StatusOK)
	f.review(t, disabled, f.editor, "approved")
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-resources", modContentResourceEdit{
		VersionPublicID: version.PublicID, SectionPublicID: &section.PublicID, CanonicalID: "test013:new_machine", KindCode: "minecraft.item", EntryTypeCode: "machine",
		DefaultLocale: "en-US", Definition: map[string]any{"power": 1}, Localizations: edit.Localizations}, http.StatusUnprocessableEntity)
	f.unchanged(t, before)
	f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, nil, http.StatusOK)
	existingEdit := modContentResourceEdit{ResourcePublicID: resource.PublicID, VersionPublicID: version.PublicID, SectionPublicID: &section.PublicID, EntryTypeCode: "machine",
		DefaultLocale: "en-US", Definition: map[string]any{"power": 200}, Localizations: edit.Localizations, BaseRevisionID: &resource.RevisionID}
	updated := f.submit(t, f.editor, http.MethodPut, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, existingEdit, http.StatusOK)
	f.review(t, updated, f.editor, "approved")
	_, definition := test013DetailRevision(t, f, resource.PublicID, version.PublicID)
	if definition["power"] != float64(200) {
		t.Fatalf("disabled subtype lost editable existing definition: %v", definition)
	}
	otherDisabledEdit := modContentTemplateEdit{Code: "other_machine", DefaultLocale: "en-US", DefaultDisplayMode: "compact", Definition: templateSchemaGuardMap(true, "number", false), Localizations: edit.Localizations}
	otherDisabled := f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-templates", otherDisabledEdit, http.StatusCreated)
	f.review(t, otherDisabled, f.editor, "approved")
	otherSection := f.section(t, version.PublicID, otherDisabled.PublicID)
	existingEdit.SectionPublicID, existingEdit.BaseRevisionID = &otherSection.PublicID, &updated.RevisionID
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPut, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, existingEdit, http.StatusUnprocessableEntity)
	f.unchanged(t, before)
	archived := f.submit(t, f.editor, http.MethodDelete, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID+"?version="+version.PublicID, nil, http.StatusOK)
	f.review(t, archived, f.editor, "approved")
	f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, nil, http.StatusNotFound)
	historicalDelete := f.submit(t, f.editor, http.MethodDelete, path, nil, http.StatusOK)
	before = f.facts(t)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+historicalDelete.RevisionID, map[string]any{"status": "approved"}, http.StatusInternalServerError)
	f.unchanged(t, before)
	f.review(t, historicalDelete, f.editor, "rejected")
	edit.Code, edit.BaseRevisionID, edit.Definition = "unused_machine", nil, templateSchemaGuardMap(false, "number", false)
	unused := f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-templates", edit, http.StatusCreated)
	f.review(t, unused, f.editor, "approved")
	edit.BaseRevisionID, edit.Definition = &unused.RevisionID, templateSchemaGuardMap(false, "number", true)
	unusedEdit := f.submit(t, f.editor, http.MethodPut, "/api/v1/mods/test013-mod/content-templates/"+unused.PublicID, edit, http.StatusOK)
	f.review(t, unusedEdit, f.editor, "approved")
	deletion := f.submit(t, f.editor, http.MethodDelete, "/api/v1/mods/test013-mod/content-templates/"+unused.PublicID, map[string]any{"baseRevisionId": unusedEdit.RevisionID}, http.StatusOK)
	f.review(t, deletion, f.editor, "approved")
	var status string
	if err := f.db.QueryRow(f.ctx, `select status from mod_content_templates where public_id=$1`, unused.PublicID).Scan(&status); err != nil || status != "archived" {
		t.Fatalf("unused template=%s err=%v", status, err)
	}
	// Fail after the real publisher changed rows, but before review/publication commit.
	edit.Code, edit.BaseRevisionID = "rollback_machine", nil
	rollback := f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-templates", edit, http.StatusCreated)
	if _, err := f.db.Exec(f.ctx, `alter table review_events add constraint test013_reject_publication check(event_type<>'published') not valid`); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+rollback.RevisionID, map[string]any{"status": "approved"}, http.StatusInternalServerError)
	f.unchanged(t, before)
	if _, err := f.db.Exec(f.ctx, `alter table review_events drop constraint test013_reject_publication`); err != nil {
		t.Fatal(err)
	}
	f.review(t, rollback, f.editor, "approved")
}

func TestTEST013CrossVersionAndGlobalResourceHTTPBoundaries(t *testing.T) {
	f := newTEST013Fixture(t)
	v1, v2 := f.version(t, f.editor, "test013-mod", "first"), f.version(t, f.editor, "test013-mod", "second")
	foreign := f.version(t, f.otherEditor, "test013-other", "foreign")
	s1, s2 := f.section(t, v1.PublicID, f.builtin(t, "item_block")), f.section(t, v2.PublicID, f.builtin(t, "item_block"))
	for _, bad := range []struct{ name, version, section string }{
		{"cross version section", v1.PublicID, s2.PublicID}, {"foreign mod version", foreign.PublicID, s1.PublicID},
	} {
		t.Run(bad.name, func(t *testing.T) {
			before := f.facts(t)
			f.require(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-resources", modContentResourceEdit{
				VersionPublicID: bad.version, SectionPublicID: &bad.section, CanonicalID: "test013:bad_resource", KindCode: "minecraft.item", EntryTypeCode: "default",
				DefaultLocale: "en-US", Definition: map[string]any{}, Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Bad"}}}, http.StatusUnprocessableEntity)
			f.unchanged(t, before)
		})
	}
	before := f.facts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-sections", modContentSectionEdit{
		VersionPublicID: v2.PublicID, TemplatePublicID: f.builtin(t, "item_block"), ParentPublicID: s1.PublicID, DefaultLocale: "en-US", DisplayMode: "compact",
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Cross parent"}}}, http.StatusUnprocessableEntity)
	f.unchanged(t, before)
	var globalID int64
	globalPublicID := randomCatalogPublicID()
	if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		values('resource:test013-global',$1,'resource','active') returning id`, globalPublicID).Scan(&globalID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		values($1,'minecraft.item','test013:global','test013','global',null,true)`, globalID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+globalPublicID, nil, http.StatusNotFound)
	globalEdit := modContentResourceEdit{ResourcePublicID: globalPublicID, KindCode: "minecraft.item", VersionPublicID: v1.PublicID, SectionPublicID: &s1.PublicID,
		EntryTypeCode: "default", DefaultLocale: "en-US", Definition: map[string]any{}, Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Global bound"}}}
	bound := f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-resources", globalEdit, http.StatusCreated)
	f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+globalPublicID, nil, http.StatusNotFound)
	f.require(t, f.reviewer, http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+globalPublicID, nil, http.StatusOK)
	f.review(t, bound, f.editor, "approved")
	f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+globalPublicID, nil, http.StatusOK)
	var owner *int64
	var boundModID int64
	if err := f.db.QueryRow(f.ctx, `select resource.owner_mod_id,binding.mod_id from game_resources resource
		join mod_resource_bindings binding on binding.resource_id=resource.entity_id where resource.entity_id=$1`, globalID).Scan(&owner, &boundModID); err != nil || owner != nil || boundModID != f.modID {
		t.Fatalf("global identity owner=%v binding=%d err=%v", owner, boundModID, err)
	}
	globalEdit.VersionPublicID, globalEdit.SectionPublicID = foreign.PublicID, nil
	before = f.facts(t)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/mods/test013-other/content-resources", globalEdit, http.StatusConflict)
	f.require(t, f.reviewer, http.MethodGet, "/api/v1/mods/test013-other/content-resources/"+globalPublicID, nil, http.StatusNotFound)
	f.unchanged(t, before)
	globalEdit.VersionPublicID, globalEdit.SectionPublicID = v2.PublicID, &s2.PublicID
	second := f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-resources", globalEdit, http.StatusCreated)
	f.review(t, second, f.editor, "approved")
	public := f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+globalPublicID, nil, http.StatusOK)
	var payload struct {
		Data struct {
			Details []struct {
				Version string `json:"versionPublicId"`
				Section string `json:"sectionPublicId"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(public, &payload); err != nil {
		t.Fatal(err)
	}
	placements := map[string]string{}
	for _, detail := range payload.Data.Details {
		placements[detail.Version] = detail.Section
	}
	if !reflect.DeepEqual(placements, map[string]string{v1.PublicID: s1.PublicID, v2.PublicID: s2.PublicID}) {
		t.Fatalf("version placements leaked/missing: %s", public)
	}
	patch := modContentLayoutPatch{VersionPublicID: v2.PublicID, RootSectionPublicID: s1.PublicID, DisplayMode: "compact", BaseRevisionID: &s1.RevisionID}
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/mods/test013-mod/content-sections/"+s1.PublicID+"/layout", patch, http.StatusConflict)
	f.unchanged(t, before)
	var actualBase string
	if err := f.db.QueryRow(f.ctx, `select revision.public_id from mod_content_sections section
		join content_revisions revision on revision.id=section.published_revision_id where section.public_id=$1`, s1.PublicID).Scan(&actualBase); err != nil {
		t.Fatal(err)
	}
	patch.VersionPublicID, patch.BaseRevisionID = v1.PublicID, &actualBase
	patch.Resources = []modContentLayoutResourceEdit{{ResourcePublicID: globalPublicID, SectionPublicID: s2.PublicID}}
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/mods/test013-mod/content-sections/"+s1.PublicID+"/layout", patch, http.StatusUnprocessableEntity)
	f.unchanged(t, before)
	removed := f.submit(t, f.editor, http.MethodDelete, "/api/v1/mods/test013-mod/content-resources/"+globalPublicID+"?version="+v1.PublicID, nil, http.StatusOK)
	f.review(t, removed, f.editor, "approved")
	public = f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+globalPublicID, nil, http.StatusOK)
	if err := json.Unmarshal(public, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Details) != 1 || payload.Data.Details[0].Version != v2.PublicID || payload.Data.Details[0].Section != s2.PublicID {
		t.Fatalf("version deletion removed/leaked another version: %s", public)
	}
	sectionDeletion := f.submit(t, f.editor, http.MethodDelete, "/api/v1/mods/test013-mod/content-sections/"+s1.PublicID, nil, http.StatusOK)
	f.review(t, sectionDeletion, f.editor, "approved")
	var archivedSections, activeOtherSections, remainingPlacements int
	if err := f.db.QueryRow(f.ctx, `select
		(select count(*) from mod_content_sections section join mod_content_versions version on version.id=section.version_id where version.public_id=$1 and section.status='archived'),
		(select count(*) from mod_content_sections section join mod_content_versions version on version.id=section.version_id where version.public_id=$2 and section.status='active'),
		(select count(*) from mod_content_section_resources placement join mod_content_versions version on version.id=placement.version_id where version.public_id=$1)`,
		v1.PublicID, v2.PublicID).Scan(&archivedSections, &activeOtherSections, &remainingPlacements); err != nil {
		t.Fatal(err)
	}
	if archivedSections != 3 || activeOtherSections != 3 || remainingPlacements != 0 {
		t.Fatalf("section subtree archive crossed version: archived=%d activeOther=%d placements=%d", archivedSections, activeOtherSections, remainingPlacements)
	}
	versionDeletion := f.submit(t, f.editor, http.MethodDelete, "/api/v1/mods/test013-mod/content-versions/"+v1.PublicID, nil, http.StatusOK)
	f.review(t, versionDeletion, f.editor, "approved")
	listed := f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-versions", nil, http.StatusOK)
	if bytes.Contains(listed, []byte(v1.PublicID)) || !bytes.Contains(listed, []byte(v2.PublicID)) {
		t.Fatalf("version archive public visibility wrong: %s", listed)
	}
}

func TestTEST013VersionListMidstreamDatabaseErrorFailsClosed(t *testing.T) {
	f := newTEST013Fixture(t)
	f.version(t, f.editor, "test013-mod", "healthy")
	// Only the fresh owned database is altered. The original table keeps its
	// OID/FKs under a temporary name; the read view raises after data rows.
	if _, err := f.db.Exec(f.ctx, `insert into mod_content_versions(mod_id,label,status)
		select $1,'midstream-'||n,'active' from generate_series(1,32) n`, f.modID); err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.Query(f.ctx, `select column_name from information_schema.columns
		where table_schema='public' and table_name='mod_content_versions' order by ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	columns := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if name == "label" {
			columns = append(columns, "test013_stream_label(label) as label")
		} else {
			columns = append(columns, pgx.Identifier{name}.Sanitize())
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `create function test013_stream_label(input text) returns text language plpgsql immutable as $$
		begin
			if input not like 'midstream-%' then raise exception 'test013 actual midstream failure'; end if;
			return repeat(input,4096);
		end $$;
		alter table mod_content_versions rename to test013_original_versions`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "create view mod_content_versions as select "+strings.Join(columns, ",")+" from test013_original_versions"); err != nil {
		t.Fatal(err)
	}
	// Force a streaming index read in the owned-only fault fixture. Its pure
	// function fails on the last (oldest) row, after large valid data rows.
	// This is not a production index or performance measurement.
	if _, err = f.db.Exec(f.ctx, `create index test013_stream_order on test013_original_versions
		(mod_id,(status='active') desc,updated_at desc,id desc)`); err != nil {
		t.Fatal(err)
	}
	connections := make([]*pgxpool.Conn, 0, 8)
	for range 8 {
		connection, acquireErr := f.db.Acquire(f.ctx)
		if acquireErr != nil {
			for _, held := range connections {
				held.Release()
			}
			t.Fatal(acquireErr)
		}
		connections = append(connections, connection)
	}
	var settingErr error
	for _, connection := range connections {
		if _, setErr := connection.Exec(f.ctx, `set enable_seqscan=off; set enable_sort=off; set jit=off`); setErr != nil && settingErr == nil {
			settingErr = setErr
		}
		connection.Release()
	}
	if settingErr != nil {
		t.Fatal(settingErr)
	}
	var plan []byte
	if err = f.db.QueryRow(f.ctx, `explain (format json) select public_id,label from mod_content_versions where mod_id=$1
		order by (status='active') desc,updated_at desc,id desc`, f.modID).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(plan, []byte(`"Index Name": "test013_stream_order"`)) || bytes.Contains(plan, []byte(`"Node Type": "Sort"`)) {
		t.Fatalf("fault query is not a streaming index read: %s", plan)
	}
	rows, err = f.db.Query(f.ctx, `select public_id,label from mod_content_versions where mod_id=$1 order by (status='active') desc,updated_at desc,id desc`, f.modID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var id, label string
		if err = rows.Scan(&id, &label); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if id == "" || label == "" {
			t.Fatal("no valid row before failure")
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if count == 0 || err == nil || !strings.Contains(err.Error(), "actual midstream failure") {
		t.Fatalf("fixture not midstream: delivered=%d error=%v", count, err)
	}
	t.Logf("actual PostgreSQL delivered %d valid rows before rows.Err: %v", count, err)
	f.require(t, f.editor, http.MethodGet, "/api/v1/mods/test013-mod/content-versions", nil, http.StatusInternalServerError)
	if _, err = f.db.Exec(f.ctx, `drop view mod_content_versions; alter table test013_original_versions rename to mod_content_versions`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, "/api/v1/mods/test013-mod/content-versions", nil, http.StatusOK)
}

func test013DetailRevision(t *testing.T, f test013Fixture, resource, version string) (int64, map[string]any) {
	t.Helper()
	var revision int64
	var raw []byte
	if err := f.db.QueryRow(f.ctx, `select detail.published_revision_id,detail.definition from mod_resource_version_details detail
		join catalog_entities entity on entity.id=detail.resource_id join mod_content_versions version on version.id=detail.version_id
		where entity.public_id=$1 and version.public_id=$2`, resource, version).Scan(&revision, &raw); err != nil {
		t.Fatal(err)
	}
	var definition map[string]any
	if err := json.Unmarshal(raw, &definition); err != nil {
		t.Fatal(err)
	}
	return revision, definition
}
