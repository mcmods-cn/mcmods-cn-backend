package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Each contender must reach the database aggregate lock before it is released;
// scheduling uses the PostgreSQL lock barrier, not a probabilistic sleep race.
func TestAuditFixRPH001ContentAggregateConcurrencyMatrix(t *testing.T) {
	for _, kind := range []string{"version", "resource", "template", "layout"} {
		for _, mode := range []string{"review-required", "review-disabled", "no-review"} {
			for _, writers := range []string{"two-tabs", "two-editors"} {
				t.Run(kind+"/"+mode+"/"+writers, func(t *testing.T) {
					f := newTEST013Fixture(t)
					var target modContentMutationResult
					var aggregate, path, method string
					var build func(string, int) any
					switch kind {
					case "version":
						target = f.version(t, f.editor, "test013-mod", "matrix version")
						aggregate = modContentAggregateVersion
						method = http.MethodPut
						path = "/api/v1/mods/test013-mod/content-versions/" + target.PublicID
						build = func(base string, i int) any {
							return modContentVersionEdit{BaseRevisionID: &base, MinecraftVersions: []string{"1.21.1"}, Loaders: []string{"neoforge"}, ModVersion: fmt.Sprintf("writer-%d", i), Reason: "matrix"}
						}
					case "resource":
						version, section, resource := auditRPHResource(t, f)
						target = resource
						aggregate = modContentAggregateResource
						method = http.MethodPut
						path = "/api/v1/mods/test013-mod/content-resources/" + target.PublicID
						build = func(base string, i int) any {
							return modContentResourceEdit{BaseRevisionID: &base, ResourcePublicID: target.PublicID, VersionPublicID: version.PublicID, SectionPublicID: &section.PublicID, EntryTypeCode: "default", DefaultLocale: "en-US", Definition: map[string]any{}, Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: fmt.Sprintf("writer-%d", i)}}, Reason: "matrix"}
						}
						aggregate += ":" + target.PublicID + ":" + version.PublicID
					case "template":
						edit := modContentTemplateEdit{Code: "auditmatrix", DefaultLocale: "en-US", DefaultDisplayMode: "compact", Definition: templateSchemaGuardMap(false, "number", false), Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Matrix template"}}, Reason: "matrix"}
						target = f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-templates", edit, 201)
						f.review(t, target, f.editor, "approved")
						aggregate = modContentAggregateTemplate
						method = http.MethodPut
						path = "/api/v1/mods/test013-mod/content-templates/" + target.PublicID
						build = func(base string, i int) any {
							proposal := edit
							proposal.BaseRevisionID = &base
							proposal.Localizations = []catalogLocalizationEdit{{Locale: "en-US", Name: fmt.Sprintf("writer-%d", i)}}
							return proposal
						}
					case "layout":
						version := f.version(t, f.editor, "test013-mod", "layout matrix")
						target = f.section(t, version.PublicID, f.builtin(t, "item_block"))
						aggregate = modContentAggregateSection
						method = http.MethodPatch
						path = "/api/v1/mods/test013-mod/content-sections/" + target.PublicID + "/layout"
						build = func(base string, i int) any {
							return modContentLayoutPatch{BaseRevisionID: &base, VersionPublicID: version.PublicID, RootSectionPublicID: target.PublicID, DisplayMode: "large", Resources: []modContentLayoutResourceEdit{}, Reason: fmt.Sprintf("writer-%d", i)}
						}
					}
					if kind != "resource" {
						aggregate += ":" + target.PublicID
					}
					grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+f.modCode)
					if mode == "no-review" {
						for _, token := range []string{f.editor, f.otherEditor} {
							grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "project.no-review."+f.modCode)
						}
					}
					if mode == "review-disabled" {
						cfg := defaultReviewConfig()
						cfg.CatalogEdit = false
						raw, _ := json.Marshal(cfg)
						if _, err := f.db.Exec(f.ctx, `update system_settings set value=$2::jsonb where key=$1`, reviewConfigSettingKey, raw); err != nil {
							t.Fatal(err)
						}
					}
					lock, err := f.db.Begin(f.ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer lock.Rollback(f.ctx)
					if _, err = lock.Exec(f.ctx, `select pg_advisory_xact_lock(hashtext($1))`, aggregate); err != nil {
						t.Fatal(err)
					}
					tokenB := f.editor
					if writers == "two-editors" {
						tokenB = f.otherEditor
					}
					a := auditCompletionAsyncJSON(f, f.editor, method, path, build(target.RevisionID, 1))
					b := auditCompletionAsyncJSON(f, tokenB, method, path, build(target.RevisionID, 2))
					auditCompletionAwaitLocks(t, f, 2)
					if err = lock.Commit(f.ctx); err != nil {
						t.Fatal(err)
					}
					ra, rb := <-a, <-b
					if ra.err != nil || rb.err != nil {
						t.Fatalf("transport errors %v/%v", ra.err, rb.err)
					}
					if !((ra.status == 200 && rb.status == 409) || (ra.status == 409 && rb.status == 200)) {
						t.Fatalf("same base statuses=%d/%d body=%s/%s", ra.status, rb.status, ra.raw, rb.raw)
					}
					var children int
					if err = f.db.QueryRow(f.ctx, `select count(*) from content_revisions where base_revision_id=(select id from content_revisions where public_id=$1)`, target.RevisionID).Scan(&children); err != nil || children != 1 {
						t.Fatalf("failed contender left revision: children=%d err=%v", children, err)
					}
					winnerRaw, winnerToken := ra.raw, f.editor
					if rb.status == 200 {
						winnerRaw, winnerToken = rb.raw, tokenB
					}
					var result struct {
						Data modContentMutationResult `json:"data"`
					}
					if err = json.Unmarshal(winnerRaw, &result); err != nil {
						t.Fatal(err)
					}
					if mode == "review-required" {
						f.review(t, result.Data, winnerToken, "approved")
					}
					// Reloading a new baseline permits a real retry; the rejected old base
					// remains rejected regardless of the account used for the retry.
					f.require(t, f.editor, method, path, build(target.RevisionID, 3), 409)
					f.require(t, f.editor, method, path, build(result.Data.RevisionID, 4), 200)
				})
			}
		}
	}
}
