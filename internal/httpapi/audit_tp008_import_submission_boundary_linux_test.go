package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
)

// This closes TP-C08's import-creation guard assertions only. The eleven actual
// Worker/native journeys remain their distinct recorded positive execution.
func TestAuditTP008ImportJobCreationAndElevenRevokedSourceGuardsHTTPIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || os.Getenv("MCMODS_RUN_TP008_IMPORT_BOUNDARY") != "1" {
		t.Skip("opt-in actual TP-C08 HTTP/PostgreSQL task-creation guard scope")
	}
	f := newAuditTP008NegativeFixture(t)
	f.report["layer"] = "ACTUAL_HTTP_POSTGRESQL_IMPORT_JOB_CREATION_GUARDS_ONLY_NO_WORKER_OR_NETWORK_FETCH"
	f.report["ratePlan"] = "Same fixed actor: one allowed import POST plus eleven denied source-combination POSTs, exactly the original write.generic12/60s allowance. No other actor write, retry, multiplier or IP switch; no business review.submit actions."
	var providerRequests atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerRequests.Add(1)
		http.Error(w, "this task-creation-only fixture never fetches metadata", 503)
	}))
	defer func() { provider.CloseClientConnections(); provider.Close() }()
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL, cfg.Modrinth.Token = provider.URL, ""
	cfg.CurseForge.BaseURL, cfg.CurseForge.APIKey, cfg.CurseForge.Enabled = "https://api.curseforge.com/v1", "tp008-c08-declared-synthetic-key", true
	cfg.GitHub.Token = ""
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	for _, kind := range simpleProjectTypeValues() {
		f.permission(t, kind, true)
	}
	queued := auditTP008NegativeData[modMetadataImportJobResponse](t, f.request(t, "revoked", http.MethodPost, "/api/v1/content-project-imports/plugin", map[string]string{"provider": "modrinth", "url": "https://modrinth.com/plugin/tp008-c08-" + f.nonce}, 202))
	var jobs, projects, revisions, outbox int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from mod_metadata_import_jobs where public_id=$1 and user_id=$2 and project_type='plugin' and provider='modrinth' and status='queued' and result is null),(select count(*) from simple_projects),(select count(*) from content_revisions),(select count(*) from nats_outbox where aggregate_type='mod_metadata_import_job' and aggregate_id=$1)`, queued.ID, f.ids["revoked"]).Scan(&jobs, &projects, &revisions, &outbox); err != nil || queued.ID == "" || queued.Status != "queued" || jobs != 1 || projects != 0 || revisions != 0 || outbox != 1 {
		t.Fatalf("normal task creation must create only one queued job/outbox and no direct project/revision: jobs=%d projects=%d revisions=%d outbox=%d err=%v", jobs, projects, revisions, outbox, err)
	}
	f.report["normalImportOnlyPrecondition"] = map[string]any{"jobId": queued.ID, "status": queued.Status, "owner": f.publicIDs["revoked"], "actualJobRows": jobs, "directProjects": projects, "directRevisions": revisions, "durableTaskOutbox": outbox, "providerRequests": providerRequests.Load(), "queueDelivery": "NOT_RUN; disabled dispatcher/Worker, only durable creation intent is verified"}
	beforeRead := f.importBoundaryFacts(t)
	f.request(t, "revoked", http.MethodGet, "/api/v1/content-project-imports/plugin/"+queued.ID, nil, 200)
	f.request(t, "foreign", http.MethodGet, "/api/v1/content-project-imports/plugin/"+queued.ID, nil, 404)
	f.request(t, "", http.MethodGet, "/api/v1/content-project-imports/plugin/"+queued.ID, nil, 401)
	f.request(t, "revoked", http.MethodGet, "/api/v1/content-project-imports/map/"+queued.ID, nil, 404)
	if !reflect.DeepEqual(beforeRead, f.importBoundaryFacts(t)) {
		t.Fatal("queued task read isolation mutated business or task facts")
	}
	var oldPermission, oldAuth, newPermission, newAuth int64
	if err := f.db.QueryRow(f.ctx, `select permission_version,auth_version from users where id=$1`, f.ids["revoked"]).Scan(&oldPermission, &oldAuth); err != nil {
		t.Fatal(err)
	}
	f.request(t, "governor", http.MethodPut, "/api/v1/admin/users/"+f.publicIDs["revoked"]+"/permissions", updateUserPermissionsRequest{Permissions: []userPermissionEntry{}}, 200)
	f.request(t, "revoked", http.MethodGet, "/api/v1/auth/me", nil, 200)
	for _, kind := range simpleProjectTypeValues() {
		f.permission(t, kind, false)
	}
	stillOwned := auditTP008NegativeData[modMetadataImportJobResponse](t, f.request(t, "revoked", http.MethodGet, "/api/v1/content-project-imports/plugin/"+queued.ID, nil, 200))
	var audits int
	if err := f.db.QueryRow(f.ctx, `select permission_version,auth_version,(select count(*) from permission_audit_logs where operator_id=$2 and target_user_id=$1 and action='update_user_permissions') from users where id=$1`, f.ids["revoked"], f.ids["governor"]).Scan(&newPermission, &newAuth, &audits); err != nil || newPermission <= oldPermission || newAuth != oldAuth || audits != 1 || stillOwned.ID != queued.ID || stillOwned.Status != "queued" {
		t.Fatal("actual same-session revocation or existing owned queued-job contract was not established")
	}
	f.report["revocation"] = map[string]any{"actor": f.publicIDs["revoked"], "operator": f.publicIDs["governor"], "permissionVersionBefore": oldPermission, "permissionVersionAfter": newPermission, "authVersionUnchanged": newAuth == oldAuth, "auditCount": audits, "sameOriginalSessionAuthenticated": true, "existingOwnQueuedJobRemainsReadable": true, "allSixTypedCapabilitiesEvaluatedTrueThenFalse": true}
	for _, kind := range simpleProjectTypeValues() {
		spec := projectImportSourceSpecs[kind]
		for _, source := range []string{"modrinth", "curseforge"} {
			if source == "modrinth" && len(spec.ModrinthSections) == 0 {
				continue
			}
			sourceURL := "https://www.curseforge.com/minecraft/" + spec.CurseForgeSections[0] + "/tp008-c08-" + f.nonce
			if source == "modrinth" {
				sourceURL = "https://modrinth.com/" + spec.ModrinthSections[0] + "/tp008-c08-" + f.nonce
			}
			if _, _, _, err := parseProjectImportSource(kind, source, sourceURL); err != nil {
				t.Fatalf("declared supported source payload invalid before authorization: %s/%s: %v", kind, source, err)
			}
			t.Run(kind+"_"+source, func(t *testing.T) {
				before := f.importBoundaryFacts(t)
				row := map[string]any{"name": "revoked_import_" + kind + "_" + source, "projectType": kind, "provider": source, "status": "RUNNING", "before": before}
				f.report["cases"] = append(f.report["cases"].([]map[string]any), row)
				f.write(t)
				raw := f.request(t, "revoked", http.MethodPost, "/api/v1/content-project-imports/"+kind, map[string]string{"provider": source, "url": sourceURL}, 403)
				var failure struct{ Error, Code string }
				if json.Unmarshal(raw, &failure) != nil || failure.Error != "project creation permission is required" || failure.Code != "HTTP_403" {
					t.Errorf("import denial must be the exact current create-permission boundary: code=%s message=%s", failure.Code, failure.Error)
				}
				after := f.importBoundaryFacts(t)
				row["after"], row["actualStatus"], row["exactMessage"], row["completeBusinessJobRowsAndAssetsUnchanged"] = after, 403, failure.Error, reflect.DeepEqual(before, after)
				if !reflect.DeepEqual(before, after) {
					t.Error("denied import creation changed durable task, project, revision, file, authorship, reward or outbox rows")
				}
				row["status"] = "PASS"
				if t.Failed() {
					row["status"] = "FAIL"
				}
				f.write(t)
			})
		}
	}
	if len(f.report["cases"].([]map[string]any)) != 11 || providerRequests.Load() != 0 {
		t.Fatalf("eleven current supported source guards and zero provider fetch required: cases=%d requests=%d", len(f.report["cases"].([]map[string]any)), providerRequests.Load())
	}
	f.report["providerRequests"], f.report["finishedBusinessFacts"] = providerRequests.Load(), f.importBoundaryFacts(t)
	f.report["sameOriginalRevokedSessionRemainsTaskOwner"] = "Current contract permits own queued result lookup after create capability removal; no fabricated GET403 policy is asserted."
	f.write(t)
}

func (f *auditTP008NegativeFixture) importBoundaryFacts(t *testing.T) map[string]any {
	t.Helper()
	facts := f.facts(t)
	var count int64
	var raw string
	if err := f.db.QueryRow(f.ctx, `select count(*),coalesce(jsonb_agg(to_jsonb(job) order by to_jsonb(job)::text),'[]'::jsonb)::text from mod_metadata_import_jobs job`).Scan(&count, &raw); err != nil {
		t.Fatal(fmt.Errorf("snapshot actual metadata task rows: %w", err))
	}
	facts["mod_metadata_import_jobs"] = map[string]any{"rows": count, "completeRowsSha256": sha256Hex([]byte(raw))}
	return facts
}
