package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

// API+PG+actual explicitly invoked Worker, controlled TLS public MR source.
// Native UI and actual JetStream are separately evidenced for the quests mixed
// sample; this table never promotes all six rows to browser/remote-vendor PASS.
func TestAuditResumeTP005CategoryProjectionActualWorkerHTTPPostgresIntegration(t *testing.T) {
	for _, tc := range []struct{ source, expected string }{{"quests", "quests"}, {"skyblock", "skyblock"}, {"tech", "technology"}, {"optimization", "optimization"}, {"building", "building"}, {"unknown", "adventure"}} {
		t.Run(tc.source, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			auditResumeModpackReviewPolicy(t, f)
			var archive bytes.Buffer
			zw := zip.NewWriter(&archive)
			entry, err := zw.Create("modrinth.index.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = json.NewEncoder(entry).Encode(map[string]any{"formatVersion": 1, "game": "minecraft", "versionId": "category-release", "name": "Controlled category source", "dependencies": map[string]string{"minecraft": "1.20.1", "forge": "47.4.21"}, "files": []any{}}); err != nil {
				t.Fatal(err)
			}
			if err = zw.Close(); err != nil {
				t.Fatal(err)
			}
			var downloads atomic.Int32
			var upstream *httptest.Server
			upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
					t.Error("uncredentialed controlled source received credentials")
				}
				switch r.URL.Path {
				case "/project/category-source":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "category-source", "slug": "category-" + tc.source, "title": "Owned category " + tc.source, "description": "Normal provider category preview " + tc.source, "body": "Independent category publication documentation " + tc.source, "project_type": "modpack", "status": "approved", "categories": []string{tc.source}, "client_side": "required", "server_side": "required", "license": map[string]string{"id": "MIT"}})
				case "/project/category-source/version":
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "category-release", "name": "Category release", "version_number": "1.0", "version_type": "release", "status": "listed", "date_published": "2026-10-01T00:00:00Z", "game_versions": []string{"1.20.1"}, "loaders": []string{"forge"}, "files": []map[string]any{{"url": upstream.URL + "/archive.mrpack", "filename": "category.mrpack", "primary": true}}}})
				case "/archive.mrpack":
					downloads.Add(1)
					_, _ = w.Write(archive.Bytes())
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(upstream.Close)
			original := http.DefaultTransport
			http.DefaultTransport = upstream.Client().Transport
			t.Cleanup(func() { http.DefaultTransport = original })
			auditResumeMetadataSettings(t, f, "modrinth", upstream.URL)
			job := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpack-imports", modMetadataImportRequest{Provider: "modrinth", URL: "https://modrinth.com/modpack/category-source"}, 202))
			if err = f.server.runModMetadataImport(f.ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			preview := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodGet, "/api/v1/modpack-imports/"+job.ID, nil, 200))
			if preview.Status != "completed" || preview.Progress != 100 || preview.Error != "" || downloads.Load() != 1 {
				t.Fatal("actual metadata job failed to produce one normal archive preview")
			}
			var draft createModpackRequest
			if err = json.Unmarshal(preview.Result, &draft); err != nil {
				t.Fatal(err)
			}
			if draft.PrimaryCategory != tc.expected {
				t.Fatalf("preview category=%s expected=%s", draft.PrimaryCategory, tc.expected)
			}
			// This is an explicit ordinary edit, preserving the provider category,
			// source, archive selection and all nonempty business fields.
			draft.DefaultLocale = "en-US"
			draft.BodyMarkdown += "\nA normal editor checked this category before submission."
			pending := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", draft, 201))
			if pending.ReviewStatus != "pending" || pending.SubmissionRevisionID == "" || pending.PrimaryCategory != tc.expected {
				t.Fatal("normal edit changed mapping or bypassed independent review")
			}
			f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+pending.SiteID, nil, 404)
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+pending.SubmissionRevisionID, map[string]string{"status": "approved", "note": "Independent category projection"}, 200)
			public := decodeTEST022Data[modpackResponse](t, f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+pending.SiteID, nil, 200))
			if public.PrimaryCategory != tc.expected || public.BodyMarkdown != draft.BodyMarkdown || !reflect.DeepEqual(public.Tags, pending.Tags) || public.PublishedRevisionID == nil || *public.PublishedRevisionID != pending.SubmissionRevisionID {
				t.Fatal("preview/edit/review/public category projection diverged")
			}
			if tc.source == "unknown" && len(public.Tags) != 0 {
				t.Fatal("unknown category fabricated a known source category")
			}
			var persisted string
			if err = f.db.QueryRow(f.ctx, `select primary_category from modpacks where public_id=$1`, public.PublicID).Scan(&persisted); err != nil || persisted != tc.expected {
				t.Fatalf("persisted category=%s error=%v", persisted, err)
			}
			t.Logf("TP005_CATEGORY source=%s preview/edited/pending/independent/public/persisted=%s; original unknown policy adventure fallback and no known tags retained", tc.source, tc.expected)
		})
	}
}
