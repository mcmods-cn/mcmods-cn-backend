package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These three supported ingredient contracts are synthetic. No modified JSON
// here is evidence of an actual Minecraft/JEI gas or fluid export.
func auditRecipeGeometryZIP(t *testing.T) []byte {
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
	categories := documents["recipes/jei/categories.json"]
	collection := documents["recipes/jei/recipes/example/pressing.json"]
	sourceRecipe, err := json.Marshal(collection["recipes"].([]any)[0])
	if err != nil {
		t.Fatal(err)
	}
	sourceSummary, err := json.Marshal(categories["recipes"].([]any)[0])
	if err != nil {
		t.Fatal(err)
	}
	recipes, summaries := []any{}, []any{}
	for _, input := range []struct {
		kind, ingredientType, resource, field string
		amount                                int
	}{
		{"item", "minecraft:item_stack", "example:geometry_item", "count", 3},
		{"fluid", "net.minecraftforge.fluids.FluidStack", "minecraft:water", "amount", 750},
		{"gas", "mekanism.api.chemical.gas.GasStack", "mekanism:hydrogen", "amount", 1000},
	} {
		var recipe, summary map[string]any
		if err := json.Unmarshal(sourceRecipe, &recipe); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(sourceSummary, &summary); err != nil {
			t.Fatal(err)
		}
		id := "example:geometry_" + input.kind
		recipe["recipe_id"], recipe["recipe_key"] = id, id
		summary["recipe_id"], summary["recipe_key"] = id, id
		output := recipe["bindings"].([]any)[1].(map[string]any)
		alternative := map[string]any{"type": input.ingredientType, input.field: input.amount}
		if input.kind == "item" {
			alternative["item"] = input.resource
		} else {
			alternative["resource_location"] = input.resource
		}
		output["alternatives"] = []any{alternative}
		recipes, summaries = append(recipes, recipe), append(summaries, summary)
	}
	collection["count"], collection["recipes"] = 3, recipes
	categories["recipe_count"], categories["recipes"] = 3, summaries
	category := categories["categories"].([]any)[0].(map[string]any)
	for _, field := range []string{"source_recipe_count", "recipe_count", "exported_recipe_count"} {
		category[field] = 3
	}
	filter := categories["filter_summary"].(map[string]any)
	filter["discovered_recipe_presentations"], filter["exported_recipe_presentations"] = 3, 3
	for path, document := range documents {
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(raw)
	}
	return auditFixGRZip(t, files)
}

func TestAuditLiveRecipeGeometryFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_RECIPE_GEOMETRY") != "1" {
		t.Skip("opt-in owned synthetic geometry browser fixture")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	top, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	checkouts := auditLiveSourceCheckouts(strings.TrimSpace(string(top)), "")
	revisions := checkouts.revisions(t)
	directory := os.Getenv("MCMODS_AUDIT_RECIPE_GEOMETRY_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("fresh absolute output directory required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-recipe-geometry pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	frontendOrigin := os.Getenv("MCMODS_AUDIT_FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "http://127.0.0.1:3361"
	}
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	unlock := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlock() })
	f := newAuditFixGRFixture(t, 35*time.Minute)
	unlock()
	t.Cleanup(func() { unlock = auditGlobalCatalogPGLock(t) })
	config := f.db.Config()
	config.MaxConns, config.MinConns = 12, 1
	db, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	cfg := f.server.cfg
	cfg.FrontendOrigin, cfg.NATS.Enabled = frontendOrigin, false
	f.origin.Close()
	api := NewServer(f.ctx, cfg, db, nil, nil, nil, nil)
	origin := httptest.NewServer(api)
	t.Cleanup(func() {
		origin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := api.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		api.cache.Close()
	})
	f.db, f.server, f.origin = db, api, origin
	raw := auditRecipeGeometryZIP(t)
	withAssets := os.Getenv("MCMODS_AUDIT_GEOMETRY_WITH_ASSETS") == "1"
	if withAssets {
		raw = auditRecipeGeometryAssetZIP(t)
	}
	digest := sha256.Sum256(raw)
	if err := os.WriteFile(filepath.Join(directory, "synthetic-jei-v7-three-kinds.zip"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	job, revision := f.imported(t, raw)
	f.reviewGR(t, revision, "approved")
	cases := []map[string]any{}
	for _, expected := range []struct {
		name, kind string
		amount     int
	}{{"item", "item", 3}, {"fluid", "fluid", 750}, {"gas", "mekanism_gas", 1000}} {
		var publicID, typeID, kind string
		var amount float64
		if err := db.QueryRow(f.ctx, `select entity.public_id,parent.public_id,candidate.ingredient_kind,candidate.amount
			from recipe_import_snapshots snapshot join recipes recipe on recipe.entity_id=snapshot.recipe_id
			join catalog_entities entity on entity.id=recipe.entity_id join catalog_entities parent on parent.id=recipe.recipe_type_id
			join recipe_import_bindings binding on binding.recipe_snapshot_id=snapshot.id
			join recipe_import_binding_candidates candidate on candidate.binding_id=binding.id
			where snapshot.revision_id=$1 and recipe.canonical_source_id=$2`, revision, "example:geometry_"+expected.name).Scan(&publicID, &typeID, &kind, &amount); err != nil {
			t.Fatal(err)
		}
		if kind != expected.kind || amount != float64(expected.amount) {
			t.Fatal("Worker changed the supported ingredient kind or amount")
		}
		render := f.require(t, "", http.MethodGet, "/api/v1/catalog/recipes/"+publicID+"/render", nil, 200)
		auditJourneyWriteJSON(t, filepath.Join(directory, "render-"+expected.name+".json"), json.RawMessage(render))
		cases = append(cases, map[string]any{"recipePublicId": publicID, "typePublicId": typeID, "kind": kind, "amount": amount, "source": "synthetic supported v7 contract; not game output", "expectedOutputRect": map[string]int{"x": 80, "y": 20, "width": 16, "height": 16}, "expectedOutputVisualRect": map[string]int{"x": 78, "y": 18, "width": 20, "height": 20}})
	}
	var schema string
	if err := db.QueryRow(f.ctx, `select current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	runtime := map[string]any{"owner": "mcmods-live-recipe-geometry", "pid": os.Getpid(), "state": "READY", "createdAt": time.Now().UTC().Format(time.RFC3339Nano), "apiOrigin": origin.URL, "frontendOrigin": frontendOrigin, "repoRevisions": revisions, "ownedSchema": schema, "gr010": map[string]any{"cases": cases}, "sampleSHA256": hex.EncodeToString(digest[:]), "jobId": job, "revisionId": revision, "creatorInternalId": f.userIDs[f.editor], "reviewerInternalId": f.userIDs[f.reviewer], "layer": "ACTUAL_HTTP_POSTGRESQL_DIRECT_BUSINESS_WORKER_INDEPENDENT_REVIEW_SYNTHETIC_CONTRACT; browser separate; no actual queue/game claim", "stopPath": filepath.Join(directory, "STOP")}
	runtime["assetsIncluded"] = withAssets
	if withAssets {
		runtime["renderAssets"] = auditGeometryAssetExpectations()
	}
	auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
	t.Logf("RECIPE_GEOMETRY_READY runtime=%s; independent ordinary creator/reviewer, no browser/game/queue PASS inferred", filepath.Join(directory, "runtime.json"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal("owned geometry fixture expired before STOP")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(directory, "STOP")); err == nil {
				if withAssets && os.Getenv("MCMODS_AUDIT_GEOMETRY_VERIFY_WITHDRAWAL") == "1" {
					auditGeometryIndependentWithdrawalSeal(t, f, revision, directory)
					runtime["independentWithdrawalVerified"] = true
				}
				runtime["state"], runtime["stoppedAt"] = "STOPPED", time.Now().UTC().Format(time.RFC3339Nano)
				auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}
