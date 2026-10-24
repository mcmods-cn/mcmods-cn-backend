package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

// Eleven provider combinations use real controlled-provider HTTP and the real
// business API/PostgreSQL/review/publication paths. Five Modrinth combinations
// also traverse import jobs and explicitly invoked Worker execution. CurseForge
// local origins cannot accept credentials under production configuration policy,
// so those six invoke the importer only; no CF Worker, queue or browser claim.
func TestAuditTP008ElevenSourceCombinationsProviderHTTPToIndependentPublicationIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	types := []string{"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"}
	for _, kind := range types {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create."+kind)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
	cfClasses := map[string]int{}
	for i, kind := range types {
		for _, section := range projectImportSourceSpecs[kind].CurseForgeSections {
			if _, ok := cfClasses[section]; !ok {
				cfClasses[section] = 100 + i
			}
		}
	}
	references := map[string]string{}
	for _, kind := range types {
		for _, provider := range []string{"modrinth", "curseforge"} {
			references["tp008-"+strings.ReplaceAll(kind, "_", "-")+"-"+provider] = kind
		}
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
			t.Error("configured public fixture received provider credentials")
		}
		if strings.HasPrefix(r.URL.Path, "/project/") {
			reference := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/project/"), "/version")
			if strings.HasSuffix(r.URL.Path, "/version") {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			kind := references[reference]
			spec := projectImportSourceSpecs[kind]
			if len(spec.ModrinthSections) == 0 {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": reference, "slug": reference, "title": "TP008 " + kind, "description": "Synthetic controlled-provider sample", "body": "TP008 source body " + reference, "project_type": spec.ModrinthSections[0], "status": "approved", "game_versions": []string{"1.20.1"}, "loaders": []string{"paper", "iris"}, "license": map[string]string{"id": "MIT"}})
			return
		}
		switch r.URL.Path {
		case "/categories":
			classes := []map[string]any{}
			for section, id := range cfClasses {
				classes = append(classes, map[string]any{"id": id, "slug": section, "isClass": true})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": classes})
		case "/mods/search":
			reference := r.URL.Query().Get("slug")
			classID, _ := strconv.Atoi(r.URL.Query().Get("classId"))
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": classID, "classId": classID, "slug": reference, "name": "TP008 " + references[reference], "summary": "Synthetic controlled-provider sample", "isAvailable": true, "latestFiles": []map[string]any{{"gameVersions": []string{"1.20.1", "Paper", "Iris"}}}, "links": map[string]string{"websiteUrl": "https://www.curseforge.com/minecraft/source/" + reference}}}})
		default:
			if strings.HasSuffix(r.URL.Path, "/description") {
				_ = json.NewEncoder(w).Encode(map[string]string{"data": "<p>TP008 synthetic source body</p>"})
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = provider.URL
	cfg.CurseForge.BaseURL = provider.URL
	cfg.CurseForge.APIKey = ""
	cfg.CurseForge.Enabled = false
	serverCfg := config.Load()
	serverCfg.SettingsEncryptionKey = "test013-only-settings-key-at-least-32-characters"
	workerServer := &Server{db: f.db, cfg: serverCfg}
	sealed, err := workerServer.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	executed := 0
	for _, kind := range types {
		for _, source := range []string{"modrinth", "curseforge"} {
			spec := projectImportSourceSpecs[kind]
			if source == "modrinth" && len(spec.ModrinthSections) == 0 {
				continue
			}
			t.Run(kind+"/"+source, func(t *testing.T) {
				executed++
				sample := "tp008-" + strings.ReplaceAll(kind, "_", "-") + "-" + source
				sourceURL := "https://www.curseforge.com/minecraft/" + spec.CurseForgeSections[0] + "/" + sample
				if source == "modrinth" {
					sourceURL = "https://modrinth.com/" + spec.ModrinthSections[0] + "/" + sample
				}
				importPath := "/api/v1/content-project-imports/" + kind
				var completed struct {
					Data modMetadataImportJobResponse `json:"data"`
				}
				jobID := "NOT_RUN_CF_CREDENTIAL_GATE"
				if source == "modrinth" {
					var queued struct {
						Data modMetadataImportJobResponse `json:"data"`
					}
					if err := json.Unmarshal(f.require(t, f.editor, http.MethodPost, importPath, map[string]string{"provider": source, "url": sourceURL}, 202), &queued); err != nil {
						t.Fatal(err)
					}
					jobID = queued.Data.ID
					if err := workerServer.runModMetadataImport(f.ctx, jobID); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(f.require(t, f.editor, http.MethodGet, importPath+"/"+jobID, nil, 200), &completed); err != nil {
						t.Fatal(err)
					}
					if completed.Data.Status != "completed" {
						t.Fatalf("Worker did not complete: %#v", completed.Data)
					}
				} else {
					// This directly exercises the supported importer with an
					// uncredentialed controlled HTTP source. It neither changes
					// provider configuration rules nor proves the CF Worker gate.
					imported, err := importCurseForgeSimpleProject(f.ctx, provider.Client(), cfg, kind, sourceURL, sample)
					if err != nil {
						t.Fatal(err)
					}
					if err = normalizeAndValidateSimpleProjectDraft(&imported, true); err != nil {
						t.Fatal(err)
					}
					raw, err := json.Marshal(imported)
					if err != nil {
						t.Fatal(err)
					}
					completed.Data.Result = raw
				}
				var fields map[string]any
				if err := json.Unmarshal(completed.Data.Result, &fields); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{"galleryImages", "parentProjects", "authors", "links", "minecraftVersions", "loaders", "categories", "features", "searchKeywords"} {
					if _, ok := fields[key].([]any); !ok {
						t.Fatalf("%s is not an array in actual persisted result: %s", key, completed.Data.Result)
					}
				}
				var draft simpleProjectSnapshot
				if err := json.Unmarshal(completed.Data.Result, &draft); err != nil {
					t.Fatal(err)
				}
				if kind == "addon" {
					draft.ParentProjects = []simpleProjectParent{{Type: "mod", PublicID: f.modCode}}
				}
				draft.Localizations[0].Name += " edited"
				var created struct {
					Data simpleProjectResponse `json:"data"`
				}
				businessPath := "/api/v1/content-projects/" + kind
				if err := json.Unmarshal(f.require(t, f.editor, http.MethodPost, businessPath, draft, 201), &created); err != nil {
					t.Fatal(err)
				}
				project := created.Data
				if project.ReviewStatus != "pending" || project.SubmissionRevisionID == "" {
					t.Fatalf("creation bypassed review: %#v", project)
				}
				f.require(t, "", http.MethodGet, businessPath+"/"+project.SiteID, nil, 404)
				f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+project.SubmissionRevisionID, map[string]string{"status": "approved", "note": "TP008 independent controlled-provider regression"}, 200)
				public := f.require(t, "", http.MethodGet, businessPath+"/"+project.SiteID, nil, 200)
				if !strings.Contains(string(public), "edited") {
					t.Fatal("published edit missing")
				}
				var published bool
				if err := f.db.QueryRow(f.ctx, `select project.review_status='approved' and project.published_revision_id=revision.id and request.status='approved' from simple_projects project join content_revisions revision on revision.public_id=$2 join change_requests request on request.proposed_revision_id=revision.id where project.public_id=$1`, project.PublicID, project.SubmissionRevisionID).Scan(&published); err != nil || !published {
					t.Fatalf("published pointers inconsistent: %v %v", published, err)
				}
				digest := sha256.Sum256(completed.Data.Result)
				t.Logf("sample=%s source-result-sha256=%s job=%s revision=%s project=%s roles=ordinary-creator,independent-reviewer,anonymous", sample, hex.EncodeToString(digest[:]), jobID, project.SubmissionRevisionID, project.PublicID)
				if destination := os.Getenv("MCMODS_AUDIT_SAMPLE_DIR"); destination != "" {
					if err := os.MkdirAll(destination, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(destination, sample+".json"), completed.Data.Result, 0600); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
	if executed != 11 {
		t.Fatalf("supported source combinations executed=%d want=11", executed)
	}
	var jobs, projects int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from mod_metadata_import_jobs where user_id=$1 and status='completed'),(select count(*) from simple_projects where submitted_by=$1 and review_status='approved')`, f.userIDs[f.editor]).Scan(&jobs, &projects); err != nil {
		t.Fatal(err)
	}
	if jobs != 5 || projects != 11 {
		t.Fatal(fmt.Sprintf("completed jobs=%d published projects=%d", jobs, projects))
	}
}
