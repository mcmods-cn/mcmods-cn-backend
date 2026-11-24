package httpapi

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// These are synthetic negative contract packages, never relabelled game output.
// The input structure is the unchanged v7 producer golden; each case changes
// only the named business value/reference. All five must reach their own oracle.
func auditRecipeNegativePackage(t *testing.T, variant string) []byte {
	t.Helper()
	files := map[string]string{}
	documents := map[string]map[string]any{}
	for path, name := range map[string]string{
		"recipes/jei/categories.json":                 "jei-categories-v7.json",
		"recipes/jei/templates/example/pressing.json": "jei-templates-v2.json",
		"recipes/jei/recipes/example/pressing.json":   "jei-recipes-v2.json",
	} {
		raw, err := os.ReadFile("testdata/audit-20261024/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		documents[path] = document
	}
	recipe := documents["recipes/jei/recipes/example/pressing.json"]["recipes"].([]any)[0].(map[string]any)
	bindings := recipe["bindings"].([]any)
	output := bindings[1].(map[string]any)
	alternative := output["alternatives"].([]any)[0].(map[string]any)
	switch variant {
	case "valid":
	case "v7-quantities", "v6-implicit-quantity":
		unknown := map[string]any{"type": "fluid_stack", "resource_location": "example:fluid"}
		values := []any{unknown}
		if variant == "v7-quantities" {
			values = append(values, map[string]any{"type": "fluid_stack", "resource_location": "example:empty", "amount": 0})
		} else {
			header := documents["recipes/jei/categories.json"]
			header["schema_version"] = "mcmods-jei-categories/v6"
			cat := header["categories"].([]any)[0].(map[string]any)
			delete(cat, "export_mode")
			delete(cat, "recipes_omitted")
		}
		output["alternatives"] = values
	case "valid-second":
		documents["recipes/jei/categories.json"]["categories"].([]any)[0].(map[string]any)["title_names"] = map[string]any{"en_us": "Second synthetic pressing import"}
	case "negative-count":
		alternative["count"] = -7
	case "text-amount":
		delete(alternative, "count")
		delete(alternative, "item")
		alternative["type"] = "net.minecraftforge.fluids.FluidStack"
		alternative["resource_location"] = "minecraft:water"
		alternative["amount"] = "many"
	case "inconsistent-chance":
		output["chance_available"] = true
		output["chance"], output["chance_percent"] = .25, 90
	case "missing-template":
		recipe["template_id"] = "template_1111111111111111"
		documents["recipes/jei/categories.json"]["recipes"].([]any)[0].(map[string]any)["template_id"] = recipe["template_id"]
	case "missing-slot":
		output["slot_id"] = "output_99"
	default:
		t.Fatalf("unknown test variant %q", variant)
	}
	for path, document := range documents {
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(raw)
	}
	return auditFixGRZip(t, files)
}

func TestAuditRecipeNegativeCasesReachExactValidationAndPreservePublishedDataIntegration(t *testing.T) {
	for _, tc := range []struct{ variant, exactMessage, constraint string }{
		{"negative-count", "count must be a non-negative finite quantity within the exact numeric range", ""},
		{"text-amount", "amount must be a non-negative finite quantity within the exact numeric range", ""},
		{"inconsistent-chance", "chance and chance_percent must describe the same probability", ""},
		{"missing-template", "", "recipe_import_snapshots_template_id_fkey"},
		{"missing-slot", "", "recipe_import_bindings_template_slot_id_fkey"},
	} {
		t.Run(tc.variant, func(t *testing.T) {
			// Each counterexample has its own ordinary actor and existing public
			// baseline. This is not an upload-rate benchmark; retain the actual
			// production limiter and avoid combining five users' cases in a burst.
			f := newAuditFixGRFixture(t)
			_, published := f.imported(t, auditRecipeNegativePackage(t, "valid"))
			f.reviewGR(t, published, "approved")
			var recipePublicID string
			if err := f.db.QueryRow(f.ctx, `select entity.public_id from recipe_import_snapshots snapshot join catalog_entities entity on entity.id=snapshot.recipe_id where snapshot.revision_id=$1`, published).Scan(&recipePublicID); err != nil {
				t.Fatal(err)
			}
			renderPath := "/api/v1/catalog/recipes/" + recipePublicID + "/render"
			f.require(t, "", "GET", renderPath, nil, 200)
			beforeDataset := f.number(t, `select version from catalog_dataset_state where singleton`)
			beforeSnapshots := f.number(t, `select count(*) from recipe_import_snapshots`)
			beforeDetails := f.number(t, `select count(*) from mod_resource_version_details where import_revision_id=$1 and status='active'`, published)
			if beforeDetails == 0 {
				t.Fatal("positive precondition did not publish a resource projection")
			}
			job := f.job(t, f.upload(t, auditRecipeNegativePackage(t, tc.variant)))
			err := f.server.importMCModsExportJob(f.ctx, job.ID)
			if tc.constraint != "" {
				var pgError *pgconn.PgError
				if !errors.As(err, &pgError) || pgError.Code != "23503" || pgError.ConstraintName != tc.constraint {
					t.Fatalf("target reference rejection not reached: want SQLSTATE23503/%s, got %v", tc.constraint, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.exactMessage) {
				t.Fatalf("target numeric rejection not reached: want %q, got %v", tc.exactMessage, err)
			}
			var state, code string
			if err := f.db.QueryRow(f.ctx, `select status,error_code from catalog_import_jobs where id=$1`, job.ID).Scan(&state, &code); err != nil {
				t.Fatal(err)
			}
			if state != "failed" || code != "import_failed" {
				t.Fatalf("wrong failure state %s/%s", state, code)
			}
			if left := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1`, job.ID); left != 0 {
				t.Fatalf("failed job retained %d staged revisions", left)
			}
			if left := f.number(t, `select count(*) from catalog_import_job_artifacts where job_id=$1`, job.ID); left != 0 {
				t.Fatalf("failed job retained %d derived artifacts", left)
			}
			if f.number(t, `select count(*) from recipe_import_snapshots`) != beforeSnapshots ||
				f.number(t, `select version from catalog_dataset_state where singleton`) != beforeDataset ||
				f.number(t, `select count(*) from catalog_import_revisions where id=$1 and is_active`, published) != 1 ||
				f.number(t, `select count(*) from mod_resource_version_details where import_revision_id=$1 and status='active'`, published) != beforeDetails {
				t.Fatal("failed import changed existing published projection or dataset version")
			}
			f.require(t, "", "GET", renderPath, nil, 200)
			t.Logf("exact_oracle variant=%s constraint=%s status=failed/import_failed revisions=0 artifacts=0 previous_publication=unchanged", tc.variant, tc.constraint)
		})
	}
}
