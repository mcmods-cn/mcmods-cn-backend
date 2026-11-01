package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

// Configure the real permission APIs instead of injecting an artificial rule.
// project.review.* is a scoped wildcard, retaining self rejection and an exact
// deny's priority. It must discover the same objects it may already approve.
func TestAuditGR004RealConfiguredWildcardScopePreservesDenyAndSelfHTTPPostgresIntegration(t *testing.T) {
	live := os.Getenv("MCMODS_RUN_LIVE_IMPORT_REVIEW") == "1"
	duration := 150 * time.Second
	unlock := func() {}
	if live {
		duration = 35 * time.Minute
		t.Setenv("FRONTEND_ORIGIN", os.Getenv("MCMODS_AUDIT_FRONTEND_ORIGIN"))
		unlock = auditGlobalCatalogPGLock(t)
		t.Cleanup(func() { unlock() })
	}
	f := newAuditFixGRFixture(t, duration)
	if live {
		unlock()
		t.Cleanup(func() { unlock = auditGlobalCatalogPGLock(t) })
	}
	_, first := f.imported(t, auditFixGRZip(t, nil))
	other := f
	other.editor = f.otherEditor
	other.modID = f.otherModID
	other.modCode = f.otherModCode
	if _, err := f.db.Exec(f.ctx, `insert into mod_identifiers(mod_id,identifier,is_primary) values($1,'otherexample',true)`, other.modID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status) values($1,'Other queue project',ARRAY['1.20.1'],ARRAY['forge'],'active') returning id,public_id`, other.modID).Scan(&other.versionID, &other.versionPublic); err != nil {
		t.Fatal(err)
	}
	_, second := other.imported(t, auditFixGRZip(t, map[string]string{
		"manifest.json":         `{"schema_version":"mcmods-export/v1","exporter_version":"audit-synthetic-legacy-v6","status":"complete","minecraft_version":"1.20.1","loader":"forge","package_format":"zip","configuration":{"namespaces":["otherexample"],"languages":["en-US"],"profile":"all"},"stages":[],"errors":[],"error_count":0}`,
		"registries/items.json": `{"registry":"items","entries":[{"id":"otherexample:audit_item","namespace":"otherexample","names":{"en-US":"Other audit item"},"max_stack_size":64}]}`,
	}))
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "permission.write")
	reviewerID, reviewerPublic, reviewer := createTEST044User(t, f.ctx, f.db, f.server.cfg, "wildcard-review-"+randomCatalogPublicID(), "UTC")
	if _, err := f.db.Exec(f.ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, reviewerID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/permissions", map[string]any{"code": "project.review.*", "module": "project", "name": "Configured scoped project reviews", "description": "Task-owned effective wildcard regression"}, 200)
	f.require(t, f.reviewer, http.MethodPut, "/api/v1/admin/users/"+reviewerPublic+"/permissions", map[string]any{"permissions": []map[string]any{{"code": "project.review.*", "allow": true, "source": "manual"}, {"code": "project.review." + f.otherModCode, "allow": false, "source": "manual"}}}, 200)
	allowed := f.require(t, reviewer, http.MethodGet, "/api/v1/export-revisions/"+first, nil, 200)
	var detail struct {
		Data struct {
			CanReview bool `json:"canReview"`
		} `json:"data"`
	}
	if err := json.Unmarshal(allowed, &detail); err != nil || !detail.Data.CanReview {
		t.Fatal("real configured wildcard is not current decision authority")
	}
	f.require(t, reviewer, http.MethodGet, "/api/v1/export-revisions/"+second, nil, 403)
	raw := f.require(t, reviewer, http.MethodGet, "/api/v1/admin/mod-content-reviews?category=catalog_import&limit=100", nil, 200)
	var page auditGR004QueueEnvelope
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	auditGR004RequireQueueTotals(t, page, 1)
	if page.Data.Items[0].ID != first || page.Data.Items[0].ReviewerScope != "project" {
		t.Fatal("wildcard queue leaked denied project or misrepresented scoped review")
	}
	auditGR004MaybeHoldBrowser(t, f, reviewer, first, second)
	before := auditImportActivityFacts(t, f.test013Fixture)
	f.require(t, reviewer, http.MethodPatch, "/api/v1/admin/export-revisions/"+second+"/activate", map[string]string{"status": "approved"}, 403)
	auditFixRequireProjectFactsUnchanged(t, before, auditImportActivityFacts(t, f.test013Fixture))
	var authorPublic string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&authorPublic); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodPut, "/api/v1/admin/users/"+authorPublic+"/permissions", map[string]any{"permissions": []map[string]any{{"code": "project.edit." + f.modCode, "allow": true, "source": "manual"}, {"code": "project.review.*", "allow": true, "source": "manual"}, {"code": "project.review." + f.otherModCode, "allow": false, "source": "manual"}}}, 200)
	raw = f.require(t, f.editor, http.MethodGet, "/api/v1/admin/mod-content-reviews?category=catalog_import&limit=100", nil, 200)
	page = auditGR004QueueEnvelope{}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	auditGR004RequireQueueTotals(t, page, 0)
	raw = f.require(t, f.editor, http.MethodGet, "/api/v1/export-revisions/"+first, nil, 200)
	detail = struct {
		Data struct {
			CanReview bool `json:"canReview"`
		} `json:"data"`
	}{}
	if err := json.Unmarshal(raw, &detail); err != nil || detail.Data.CanReview {
		t.Fatal("scoped wildcard gained self-review")
	}
	before = auditImportActivityFacts(t, f.test013Fixture)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/admin/export-revisions/"+first+"/activate", map[string]string{"status": "approved"}, 403)
	auditFixRequireProjectFactsUnchanged(t, before, auditImportActivityFacts(t, f.test013Fixture))
	f.require(t, reviewer, http.MethodPatch, "/api/v1/admin/export-revisions/"+first+"/activate", map[string]string{"status": "approved"}, 200)
	if f.number(t, `select count(*) from catalog_import_revisions where id=$1 and is_active`, first) != 1 || f.number(t, `select count(*) from catalog_import_revisions where id=$1 and is_active`, second) != 0 {
		t.Fatal("configured wildcard changed wrong project publication")
	}
	t.Log("actual permissionPOST200+manualGrantPUT200; effective project.review.* with exact deny; queue/detail/action coherent and self-review remains denied, all counts/facets exact")
}
