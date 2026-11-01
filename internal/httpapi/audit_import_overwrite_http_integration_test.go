package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Keep the existing purpose-specific temporary-table scope matrix. This adds
// the original mounted HTTP/actual Worker path and persisted job option, using
// synthetic registry input and explicitly seeded historical human edits.
func TestAuditGR003ActualHTTPWorkerOverwritePreservesHumanAndMissingEntriesIntegration(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("overwrite=%v", overwrite), func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			initial := auditFixGRZip(t, map[string]string{"registries/items.json": `{"registry":"items","entries":[{"id":"example:keep","namespace":"example","names":{"en-US":"Imported old"},"max_stack_size":64},{"id":"example:removed","namespace":"example","names":{"en-US":"Removed from next package"},"max_stack_size":32},{"id":"example:manual","namespace":"example","names":{"en-US":"Initial manual target"},"max_stack_size":16}]}`})
			_, first := f.imported(t, initial)
			f.reviewGR(t, first, "approved")
			// Historical edits are the fixture precondition, not a claim that this
			// test exercised the manual authoring editor or its independent review.
			if _, err := f.db.Exec(f.ctx, `update mod_resource_version_detail_localizations localization
				set name='Human name',summary='Human summary',content_markdown='Human body',provenance='human'
				from game_resources resource where resource.entity_id=localization.resource_id
				and resource.canonical_id='example:keep' and localization.version_id=$1`, f.versionID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec(f.ctx, `update mod_resource_version_details detail set projection_source='manual',import_source_namespace='',import_source_kind='',import_revision_id='',definition='{"maxStackSize":16}'::jsonb
				from game_resources resource where resource.entity_id=detail.resource_id and resource.canonical_id='example:manual' and detail.version_id=$1`, f.versionID); err != nil {
				t.Fatal(err)
			}
			next := auditFixGRZip(t, map[string]string{"registries/items.json": `{"registry":"items","entries":[{"id":"example:keep","namespace":"example","names":{"en-US":"Imported new"},"max_stack_size":32,"enchantment_value":7},{"id":"example:added","namespace":"example","names":{"en-US":"New entry"},"max_stack_size":8},{"id":"example:manual","namespace":"example","names":{"en-US":"Imported manual replacement"},"max_stack_size":64}]}`})
			file := f.upload(t, next)
			job := decodeTEST022Data[modExportJobResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/mods/audit-gr-"+f.modCode+"/export-imports", map[string]any{"ossFileId": file, "targetVersionPublicId": f.versionPublic, "overwriteExistingImportData": overwrite}, 202))
			var stored bool
			if err := f.db.QueryRow(f.ctx, `select overwrite_existing from catalog_import_jobs where id=$1`, job.ID).Scan(&stored); err != nil || stored != overwrite {
				t.Fatalf("real HTTP option was not persisted: %v/%v", stored, err)
			}
			if err := f.server.importMCModsExportJob(f.ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			var second string
			if err := f.db.QueryRow(f.ctx, `select id from catalog_import_revisions where job_id=$1 and status='ready' and not is_active`, job.ID).Scan(&second); err != nil {
				t.Fatal(err)
			}
			f.reviewGR(t, second, "approved")
			wantStack := float64(64)
			wantRemoved, removedHTTP := "active", 200
			if overwrite {
				wantStack, wantRemoved, removedHTTP = 32, "archived", 404
			}
			for _, expected := range []struct {
				id, status, source string
				stack              float64
			}{{"keep", "active", "import", wantStack}, {"removed", wantRemoved, "import", 32}, {"added", "active", "import", 8}, {"manual", "active", "manual", 16}} {
				var definition []byte
				var publicID, status, source string
				if err := f.db.QueryRow(f.ctx, `select entity.public_id,detail.status,detail.projection_source,detail.definition
					from mod_resource_version_details detail join game_resources resource on resource.entity_id=detail.resource_id
					join catalog_entities entity on entity.id=resource.entity_id where resource.canonical_id=$1 and detail.version_id=$2`, "example:"+expected.id, f.versionID).Scan(&publicID, &status, &source, &definition); err != nil {
					t.Fatal(err)
				}
				var values map[string]any
				if err := json.Unmarshal(definition, &values); err != nil {
					t.Fatal(err)
				}
				if status != expected.status || source != expected.source || values["maxStackSize"] != expected.stack {
					t.Fatalf("actual projection %s status/source=%s/%s definition=%s", expected.id, status, source, definition)
				}
				code := 200
				if expected.id == "removed" {
					code = removedHTTP
				}
				f.require(t, "", http.MethodGet, "/api/v1/mods/audit-gr-"+f.modCode+"/content-resources/"+publicID, nil, code)
				if expected.id == "keep" {
					if values["enchantability"] != float64(7) {
						t.Fatal("incremental mode discarded a newly supplied registered field")
					}
					var name, summary, body, provenance string
					if err := f.db.QueryRow(f.ctx, `select localization.name,localization.summary,localization.content_markdown,localization.provenance
						from mod_resource_version_detail_localizations localization join game_resources resource on resource.entity_id=localization.resource_id
						where resource.canonical_id='example:keep' and localization.version_id=$1`, f.versionID).Scan(&name, &summary, &body, &provenance); err != nil {
						t.Fatal(err)
					}
					if name != "Human name" || summary != "Human summary" || body != "Human body" || provenance != "human" {
						t.Fatal("import replaced a human localization")
					}
				}
			}
			t.Logf("real persisted HTTP option=%v; actual Worker/independent review preserves human text/manual technical fields; imported change/new field/new entry/missing entry verified in DB and anonymous project API", overwrite)
		})
	}
}
