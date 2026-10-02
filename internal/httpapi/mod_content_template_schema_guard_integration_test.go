package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestTemplateSchemaGuardDistinguishesDisableDeleteAndReferencesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify template schema reference guards")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	nonce := time.Now().UnixNano()
	var modID, versionID, templateID, unreferencedTemplateID, sectionID, resourceID int64
	var templatePublicID, unreferencedTemplatePublicID, sectionPublicID string
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,'Schema guard','approved') returning id`, randomCatalogPublicID(), fmt.Sprintf("schema-guard-%d", nonce)).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status)
		values($1,'Schema guard','active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	definition := `{"resourceKinds":["minecraft.item"],"entryTypes":[{"code":"machine","kindCodes":["minecraft.item"],"names":{"en-US":"Machine"},"groups":[{"code":"main","names":{"en-US":"Main"},"fields":[{"code":"power","type":"number","names":{"en-US":"Power"},"paths":[["power"]]}]}]}]}`
	if err = tx.QueryRow(ctx, `insert into mod_content_templates(owner_mod_id,code,definition,status)
		values($1,'machine',$2::jsonb,'active') returning id,public_id`, modID, definition).Scan(&templateID, &templatePublicID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_templates(owner_mod_id,code,definition,status)
		values($1,'unused_machine',$2::jsonb,'active') returning id,public_id`, modID, definition).Scan(&unreferencedTemplateID, &unreferencedTemplatePublicID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,display_mode,status)
		values($1,$2,$3,'compact','active') returning id,public_id`, modID, versionID, templateID).Scan(&sectionID, &sectionPublicID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		values($1,$2,'resource','active') returning id`, fmt.Sprintf("resource:schema-guard:%d", nonce), randomCatalogPublicID()).Scan(&resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		values($1,'minecraft.item',$2,'schema_guard','machine',$3,true)`, resourceID, fmt.Sprintf("schema_guard:machine_%d", nonce), modID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,entry_type_code,definition,status)
		values($1,$2,'machine','{"power":100}'::jsonb,'active')`, resourceID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal)
		values($1,$2,$3,0)`, sectionID, versionID, resourceID); err != nil {
		t.Fatal(err)
	}

	current := templateSchemaGuardFixture(false, "number")
	removed := modContentTemplateDefinition{ResourceKinds: []string{"minecraft.item"}}
	if err = validateModContentTemplateSchemaChangeTx(ctx, tx, templateID, current, removed); !errors.Is(err, errCatalogEditorReference) {
		t.Fatalf("referenced entry type deletion error=%v", err)
	}
	disabled := templateSchemaGuardFixture(true, "number")
	if err = validateModContentTemplateSchemaChangeTx(ctx, tx, templateID, current, disabled); err != nil {
		t.Fatalf("referenced entry type disable failed: %v", err)
	}
	changedStorage := templateSchemaGuardFixture(false, "text")
	if err = validateModContentTemplateSchemaChangeTx(ctx, tx, templateID, current, changedStorage); !errors.Is(err, errCatalogEditorInvalid) {
		t.Fatalf("storage type change error=%v", err)
	}
	if _, err = tx.Exec(ctx, `update mod_resource_version_details set status='archived'
		where resource_id=$1 and version_id=$2`, resourceID, versionID); err != nil {
		t.Fatal(err)
	}
	if err = validateModContentTemplateSchemaChangeTx(ctx, tx, templateID, current, removed); !errors.Is(err, errCatalogEditorReference) {
		t.Fatalf("historical referenced entry type deletion error=%v", err)
	}
	if err = validateModContentTemplateSchemaChangeTx(ctx, tx, unreferencedTemplateID, current, removed); err != nil {
		t.Fatalf("unreferenced entry type deletion failed: %v", err)
	}
	if err = validateModContentTemplateDeletionTx(ctx, tx, templateID); !errors.Is(err, errCatalogEditorReference) {
		t.Fatalf("referenced template deletion error=%v", err)
	}
	if err = validateModContentTemplateDeletionTx(ctx, tx, unreferencedTemplateID); err != nil {
		t.Fatalf("unreferenced template deletion failed: %v", err)
	}

	referencedEditRevisionID := insertTemplateSchemaGuardRevision(t, ctx, tx, "referenced-edit", 1)
	referencedEdit := modContentTemplateEdit{Code: "machine", DefaultLocale: "en-US", DefaultDisplayMode: "compact", Definition: templateSchemaGuardMap(false, "number", true)}
	if err = publishModContentSnapshotTx(ctx, tx, referencedEditRevisionID, modContentSnapshot{
		Kind: "template", Operation: "edit", ModID: modID, PublicID: templatePublicID, Template: &referencedEdit,
	}, 0); !errors.Is(err, errCatalogEditorReference) {
		t.Fatalf("custom publish removed a referenced entry type: %v", err)
	}
	disableRevisionID := insertTemplateSchemaGuardRevision(t, ctx, tx, "disable", 1)
	disableEdit := modContentTemplateEdit{Code: "machine", DefaultLocale: "en-US", DefaultDisplayMode: "compact", Definition: templateSchemaGuardMap(true, "number", false)}
	if err = publishModContentSnapshotTx(ctx, tx, disableRevisionID, modContentSnapshot{
		Kind: "template", Operation: "edit", ModID: modID, PublicID: templatePublicID, Template: &disableEdit,
	}, 0); err != nil {
		var storedDefinition []byte
		_ = tx.QueryRow(ctx, `select definition from mod_content_templates where id=$1`, templateID).Scan(&storedDefinition)
		nextDefinition, _ := json.Marshal(disableEdit.Definition)
		t.Fatalf("custom publish rejected disabling a referenced type: %v current=%s next=%s", err, storedDefinition, nextDefinition)
	}
	var enabledValue string
	if err = tx.QueryRow(ctx, `select definition#>>'{entryTypes,0,enabled}' from mod_content_templates where id=$1`, templateID).Scan(&enabledValue); err != nil || enabledValue != "false" {
		t.Fatalf("disabled custom type was not persisted: enabled=%q err=%v", enabledValue, err)
	}
	if _, err = loadModContentEntryTypeDefinition(ctx, tx, modID, versionID, "minecraft.item", &sectionPublicID, "machine", false); !errors.Is(err, errCatalogEditorReference) {
		t.Fatalf("disabled type remained selectable for new details: %v", err)
	}
	if _, err = loadModContentEntryTypeDefinition(ctx, tx, modID, versionID, "minecraft.item", &sectionPublicID, "machine", true); err != nil {
		t.Fatalf("disabled type could not interpret an existing detail: %v", err)
	}
	unreferencedEditRevisionID := insertTemplateSchemaGuardRevision(t, ctx, tx, "unreferenced-edit", 1)
	unreferencedEdit := modContentTemplateEdit{Code: "unused_machine", DefaultLocale: "en-US", DefaultDisplayMode: "compact", Definition: templateSchemaGuardMap(false, "number", true)}
	if err = publishModContentSnapshotTx(ctx, tx, unreferencedEditRevisionID, modContentSnapshot{
		Kind: "template", Operation: "edit", ModID: modID, PublicID: unreferencedTemplatePublicID, Template: &unreferencedEdit,
	}, 0); err != nil {
		t.Fatalf("custom publish rejected unreferenced type deletion: %v", err)
	}
	referencedDeleteRevisionID := insertTemplateSchemaGuardRevision(t, ctx, tx, "referenced-delete", 1)
	if err = publishModContentSnapshotTx(ctx, tx, referencedDeleteRevisionID, modContentSnapshot{
		Kind: "template", Operation: "delete", ModID: modID, PublicID: templatePublicID,
	}, 0); !errors.Is(err, errCatalogEditorReference) {
		t.Fatalf("custom publish deleted a referenced template: %v", err)
	}
	unreferencedDeleteRevisionID := insertTemplateSchemaGuardRevision(t, ctx, tx, "unreferenced-delete", 1)
	if err = publishModContentSnapshotTx(ctx, tx, unreferencedDeleteRevisionID, modContentSnapshot{
		Kind: "template", Operation: "delete", ModID: modID, PublicID: unreferencedTemplatePublicID,
	}, 0); err != nil {
		t.Fatalf("custom publish rejected unreferenced template deletion: %v", err)
	}
}

func insertTemplateSchemaGuardRevision(t *testing.T, ctx context.Context, tx pgx.Tx, aggregateKey string, revisionNo int64) int64 {
	t.Helper()
	var revisionID int64
	if err := tx.QueryRow(ctx, `insert into content_revisions(aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash)
		values('mod_content_template',$1,$2,'{}'::jsonb,$3) returning id`, aggregateKey, revisionNo, "schema-guard-"+aggregateKey).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	return revisionID
}

func templateSchemaGuardMap(disabled bool, fieldType string, removed bool) map[string]any {
	definition := map[string]any{"resourceKinds": []any{"minecraft.item"}, "entryTypes": []any{}}
	if removed {
		return definition
	}
	entryType := map[string]any{
		"code": "machine", "kindCodes": []any{"minecraft.item"}, "names": map[string]any{"en-US": "Machine"},
		"groups": []any{map[string]any{
			"code": "main", "names": map[string]any{"en-US": "Main"},
			"fields": []any{map[string]any{"code": "power", "type": fieldType, "names": map[string]any{"en-US": "Power"}, "paths": []any{[]any{"power"}}}},
		}},
	}
	if disabled {
		entryType["enabled"] = false
	}
	definition["entryTypes"] = []any{entryType}
	return definition
}

func templateSchemaGuardFixture(disabled bool, fieldType string) modContentTemplateDefinition {
	entryType := modContentEntryTypeDefinition{
		Code: "machine", KindCodes: []string{"minecraft.item"}, Names: map[string]string{"en-US": "Machine"},
		Groups: []modContentEntryTypeGroup{{
			Code: "main", Names: map[string]string{"en-US": "Main"},
			Fields: []modContentEntryTypeField{{Code: "power", Type: fieldType, Names: map[string]string{"en-US": "Power"}, Paths: [][]string{{"power"}}}},
		}},
	}
	if disabled {
		enabled := false
		entryType.Enabled = &enabled
	}
	return modContentTemplateDefinition{ResourceKinds: []string{"minecraft.item"}, EntryTypes: []modContentEntryTypeDefinition{entryType}}
}
