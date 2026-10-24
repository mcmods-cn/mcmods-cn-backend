package httpapi

// Captured, unmodified Minecraft/Fabric/JEI bytes go through mounted HTTP and
// PostgreSQL plus the business Worker called directly. This is separate from
// the browser/real-JetStream journey. Foreign template observations below are
// controlled DB fixtures for the canonical projection ownership predicate.
import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestAuditRecipeScopeActualGameHTTPWorkerAndIndependentReviewIntegration(t *testing.T) {
	auditRecipeScopeHTTPWorkerAndReview(t, "auditfixture-minecraft1201-fabric-jei1520-v7.zip", "059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb")
}

func TestAuditRecipeScopeOriginalFailedJourneyArchiveHTTPWorkerIntegration(t *testing.T) {
	auditRecipeScopeHTTPWorkerAndReview(t, "auditfixture-original-journey-fabric-v7.zip", "ef5f535f6f47d44d4f7866199c5cfc4c9a5f2b3295d5f1e38902bbd005dd1607")
}

func auditRecipeScopeHTTPWorkerAndReview(t *testing.T, name, expectedSHA256 string) {
	t.Helper()
	f := newAuditFixGRFixture(t)
	if _, err := f.db.Exec(f.ctx, `update mod_identifiers set identifier='auditfixture' where mod_id=$1 and identifier='example'`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update mod_content_versions set label='1.0.0',minecraft_versions=ARRAY['1.20.1'],loaders=ARRAY['fabric'] where id=$1`, f.versionID); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/audit-20261024/" + name)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		t.Fatal("original game archive bytes changed")
	}
	t.Logf("original_game_archive_sha256=%s", expectedSHA256)
	// The test helper's client filename is arbitrary. Its uploaded bytes and
	// computed content hash are the exact recorded game archive above.
	job, revision := f.imported(t, raw)
	if n := f.number(t, `select count(*) from catalog_import_jobs where id=$1 and status='ready'`, job); n != 1 {
		t.Fatal("actual full export did not reach ready")
	}
	if n := f.number(t, `select count(*) from recipe_import_snapshots snapshot join recipes recipe on recipe.entity_id=snapshot.recipe_id
 join recipe_types kind on kind.entity_id=recipe.recipe_type_id join catalog_import_revisions revision on revision.id=snapshot.revision_id
 where snapshot.revision_id=$1 and revision.mod_id=$2 and revision.source_namespace='auditfixture'
 and snapshot.source_mod_id='auditfixture' and kind.canonical_id='minecraft:crafting' and recipe.owner_mod_id=$2`, revision, f.modID); n != 2 {
		t.Fatalf("recipe owner/category projection count=%d", n)
	}
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision, nil, 403)
	f.require(t, f.editor, "PATCH", "/api/v1/admin/export-revisions/"+revision+"/activate", map[string]any{"status": "approved"}, 403)
	f.reviewGR(t, revision, "approved")
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision, nil, 200)
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets", nil, 200)
	if n := f.number(t, `select count(*) from recipe_import_binding_candidates candidate
 join recipe_import_bindings binding on binding.id=candidate.binding_id
 join recipe_import_snapshots snapshot on snapshot.id=binding.recipe_snapshot_id
 where snapshot.revision_id=$1 and candidate.amount=2`, revision); n != 1 {
		t.Fatalf("actual quantity-two output=%d", n)
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files, err := validateExportZIP(archive.File)
	if err != nil {
		t.Fatal(err)
	}
	templates, err := decodeExportJEITemplateCollection(auditRecipeScopeRead(t, files, "recipes/jei/templates/minecraft/crafting.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity := catalogEditorIdentityForTemplate(recipeTypeIdentity(templates.RecipeTypeID).ID, templates.Templates[0].TemplateID)
	var originalSnapshot, originalDefinition, originalSlots string
	slotProjection := `select coalesce(jsonb_agg(jsonb_build_object('key',slot_key,'role',role,'ordinal',ordinal,'x',x,'y',y,'width',width,'height',height,'definition',definition) order by slot_key),'[]'::jsonb)::text from recipe_template_slots where template_id=catalog_entity_internal_id($1)`
	if err = f.db.QueryRow(f.ctx, slotProjection, identity.ID).Scan(&originalSlots); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `select import_snapshot_id,definition::text from recipe_layout_templates where entity_id=catalog_entity_internal_id($1)`, identity.ID).Scan(&originalSnapshot, &originalDefinition); err != nil {
		t.Fatal(err)
	}
	// A different admitted source may retain its own observation of the same
	// technical template; it cannot replace the existing public canonical row.
	var packageID string
	if err = f.db.QueryRow(f.ctx, `select package_id from catalog_import_revisions where id=$1`, revision).Scan(&packageID); err != nil {
		t.Fatal(err)
	}
	var otherVersion int64
	if err = f.db.QueryRow(f.ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status) values($1,'Controlled other source',ARRAY['1.20.1'],ARRAY['fabric'],'active') returning id`, f.otherModID).Scan(&otherVersion); err != nil {
		t.Fatal(err)
	}
	otherJob, otherRevision := newExportID(), newExportID()
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status,created_by) values($1,$2,$3,$4,$5,'ready',$6)`, otherJob, f.otherModID, packageID, otherVersion, modExportImporterVersion, f.userIDs[f.otherEditor]); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, `insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active)
 values($1,$2,$3,$4,$5,1,'ready','1.20.1','fabric','0.10.0','auditother',true)`, otherRevision, f.otherModID, packageID, otherJob, otherVersion); err != nil {
		t.Fatal(err)
	}
	document := auditRecipeScopeDocument(t, files)
	document.Recipes = nil
	if err = importExportRecipeTypes(f.ctx, tx, packageID, map[string]string{"minecraft": otherRevision}, auditRecipeScopeRaw(t, document)); err != nil {
		t.Fatal(err)
	}
	changed := templates
	changed.Templates = append([]exportJEITemplate(nil), templates.Templates...)
	changed.Templates[0].Slots = append([]exportJEITemplateSlot(nil), templates.Templates[0].Slots...)
	var rect map[string]float64
	if err = json.Unmarshal(changed.Templates[0].Slots[0].Rect, &rect); err != nil {
		t.Fatal(err)
	}
	rect["x"]++
	changed.Templates[0].Slots[0].Rect, _ = json.Marshal(rect)
	batch := newModExportWriteBatch()
	if err = queueExportJEITemplateCollection(batch, map[string]string{"minecraft": otherRevision}, "recipes/jei/templates/minecraft/crafting.json", changed); err != nil {
		t.Fatal(err)
	}
	if err = batch.flush(f.ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	var snapshot, definition string
	if err = f.db.QueryRow(f.ctx, `select import_snapshot_id,definition::text from recipe_layout_templates where entity_id=catalog_entity_internal_id($1)`, identity.ID).Scan(&snapshot, &definition); err != nil {
		t.Fatal(err)
	}
	if snapshot != originalSnapshot || definition != originalDefinition {
		t.Fatal("a different source replaced the approved canonical template")
	}
	var slots string
	if err = f.db.QueryRow(f.ctx, slotProjection, identity.ID).Scan(&slots); err != nil {
		t.Fatal(err)
	}
	if slots != originalSlots {
		t.Fatal("a different source replaced canonical slot geometry or definitions")
	}
	if n := f.number(t, `select count(*) from recipe_template_import_snapshots where revision_id=$1`, otherRevision); n != 1 {
		t.Fatal("foreign source's independent observation was discarded")
	}
	// Replaying an active source's own observation is idempotent and retains its
	// established canonical state, rather than disabling imports wholesale.
	tx, err = f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	var active bool
	if err = tx.QueryRow(f.ctx, `select is_active from catalog_import_revisions where id=$1`, revision).Scan(&active); err != nil || !active {
		t.Fatal("first source lost active status")
	}
	if err = promoteImportedRecipeTemplatesTx(f.ctx, tx, revision); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.number(t, `select count(*) from recipe_template_slots where template_id=catalog_entity_internal_id($1)`, identity.ID); n != 10 {
		t.Fatalf("stable canonical slots=%d", n)
	}
}
