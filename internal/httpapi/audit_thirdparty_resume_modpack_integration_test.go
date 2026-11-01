package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
)

func auditResumeModpackFacts(t *testing.T, f auditFixGRFixture) map[string]string {
	t.Helper()
	facts := f.test013Fixture.facts(t)
	for _, table := range []string{"modpacks", "modpack_loader_compatibilities", "modpack_tags", "modpack_links", "modpack_mods", "modpack_gallery_images", "oss_files", "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations", "content_creator_bindings", "creator_claims"} {
		var value string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(row) order by to_jsonb(row)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` row`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		facts[table] = value
	}
	return facts
}
func auditResumeModpackReviewPolicy(t *testing.T, f auditFixGRFixture) {
	t.Helper()
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create", "project.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.create", "modpack.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
	review := defaultReviewConfig()
	review.ModpackCreate = true
	review.ModCreate = true
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
}
func auditResumeManualPack(f auditFixGRFixture, name, future string) createModpackRequest {
	return createModpackRequest{SiteID: name, PrimaryName: "Owned mixed pack " + name, DefaultLocale: "en-US", Environment: "bothRequired", PrimaryCategory: "quests", PackType: "native", PackagingMethod: "manual", OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual", Compatibilities: []modLoaderCompatibilityPayload{{Loader: "forge", Versions: []string{"1.20.1"}}}, Tags: []string{"quests"}, Mods: []modpackModPayload{{ModPublicID: f.modCode, Provider: "manual", ModName: "Known owned Mod", ClientRequired: true, ServerRequired: true}, {Provider: "modrinth", ProviderProjectID: future, ProviderVersionID: "future-release", ModName: "Future source Mod", FileName: "future.jar", ClientRequired: true}, {Provider: "modrinth", ProviderProjectID: future, ProviderVersionID: "future-release", ModName: "Future source Mod", FileName: "future.jar", ClientRequired: true}}}
}
func auditResumeRequireFacts(t *testing.T, before, after map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		for table, was := range before {
			if was != after[table] {
				t.Errorf("unexpected persisted change in %s", table)
			}
		}
		t.FailNow()
	}
}
func auditResumeRequireUnresolved(t *testing.T, f auditFixGRFixture, pack modpackResponse, future string, wantResolved bool, resolvedPublic string) {
	t.Helper()
	if err := f.db.QueryRow(f.ctx, `select id from modpacks where public_id=$1`, pack.PublicID).Scan(&pack.ID); err != nil {
		t.Fatal(err)
	}
	var entries, unresolved, sourceRows int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from modpack_mods where modpack_id=$1),(select count(*) from unresolved_references unresolved join modpack_mods entry on entry.id=unresolved.source_id where unresolved.source_type='modpack_mod' and entry.modpack_id=$1),(select count(*) from mods where modrinth_project_id=$2)`, pack.ID, future).Scan(&entries, &unresolved, &sourceRows); err != nil {
		t.Fatal(err)
	}
	if entries != 2 || unresolved != 1 {
		t.Fatalf("mixed projection rows entries=%d unresolved=%d", entries, unresolved)
	}
	wantRows := 0
	if wantResolved {
		wantRows = 1
	}
	if sourceRows != wantRows {
		t.Fatalf("fabricated/missing external source Mod rows=%d expected=%d", sourceRows, wantRows)
	}
	var status, project, version, file, public string
	if err := f.db.QueryRow(f.ctx, `select unresolved.status,entry.provider_project_id,entry.provider_version_id,entry.file_name,coalesce(target.project_code,'') from modpack_mods entry join unresolved_references unresolved on unresolved.source_type='modpack_mod' and unresolved.source_id=entry.id left join mods target on target.id=entry.mod_id where entry.modpack_id=$1`, pack.ID).Scan(&status, &project, &version, &file, &public); err != nil {
		t.Fatal(err)
	}
	expected := "pending"
	if wantResolved {
		expected = "resolved"
	}
	if status != expected || project != future || version != "future-release" || file != "future.jar" || public != resolvedPublic {
		t.Fatalf("retained source state=%s project=%s version=%s file=%s public=%s", status, project, version, file, public)
	}
}

func TestAuditResumeCMP007ManualAndActualProviderWorkerResolveAfterIndependentApprovalIntegration(t *testing.T) {
	for _, mode := range []string{"manual", "provider-worker"} {
		t.Run(mode, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			auditResumeModpackReviewPolicy(t, f)
			future := "future-" + f.modCode
			draft := auditResumeManualPack(f, "mixed-"+mode+"-"+f.modCode, future)
			if mode == "provider-worker" {
				if _, err := f.db.Exec(f.ctx, `update mods set modrinth_project_id='known-owned-project' where id=$1`, f.modID); err != nil {
					t.Fatal(err)
				}
				var archive bytes.Buffer
				writer := zip.NewWriter(&archive)
				entry, err := writer.Create("modrinth.index.json")
				if err != nil {
					t.Fatal(err)
				}
				if err = json.NewEncoder(entry).Encode(map[string]any{"formatVersion": 1, "game": "minecraft", "versionId": "owned-pack-release", "name": "Owned mixed source", "dependencies": map[string]string{"minecraft": "1.20.1", "forge": "47.4.21"}, "files": []map[string]any{{"path": "mods/known.jar", "downloads": []string{"https://cdn.modrinth.com/data/known-owned-project/versions/known-release/known.jar"}}, {"path": "mods/future.jar", "downloads": []string{"https://cdn.modrinth.com/data/" + future + "/versions/future-release/future.jar"}}, {"path": "mods/future.jar", "downloads": []string{"https://cdn.modrinth.com/data/" + future + "/versions/future-release/future.jar"}}}}); err != nil {
					t.Fatal(err)
				}
				if err = writer.Close(); err != nil {
					t.Fatal(err)
				}
				var reads atomic.Int32
				var provider *httptest.Server
				provider = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
						t.Error("public synthetic source unexpectedly received credentials")
					}
					switch r.URL.Path {
					case "/project/owned-mixed-source":
						_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned-mixed-source", "slug": draft.SiteID, "title": "Controlled quest pack", "description": "Owned provider mixed reference", "body": "Known and not-yet-indexed source identities", "project_type": "modpack", "categories": []string{"quests"}, "status": "approved", "client_side": "required", "server_side": "required", "license": map[string]string{"id": "MIT"}})
					case "/project/owned-mixed-source/version":
						_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "owned-release", "name": "Owned release", "version_number": "1.0", "version_type": "release", "status": "listed", "date_published": "2026-10-01T00:00:00Z", "game_versions": []string{"1.20.1"}, "loaders": []string{"forge"}, "files": []map[string]any{{"url": provider.URL + "/archive.mrpack", "filename": "owned.mrpack", "primary": true}}}})
					case "/archive.mrpack":
						reads.Add(1)
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(archive.Bytes())
					default:
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(provider.Close)
				original := http.DefaultTransport
				http.DefaultTransport = provider.Client().Transport
				t.Cleanup(func() { http.DefaultTransport = original })
				cfg := defaultModImportConfig()
				cfg.Modrinth = modImportProviderConfig{Enabled: true, BaseURL: provider.URL}
				cfg.GitHub.Enabled = false
				cfg.CurseForge.Enabled = false
				sealed, err := f.server.sealSystemSetting(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
					t.Fatal(err)
				}
				job := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpack-imports", modMetadataImportRequest{Provider: "modrinth", URL: "https://modrinth.com/modpack/owned-mixed-source"}, 202))
				if err = f.server.runModMetadataImport(f.ctx, job.ID); err != nil {
					t.Fatal(err)
				}
				completed := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodGet, "/api/v1/modpack-imports/"+job.ID, nil, 200))
				if completed.Status != "completed" || completed.Progress != 100 || completed.Error != "" || reads.Load() != 1 {
					t.Fatalf("actual metadata Worker final=%s/%d downloads=%d error=%s", completed.Status, completed.Progress, reads.Load(), completed.Error)
				}
				if err = json.Unmarshal(completed.Result, &draft); err != nil {
					t.Fatal(err)
				}
				// Explicit ordinary form choice, not fabricated untranslated input.
				draft.DefaultLocale = "en-US"
				if draft.PrimaryCategory != "quests" || len(draft.Mods) != 2 {
					t.Fatalf("producer/category/duplicate contract category=%s mods=%d", draft.PrimaryCategory, len(draft.Mods))
				}
			}
			pack := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", draft, 201))
			if pack.ReviewStatus != "pending" || pack.SubmissionRevisionID == "" || len(pack.Mods) != 2 {
				t.Fatal("normal mixed pack did not remain pending with two logical references")
			}
			auditResumeRequireUnresolved(t, f, pack, future, false, "")
			// A distinct proposal with the same site identity must reach the
			// domain conflict. Exact repeated content can legitimately be refused
			// earlier by anti-abuse, which is a different contract.
			duplicateDraft := draft
			duplicateDraft.BodyMarkdown = "Different proposal for an already registered pack identity: " + f.modCode
			before := auditResumeModpackFacts(t, f)
			duplicate := f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", duplicateDraft, 409)
			if !strings.Contains(string(duplicate), "modpack site ID is already used") {
				t.Fatal("duplicate identity hit an unrelated conflict")
			}
			auditResumeRequireFacts(t, before, auditResumeModpackFacts(t, f))
			f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+pack.SiteID, nil, 404)
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+pack.SubmissionRevisionID, map[string]string{"status": "approved", "note": "Independent owned mixed pack"}, 200)
			public := decodeTEST022Data[modpackResponse](t, f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+pack.SiteID, nil, 200))
			if len(public.Mods) != 2 || public.PublishedRevisionID == nil || *public.PublishedRevisionID != pack.SubmissionRevisionID {
				t.Fatal("review/public projection does not match exact submitted mixed revision")
			}
			missing := createModRequest{SiteID: "future-source-" + f.modCode, PrimaryName: "Future source now indexed", DefaultLocale: "en-US", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Future source now indexed", Summary: "Controlled future source declaration", ContentMarkdown: "Owned source publication"}}, Environment: "bothRequired", PrimaryCategory: "utility", ModIDs: []modIdentifierPayload{{Identifier: "future_" + f.modCode, Primary: true}}, Compatibilities: []modLoaderCompatibilityPayload{{Loader: "forge", Versions: []string{"1.20.1"}}}, OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual", ModrinthProjectID: future}
			created := decodeTEST022Data[modResponse](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/mods", missing, 201))
			if created.ReviewStatus != "pending" {
				t.Fatal("new source Mod bypassed independent review")
			}
			var stillPending bool
			if err := f.db.QueryRow(f.ctx, `select exists(select 1 from modpack_mods entry join unresolved_references u on u.source_type='modpack_mod' and u.source_id=entry.id where entry.modpack_id=(select id from modpacks where public_id=$1) and entry.mod_id is null and u.status='pending')`, pack.PublicID).Scan(&stillPending); err != nil || !stillPending {
				t.Fatalf("pending source resolved too early: %v", err)
			}
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/mods/"+created.SiteID+"/revisions/"+created.SubmissionRevisionID, map[string]string{"status": "approved", "note": "Independent new source registration"}, 200)
			auditResumeRequireUnresolved(t, f, pack, future, true, created.PublicID)
			after := decodeTEST022Data[modpackResponse](t, f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+pack.SiteID, nil, 200))
			resolved := false
			for _, m := range after.Mods {
				if m.ProviderProjectID == future {
					resolved = m.Resolved && m.ModPublicID == created.PublicID && m.ProviderVersionID == "future-release" && m.FileName == "future.jar"
				}
			}
			if !resolved || after.PublishedRevisionID == nil || *after.PublishedRevisionID != pack.SubmissionRevisionID {
				t.Fatal("later normal source approval lost reference identity or republished pack")
			}
			t.Logf("CMP007_REAL_POSTCONDITIONS mode=%s known1/unresolved1 duplicate_removed=true ordinary_pending=true independent_public=true future_source_pending_not_resolved=true future_source_independently_approved_resolves=true no_fake_mod=true", mode)
		})
	}
}

func TestAuditResumeCMP007AssociationFailureRollsBackAndRecoversHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	auditResumeModpackReviewPolicy(t, f)
	future := "rollback-" + f.modCode
	draft := auditResumeManualPack(f, "rollback-pack-"+f.modCode, future)
	if _, err := f.db.Exec(f.ctx, `alter table unresolved_references add constraint audit_cmp007_projection_failure check(raw_identifier not like 'modrinth:rollback-%') not valid`); err != nil {
		t.Fatal(err)
	}
	before := auditResumeModpackFacts(t, f)
	response := f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", draft, 500)
	if !strings.Contains(string(response), "failed to save modpack relations") {
		t.Fatal("fault did not reach precise mixed relation boundary")
	}
	auditResumeRequireFacts(t, before, auditResumeModpackFacts(t, f))
	if _, err := f.db.Exec(f.ctx, `alter table unresolved_references drop constraint audit_cmp007_projection_failure`); err != nil {
		t.Fatal(err)
	}
	created := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", draft, 201))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+created.SubmissionRevisionID, map[string]string{"status": "approved", "note": "Independent recovered relation transaction"}, 200)
	auditResumeRequireUnresolved(t, f, created, future, false, "")
	f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+created.SiteID, nil, 200)
	t.Log("CMP007_FAULT actual relation constraint, failed500 exact business error, complete business/outbox snapshots unchanged; same normal payload recovered201/independent200/public200")
}

func auditResumeRasterFile(t *testing.T, f auditFixGRFixture, actor, project string) string {
	t.Helper()
	source := test039PNG(t, 64, 64, 73)
	digest := sha256.Sum256(source)
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, actor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", map[string]any{"originalName": "mixed-pack.png", "contentType": "image/png", "source": "mod_resource:audit-gr-" + project + ":item:icon", "category": "mod_icon", "sizeBytes": len(source), "sha256": hex.EncodeToString(digest[:])}, 200))
	request, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range ticket.Headers {
		request.Header.Set(key, value)
	}
	response, err := f.provider.Client().Do(request)
	if err != nil {
		t.Fatal("controlled raster PUT failed")
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("rasterPUT status=%d", response.StatusCode)
	}
	file := decodeTEST022Data[struct {
		ID         string `json:"id"`
		ScanStatus string `json:"scanStatus"`
	}](t, f.require(t, actor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	if file.ID == "" || file.ScanStatus != "clean" {
		t.Fatalf("actual scanned image premise unmet: idPresent=%t scan=%s", file.ID != "", file.ScanStatus)
	}
	return file.ID
}
func TestAuditResumeCMP007ForeignProjectGalleryRejectedWithoutMixedReferenceWritesIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	auditResumeModpackReviewPolicy(t, f)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+f.otherModCode)
	foreign := auditResumeRasterFile(t, f, f.otherEditor, f.otherModCode)
	draft := auditResumeManualPack(f, "foreign-gallery-"+f.modCode, "foreign-future-"+f.modCode)
	draft.GalleryImages = []modGalleryImagePayload{{FileID: foreign}}
	before := auditResumeModpackFacts(t, f)
	response := f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", draft, 400)
	if !strings.Contains(string(response), "gallery image") {
		t.Fatal("foreign valid image did not reject at gallery authority boundary")
	}
	auditResumeRequireFacts(t, before, auditResumeModpackFacts(t, f))
	// The same scanned file is lawful for its actual owner, without admin.
	draft.SiteID = "owned-gallery-" + f.modCode
	pack := decodeTEST022Data[modpackResponse](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/modpacks", draft, 201))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+pack.SubmissionRevisionID, map[string]string{"status": "approved", "note": "Independent legitimate gallery owner"}, 200)
	published := decodeTEST022Data[modpackResponse](t, f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+pack.SiteID, nil, 200))
	if len(published.GalleryImages) != 1 || published.GalleryImages[0].FileID != foreign {
		t.Fatal("legitimate scanned-file owner publication lost exact image identity")
	}
	// File ownership is insufficient to move an existing pack's gallery into
	// another pack, whether the caller supplies only its file or its public
	// gallery identity. The same ordinary actor must still be refused.
	for _, tc := range []struct{ name, message string }{{"bound-file", "gallery image is already used"}, {"foreign-gallery-identity", "gallery image reference is invalid"}} {
		draft.SiteID = "cross-pack-" + tc.name + "-" + f.modCode
		draft.GalleryImages = []modGalleryImagePayload{{FileID: foreign}}
		if tc.name == "foreign-gallery-identity" {
			draft.GalleryImages[0].PublicID = published.GalleryImages[0].PublicID
			if draft.GalleryImages[0].PublicID == "" {
				t.Fatal("existing source gallery identity prerequisite missing")
			}
		}
		before = auditResumeModpackFacts(t, f)
		refused := f.require(t, f.otherEditor, http.MethodPost, "/api/v1/modpacks", draft, 400)
		if !strings.Contains(string(refused), tc.message) {
			t.Fatalf("cross-pack %s was not refused by exact binding guard", tc.name)
		}
		auditResumeRequireFacts(t, before, auditResumeModpackFacts(t, f))
	}
	t.Log("CMP007_FOREIGN_FILE actual scanned64x64PNG; same-project editor of source is not uploader; foreign400 zero mixed rows/outbox; actual owner201 independent200 public exactfile")
}
