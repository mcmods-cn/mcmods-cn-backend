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

// Actual HTTP/PG authoring and independent moderation prepare isolated data for
// an external native browser. Import Workers are called directly: this is not
// a new game export or a real queue journey. Never infer browser PASS from exit.
func TestAuditLiveRecipeCatalogFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_RECIPE_CATALOG") != "1" {
		t.Skip("opt-in owned recipe catalog fixture")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	top, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	revisions := map[string]any{}
	for name, path := range map[string]string{"mcmods-cn-backend": strings.TrimSpace(string(top)), "mcmods-cn-frontend": "/workspace/mcmods-cn-frontend", "mcmods_exporter": "/workspace/mcmods_exporter"} {
		if name == "mcmods-cn-frontend" && os.Getenv("MCMODS_AUDIT_FRONTEND_REPO") != "" {
			path = os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
		}
		r := auditJourneyRevision(path)
		if r["error"] != nil || r["dirty"] != false {
			t.Fatalf("clean checkout required: %s", name)
		}
		revisions[name] = r
	}
	if pin := os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT"); pin != "" && revisions["mcmods-cn-backend"].(map[string]any)["commit"] != pin {
		t.Fatal("wrong compiled backend pin")
	}
	directory := os.Getenv("MCMODS_AUDIT_RECIPE_CATALOG_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("fresh absolute output directory required")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-recipe-catalog pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	frontendOrigin := os.Getenv("MCMODS_AUDIT_FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "http://127.0.0.1:3341"
	}
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	unlock := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlock() })
	f := newAuditFixGRFixture(t, 45*time.Minute)
	unlock()
	t.Cleanup(func() { unlock = auditGlobalCatalogPGLock(t) })
	dbConfig := f.db.Config()
	dbConfig.MaxConns = 12
	dbConfig.MinConns = 1
	db, err := pgxpool.NewWithConfig(f.ctx, dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	// Only existing capabilities, declared before the live API is exposed.
	creatorID, _, creator := createTEST044User(t, f.ctx, db, f.server.cfg, "recipe-author-"+randomCatalogPublicID(), "UTC")
	grantTEST044Permissions(t, f.ctx, db, creatorID, "content.write", "global_resource.list", "global_resource.view", "user.draft.retention_seconds.2592000")
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.reviewer], "global_resource.list", "global_resource.view", "project.review")
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.denied], "global_resource.list", "global_resource.view")
	if _, err = db.Exec(f.ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, creatorID); err != nil {
		t.Fatal(err)
	}
	review := defaultReviewConfig()
	review.CatalogCreate = true
	review.CatalogEdit = true
	review.CatalogDelete = true
	reviewRaw, _ := json.Marshal(review)
	if _, err = db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, reviewRaw); err != nil {
		t.Fatal(err)
	}
	cfg := f.server.cfg
	cfg.FrontendOrigin = frontendOrigin
	cfg.NATS.Enabled = false
	f.origin.Close()
	api := NewServer(f.ctx, cfg, db, nil, nil, nil, nil)
	origin := httptest.NewServer(api)
	t.Cleanup(func() {
		origin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := api.Shutdown(ctx); e != nil {
			t.Error(e)
		}
		api.cache.Close()
	})
	f.server, f.origin, f.db = api, origin, db
	if _, err = db.Exec(f.ctx, `update mod_identifiers set identifier='auditfixture' where mod_id=$1 and identifier='example'`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(f.ctx, `update mod_content_versions set label='1.0.0',minecraft_versions=ARRAY['1.20.1'],loaders=ARRAY['fabric'] where id=$1`, f.versionID); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/audit-20261024/auditfixture-minecraft1201-fabric-jei1520-v7.zip")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	sampleSHA := hex.EncodeToString(digest[:])
	if sampleSHA != "059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb" {
		t.Fatal("raw actual game package changed")
	}
	gameJob, gameRevision := f.imported(t, raw)
	f.reviewGR(t, gameRevision, "approved")
	// A separate project and unmodified v7 contract golden isolate the mixed
	// group from the unchanged real-game imported-only category.
	other := f
	other.editor = f.otherEditor
	other.modID = f.otherModID
	other.modCode = f.otherModCode
	if _, err = db.Exec(f.ctx, `insert into mod_identifiers(mod_id,identifier,is_primary) values($1,'example',true)`, other.modID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(f.ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status,created_by,updated_by) values($1,'Synthetic pressing contract',ARRAY['1.20.1'],ARRAY['forge'],'active',$2,$2) returning id,public_id`, other.modID, f.userIDs[f.otherEditor]).Scan(&other.versionID, &other.versionPublic); err != nil {
		t.Fatal(err)
	}
	overrides := map[string]string{}
	for path, name := range map[string]string{"recipes/jei/categories.json": "jei-categories-v7.json", "recipes/jei/templates/example/pressing.json": "jei-templates-v2.json", "recipes/jei/recipes/example/pressing.json": "jei-recipes-v2.json"} {
		b, e := os.ReadFile("testdata/audit-20261024/" + name)
		if e != nil {
			t.Fatal(e)
		}
		overrides[path] = string(b)
	}
	contractBytes := auditFixGRZip(t, overrides)
	contractDigest := sha256.Sum256(contractBytes)
	contractJob, contractRevision := other.imported(t, contractBytes)
	other.reviewGR(t, contractRevision, "approved")
	type row struct {
		Recipe string `json:"recipePublicId"`
		Type   string `json:"typePublicId"`
	}
	findImported := func(revision string) row {
		t.Helper()
		var r row
		if e := db.QueryRow(f.ctx, `select entity.public_id,parent.public_id from recipe_import_snapshots snapshot join recipes recipe on recipe.entity_id=snapshot.recipe_id join catalog_entities entity on entity.id=recipe.entity_id join catalog_entities parent on parent.id=recipe.recipe_type_id where snapshot.revision_id=$1 order by entity.public_id limit 1`, revision).Scan(&r.Recipe, &r.Type); e != nil {
			t.Fatal(e)
		}
		return r
	}
	game := findImported(gameRevision)
	mixedImport := findImported(contractRevision)
	// Keep the unchanged ordinary3/60s review budget. A per-actor precondition
	// pacer waits before its fourth request, without raising any configured limit.
	type burst struct {
		start time.Time
		count int
	}
	bursts := map[string]*burst{}
	pace := func(actor string) {
		t.Helper()
		b := bursts[actor]
		if b == nil {
			b = &burst{start: time.Now()}
			bursts[actor] = b
		}
		if b.count >= 3 {
			remaining := time.Until(b.start.Add(61 * time.Second))
			if remaining > 0 {
				t.Log("recipe fixture waits for original per-actor review budget")
				timer := time.NewTimer(remaining)
				select {
				case <-timer.C:
				case <-f.ctx.Done():
					timer.Stop()
					t.Fatal(f.ctx.Err())
				}
			}
			b.start = time.Now()
			b.count = 0
		}
		b.count++
	}
	// Two import decisions above have already used this actor's review window.
	bursts[f.reviewer] = &burst{start: time.Now(), count: 2}
	authoring := []map[string]any{}
	submit := func(path, name string, payload map[string]any) catalogEditResult {
		t.Helper()
		pace(creator)
		payload["reason"] = "Ordinary synthetic recipe catalog precondition: " + name
		payload["defaultLocale"] = "en-US"
		payload["localizations"] = []map[string]string{{"locale": "en-US", "name": name, "summary": "Synthetic public recipe regression precondition"}}
		result := decodeTEST022Data[catalogEditResult](t, f.require(t, creator, http.MethodPost, path, payload, 202))
		if result.ReviewStatus != "pending" {
			t.Fatal("ordinary author unexpectedly bypassed review")
		}
		pace(f.reviewer)
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+result.RevisionID, map[string]string{"status": "approved", "note": "Independent recipe catalog precondition"}, 200)
		authoring = append(authoring, map[string]any{"subjectPublicId": result.ObjectPublicID, "revisionPublicId": result.RevisionID, "requestPublicId": result.ChangeRequestID, "path": path, "creatorInternalId": creatorID, "reviewerInternalId": f.userIDs[f.reviewer], "submittedStatus": "pending", "decision": "approved"})
		return result
	}
	resource := submit("/api/v1/catalog/resources", "Manual result fixture", map[string]any{"kindCode": "minecraft.item", "canonicalId": "audit_recipe:manual_result", "definition": map[string]any{}})
	parent := submit("/api/v1/recipe-types", "Manual-only recipe group", map[string]any{"canonicalId": "audit_recipe:manual_only", "definition": map[string]any{}})
	createTemplate := func(parentID, key, name string) catalogEditResult {
		return submit("/api/v1/recipe-types/"+parentID+"/templates", name, map[string]any{"templateKey": key, "canvas": map[string]any{"width": 100, "height": 50, "imageScale": 1}, "definition": map[string]any{}, "slots": []map[string]any{{"slotKey": "empty", "role": "input", "ordinal": 0, "rect": map[string]any{"x": 4, "y": 4, "width": 16, "height": 16}}, {"slotKey": "output", "role": "output", "ordinal": 1, "rect": map[string]any{"x": 40, "y": 10, "width": 16, "height": 16}}}})
	}
	manualTemplate := createTemplate(parent.ObjectPublicID, "manual-only-output", "Manual-only output layout")
	createRecipe := func(parentID, templateID, canonical, name string) catalogEditResult {
		return submit("/api/v1/recipe-types/"+parentID+"/recipes", name, map[string]any{"canonicalSourceId": canonical, "templatePublicId": templateID, "applicableVersionIds": []string{"1.20.1"}, "definition": map[string]any{}, "bindings": map[string]any{"output": map[string]any{"candidates": []map[string]any{{"resourcePublicId": resource.ObjectPublicID, "amount": 3, "probability": .5}}}}})
	}
	manual := createRecipe(parent.ObjectPublicID, manualTemplate.ObjectPublicID, "audit_recipe:manual_only_result", "Manual result three at half chance")
	mixedTemplate := createTemplate(mixedImport.Type, "mixed-manual-output", "Mixed manual output layout")
	mixedManual := createRecipe(mixedImport.Type, mixedTemplate.ObjectPublicID, "audit_recipe:mixed_manual_result", "Mixed result three at half chance")
	publicPaths := []string{"/api/v1/recipe-types/" + parent.ObjectPublicID + "/catalog", "/api/v1/recipe-types/" + game.Type + "/catalog", "/api/v1/recipe-types/" + mixedImport.Type + "/catalog", "/api/v1/catalog/recipes/" + manual.ObjectPublicID + "/render", "/api/v1/catalog/recipes/" + game.Recipe + "/render", "/api/v1/catalog/recipes/" + mixedManual.ObjectPublicID + "/render"}
	for _, path := range publicPaths {
		f.require(t, "", http.MethodGet, path, nil, 200)
	}
	var ownedSchema string
	if err = db.QueryRow(f.ctx, `select current_schema()`).Scan(&ownedSchema); err != nil {
		t.Fatal(err)
	}
	authFile := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authFile, map[string]any{"creatorToken": creator, "reviewerToken": f.reviewer, "readerToken": f.denied, "projectCreatorToken": f.editor})
	cont002 := map[string]any{"manualOnly": row{Recipe: manual.ObjectPublicID, Type: parent.ObjectPublicID}, "importedOnly": game, "mixed": map[string]any{"manualRecipePublicId": mixedManual.ObjectPublicID, "importedRecipePublicId": mixedImport.Recipe, "typePublicId": mixedImport.Type}, "expectedAmount": 3, "expectedProbability": .5, "manualTotal": 1, "importedTotal": 2, "mixedTotal": 2}
	runtime := map[string]any{"owner": "mcmods-live-recipe-catalog", "pid": os.Getpid(), "apiOrigin": origin.URL, "frontendOrigin": frontendOrigin, "repoRevisions": revisions, "ownedSchema": ownedSchema, "authFile": authFile, "cont002": cont002, "sampleSHA256": sampleSHA, "actualGameProducerCommit": "806214802c1e3447a9895fa5311e73f3be6edfcc", "gameJobId": gameJob, "gameRevisionId": gameRevision, "contractJobId": contractJob, "contractRevisionId": contractRevision, "syntheticContractSHA256": hex.EncodeToString(contractDigest[:]), "layer": "HTTP_POSTGRESQL_DIRECT_WORKER_AND_INDEPENDENT_REVIEW_PRECONDITIONS; external native browser not run; no real queue claim", "createdAt": time.Now().UTC().Format(time.RFC3339Nano), "stopPath": filepath.Join(directory, "STOP"), "gr010Item": map[string]any{"recipePublicId": game.Recipe, "kind": "minecraft.item", "source": "unchanged actual game059b", "expectedOutputRect": map[string]any{"x": 99, "y": 23, "width": 16, "height": 16}, "expectedOutputVisualRect": map[string]any{"x": -1, "y": -1, "width": 116, "height": 40}}}
	auditJourneyWriteJSON(t, filepath.Join(directory, "authoring-preconditions.json"), authoring)
	auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
	t.Logf("RECIPE_CATALOG_READY runtime=%s; tokens kept private, no browser or queue PASS inferred", filepath.Join(directory, "runtime.json"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			runtime["state"] = "EXPIRED"
			auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
			t.Fatal("owned recipe fixture expired before STOP")
		case <-ticker.C:
			if _, e := os.Stat(filepath.Join(directory, "STOP")); e == nil {
				runtime["state"] = "STOPPED"
				runtime["stoppedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
				auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
				return
			} else if !os.IsNotExist(e) {
				t.Fatal(e)
			}
		}
	}
}
