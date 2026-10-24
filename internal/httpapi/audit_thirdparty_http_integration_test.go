package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"mcmods-cn-backend/internal/config"
	"strings"
	"testing"
)

func TestAuditCMP007MixedModpackPreservesUnresolvedThroughIndependentReviewHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
	review := defaultReviewConfig()
	review.ModpackCreate = true
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"manual", "modrinth"} {
		t.Run(source, func(t *testing.T) {
			draft := perf020ModpackSnapshot("audit-cmp007-"+source, 0)
			draft.PrimaryName = "Audit CMP007 " + source
			draft.SubmissionMethod = source
			draft.Mods = []modpackModPayload{
				{ModPublicID: f.modCode, Provider: "manual", ModName: "Known Mod", ClientRequired: true, ServerRequired: true},
				{Provider: "modrinth", ProviderProjectID: "not-yet-indexed", ProviderVersionID: "source-version", ModName: "Unindexed Mod", FileName: "unindexed.jar", ClientRequired: true},
			}
			var created struct {
				Data modpackResponse `json:"data"`
			}
			if err := json.Unmarshal(f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", draft, 201), &created); err != nil {
				t.Fatal(err)
			}
			pack := created.Data
			if pack.ReviewStatus != "pending" || pack.SubmissionRevisionID == "" || len(pack.Mods) != 2 {
				t.Fatalf("pending draft missing facts: %#v", pack)
			}
			var unresolved int
			var metadata string
			if err := f.db.QueryRow(f.ctx, `select count(*),coalesce(min(ref.metadata::text),'') from unresolved_references ref join modpack_mods entry on entry.id=ref.source_id join modpacks pack on pack.id=entry.modpack_id where ref.source_type='modpack_mod' and pack.public_id=$1 and entry.mod_id is null`, pack.PublicID).Scan(&unresolved, &metadata); err != nil {
				t.Fatal(err)
			}
			if unresolved != 1 || !strings.Contains(metadata, `"projectId": "not-yet-indexed"`) || !strings.Contains(metadata, `"fileName": "unindexed.jar"`) {
				t.Fatalf("unresolved=%d metadata=%s", unresolved, metadata)
			}
			var fake int
			if err := f.db.QueryRow(f.ctx, `select count(*) from mods where modrinth_project_id='not-yet-indexed'`).Scan(&fake); err != nil || fake != 0 {
				t.Fatalf("fake Mod rows=%d err=%v", fake, err)
			}
			f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+pack.SiteID, nil, 404)
			f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", draft, 409)
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+pack.SubmissionRevisionID, map[string]string{"status": "approved", "note": "independent CMP007 regression"}, 200)
			public := string(f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+pack.SiteID, nil, 200))
			if !strings.Contains(public, "Unindexed Mod") || !strings.Contains(public, "not-yet-indexed") {
				t.Fatalf("published unresolved source lost: %s", public)
			}
			var packs, entries, refs int
			if err := f.db.QueryRow(f.ctx, `select (select count(*) from modpacks where public_id=$1),(select count(*) from modpack_mods where modpack_id=(select id from modpacks where public_id=$1)),(select count(*) from unresolved_references r join modpack_mods e on e.id=r.source_id where r.source_type='modpack_mod' and e.modpack_id=(select id from modpacks where public_id=$1))`, pack.PublicID).Scan(&packs, &entries, &refs); err != nil {
				t.Fatal(err)
			}
			if packs != 1 || entries != 2 || refs != 1 {
				t.Fatalf("non-idempotent final facts: packs=%d entries=%d unresolved=%d", packs, entries, refs)
			}
		})
	}
}

func TestAuditTP001StoredImportResultRechecksPublicSourceAndOwnershipHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "project.create")
	}
	var status atomic.Value
	status.Store("approved")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := status.Load().(string)
		if state == "outage" {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "source", "status": state})
	}))
	defer provider.Close()
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = provider.URL
	serverCfg := config.Load()
	serverCfg.SettingsEncryptionKey = "test013-only-settings-key-at-least-32-characters"
	settings := &Server{cfg: serverCfg, db: f.db}
	sealed, err := settings.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err = f.db.QueryRow(f.ctx, `insert into mod_metadata_import_jobs(user_id,provider,source_url,status,progress,result) values($1,'modrinth','https://modrinth.com/mod/source','completed',100,'{"bodyMarkdown":"SYNTHETIC_SOURCE_DOCUMENT"}') returning public_id`, f.userIDs[f.editor]).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/mod-imports/" + jobID
	if raw := string(f.require(t, f.editor, http.MethodGet, path, nil, 200)); !strings.Contains(raw, "SYNTHETIC_SOURCE_DOCUMENT") {
		t.Fatal("public source result missing")
	}
	f.require(t, f.otherEditor, http.MethodGet, path, nil, 404)
	for _, state := range []string{"private", "draft", "rejected", "unknown", "outage"} {
		status.Store(state)
		raw := string(f.require(t, f.editor, http.MethodGet, path, nil, 503))
		if strings.Contains(raw, "SYNTHETIC_SOURCE_DOCUMENT") {
			t.Fatalf("%s source leaked saved result", state)
		}
	}
	status.Store("approved")
	f.require(t, f.editor, http.MethodGet, path, nil, 200)
	var unchanged bool
	if err = f.db.QueryRow(f.ctx, `select status='completed' and progress=100 and result->>'bodyMarkdown'='SYNTHETIC_SOURCE_DOCUMENT' from mod_metadata_import_jobs where public_id=$1`, jobID).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("read gate mutated stored job: unchanged=%v err=%v", unchanged, err)
	}
}
