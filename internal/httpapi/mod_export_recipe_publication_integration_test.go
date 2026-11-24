package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"testing"
)

// These ZIPs adapt the checked-in producer contract fixtures to exercise two
// supported observations. They are independent of loader/game acceptance.
func recipeObservationPackage(t *testing.T, indexOnly bool) []byte {
	t.Helper()
	files := map[string]string{
		"registries/items.json": `{"registry":"items","entries":[{"id":"example:plate","namespace":"example","names":{"en-US":"Plate"}}]}`,
	}
	for path, fixture := range map[string]string{
		"recipes/jei/categories.json":                 "jei-categories-v7.json",
		"recipes/jei/templates/example/pressing.json": "jei-templates-v2.json",
		"recipes/jei/recipes/example/pressing.json":   "jei-recipes-v2.json",
	} {
		raw, err := os.ReadFile("testdata/audit-20261024/" + fixture)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err = json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		switch fixture {
		case "jei-categories-v7.json":
			if indexOnly {
				row := doc["recipes"].([]any)[0].(map[string]any)
				row["layout_available"], row["background_available"], row["template_id"] = false, false, ""
				doc["layout_sample_count"] = 0
				doc["filter_summary"].(map[string]any)["layout_sample_presentations"] = 0
			}
		case "jei-templates-v2.json":
			slot := doc["templates"].([]any)[0].(map[string]any)["slots"].([]any)[1].(map[string]any)
			slot["role"] = "byproduct"
		case "jei-recipes-v2.json":
			if indexOnly {
				doc["count"], doc["recipes"] = 0, []any{}
			} else {
				slot := doc["recipes"].([]any)[0].(map[string]any)["bindings"].([]any)[1].(map[string]any)
				slot["semantic_role"], slot["byproduct"] = "byproduct", true
			}
		}
		raw, err = json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(raw)
	}
	return auditFixGRZip(t, files)
}

func TestPublishedByproductAppearsInResourceProductionRecipes(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, rev := f.imported(t, recipeObservationPackage(t, false))
	f.reviewGR(t, rev, "approved")
	raw := f.require(t, "", http.MethodGet, "/api/v1/export-revisions/"+rev+"/entry-detail?registry=items&objectId=example:plate", nil, http.StatusOK)
	type recipe struct {
		ID string `json:"recipeId"`
	}
	entry := decodeTEST022Data[struct {
		Recipes []recipe
		Uses    []recipe
	}](t, raw)
	if len(entry.Recipes) != 1 || entry.Recipes[0].ID != "example:plate" || len(entry.Uses) != 0 {
		t.Fatalf("byproduct recipes=%v uses=%v", entry.Recipes, entry.Uses)
	}
}

func TestImportedRecipeVersionsFollowPublicationIncludingMissingLayouts(t *testing.T) {
	for _, tc := range []struct {
		name, nextVersion, decision string
		indexOnly                   bool
		final                       []string
	}{
		{"rejected layout", "1.20.2", "rejected", false, []string{"1.20.1"}},
		{"approved index only", "1.20.3", "approved", true, []string{"1.20.1", "1.20.3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			_, rev := f.imported(t, auditRecipeNegativePackage(t, "valid"))
			f.reviewGR(t, rev, "approved")
			var recipePublic string
			if err := f.db.QueryRow(f.ctx, `select entity.public_id from recipe_import_snapshots snapshot join catalog_entities entity on entity.id=snapshot.recipe_id where snapshot.revision_id=$1`, rev).Scan(&recipePublic); err != nil {
				t.Fatal(err)
			}
			assertVersions := func(want ...string) {
				t.Helper()
				raw := f.require(t, "", http.MethodGet, "/api/v1/catalog/recipes/"+recipePublic, nil, http.StatusOK)
				result := decodeTEST022Data[struct{ ApplicableVersions []struct{ ID string } }](t, raw)
				got := []string{}
				for _, v := range result.ApplicableVersions {
					got = append(got, v.ID)
				}
				slices.Sort(got)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Errorf("public applicable versions=%v want=%v", got, want)
				}
			}
			assertVersions("1.20.1")
			if err := f.db.QueryRow(f.ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status) values($1,$2,array['1.20.1',$2],array['forge'],'active') returning public_id`, f.modID, tc.nextVersion).Scan(&f.versionPublic); err != nil {
				t.Fatal(err)
			}
			_, observed := f.imported(t, recipeObservationPackage(t, tc.indexOnly))
			assertVersions("1.20.1")
			f.reviewGR(t, observed, tc.decision)
			assertVersions(tc.final...)
		})
	}
}

func TestPublishedImportedRecipeCandidatesKeepTheirResourceIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolved bool
		bundle   func(*testing.T) []byte
	}{
		{"registry resource", true, func(t *testing.T) []byte { return recipeObservationPackage(t, false) }},
		{"reference only", false, func(t *testing.T) []byte { return auditRecipeNegativePackage(t, "valid") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			_, rev := f.imported(t, tc.bundle(t))
			f.reviewGR(t, rev, "approved")
			var recipe, resource string
			if err := f.db.QueryRow(f.ctx, `select r.public_id,e.public_id from recipe_import_snapshots s
				join catalog_entities r on r.id=s.recipe_id
				join recipe_import_bindings b on b.recipe_snapshot_id=s.id
				join recipe_import_binding_candidates c on c.binding_id=b.id
				join catalog_entities e on e.id=c.resource_id where s.revision_id=$1 and c.raw_resource_id='example:plate'`, rev).Scan(&recipe, &resource); err != nil {
				t.Fatal(err)
			}
			raw := f.require(t, "", http.MethodGet, "/api/v1/catalog/recipes/"+recipe, nil, http.StatusOK)
			out := decodeTEST022Data[struct {
				Bindings map[string]struct{ Candidates []map[string]any }
			}](t, raw)
			candidates := out.Bindings["output_0"].Candidates
			if len(candidates) != 1 {
				t.Fatalf("output candidates=%v", candidates)
			}
			c := candidates[0]
			if c["kindCode"] != "minecraft.item" || c["unresolved"] != !tc.resolved || c["amount"] != float64(2) || c["probability"] != nil {
				t.Fatalf("candidate properties=%+v", c)
			}
			if tc.resolved && (c["resourcePublicId"] != resource || c["canonicalId"] != "example:plate" || c["rawResourceId"] != "") {
				t.Fatalf("collected candidate=%+v", c)
			}
			if !tc.resolved && c["rawResourceId"] != "example:plate" {
				t.Fatalf("reference candidate=%+v", c)
			}
		})
	}
}
