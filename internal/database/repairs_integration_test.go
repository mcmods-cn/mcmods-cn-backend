package database

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func repairTestTransaction(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated generation-85 database")
	}
	cfg := config.Load()
	if cfg.Env != "test" {
		t.Fatal("schema repair tests require APP_ENV=test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return ctx, tx
}

func repairFixture(t *testing.T, ctx context.Context, tx pgx.Tx) (int64, int64, int64) {
	t.Helper()
	var mod, version, template int64
	if err := tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
  values(new_public_id(),'repair-'||gen_random_uuid()::text,'Synthetic repair fixture','approved') returning id`).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `insert into mod_content_versions(mod_id) values($1) returning id`, mod).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `select id from mod_content_templates where builtin and code='item_block'`).Scan(&template); err != nil {
		t.Fatal(err)
	}
	return mod, version, template
}

func insertRepairSection(ctx context.Context, tx pgx.Tx, mod, version, template int64, parent any, ordinal int) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,parent_id,display_mode,ordinal)
  values($1,$2,$3,$4,'compact',$5) returning id`, mod, version, template, parent, ordinal).Scan(&id)
	return id, err
}

func TestSchemaRepairRejectsDirtyRootsWithoutDeletingData(t *testing.T) {
	ctx, tx := repairTestTransaction(t)
	// Restore only the tested baseline invariant inside a rollback-only fixture.
	_, err := tx.Exec(ctx, `drop index if exists idx_mod_content_sections_root_ordinal;drop index if exists idx_mod_content_sections_root_system_key;drop table if exists schema_repair_history`)
	if err != nil {
		t.Fatal(err)
	}
	mod, version, template := repairFixture(t, ctx, tx)
	for range 2 {
		if _, err = insertRepairSection(ctx, tx, mod, version, template, nil, 0); err != nil {
			t.Fatal(err)
		}
	}
	err = applySchemaRepairsTx(ctx, tx)
	if err == nil || !strings.Contains(err.Error(), "duplicate root ordinals=1") {
		t.Fatalf("dirty baseline should stop with a count, got %v", err)
	}
	var count int
	if err = tx.QueryRow(ctx, `select count(*) from mod_content_sections where version_id=$1`, version).Scan(&count); err != nil || count != 2 {
		t.Fatalf("dirty rows were changed: count=%d err=%v", count, err)
	}
}

func TestSchemaRepairProtectsRootUniquenessAndAncestorCycles(t *testing.T) {
	ctx, tx := repairTestTransaction(t)
	if err := applySchemaRepairsTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	mod, version, template := repairFixture(t, ctx, tx)
	root, err := insertRepairSection(ctx, tx, mod, version, template, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	child, err := insertRepairSection(ctx, tx, mod, version, template, root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `savepoint duplicate_root`); err != nil {
		t.Fatal(err)
	}
	if _, err = insertRepairSection(ctx, tx, mod, version, template, nil, 0); err == nil {
		t.Fatal("duplicate root ordinal was accepted")
	}
	if _, err = tx.Exec(ctx, `rollback to savepoint duplicate_root`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `savepoint ancestor_cycle`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update mod_content_sections set parent_id=$2 where id=$1`, root, child); err == nil {
		t.Fatal("ancestor cycle was accepted")
	}
	if _, err = tx.Exec(ctx, `rollback to savepoint ancestor_cycle`); err != nil {
		t.Fatal(err)
	}
	if err = applySchemaRepairsTx(ctx, tx); err != nil {
		t.Fatalf("repeat patch application: %v", err)
	}
	var count int
	if err = tx.QueryRow(ctx, `select count(*) from schema_repair_history where code='audit-2026-10-content-tree-and-favorite'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("patch ledger count=%d err=%v", count, err)
	}
}

func TestFavoriteBaselineAmbiguityAndForwardRepair(t *testing.T) {
	ctx, tx := repairTestTransaction(t)
	if err := applySchemaRepairsTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	mod, _, _ := repairFixture(t, ctx, tx)
	var user, collection int64
	if err := tx.QueryRow(ctx, `insert into users(username,email,password_hash) values('repair-'||gen_random_uuid()::text,gen_random_uuid()::text||'@test.invalid','synthetic-unused') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `insert into favorite_collections(user_id,name) values($1,'Synthetic repair favorite') returning id`, user).Scan(&collection); err != nil {
		t.Fatal(err)
	}
	var baseline string
	for _, statement := range ratingSchemaStatements() {
		if strings.HasPrefix(statement, "create or replace function refresh_popularity_from_favorite()") {
			baseline = statement
		}
	}
	if baseline == "" {
		t.Fatal("favorite baseline function missing")
	}
	if _, err := tx.Exec(ctx, baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `savepoint original_favorite`); err != nil {
		t.Fatal(err)
	}
	_, err := tx.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id) values($1,'mod',$2)`, collection, mod)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected original ambiguous SQL error, got %v", err)
	}
	if _, err = tx.Exec(ctx, `rollback to savepoint original_favorite;delete from schema_repair_history where code='audit-2026-10-content-tree-and-favorite';drop index idx_mod_content_sections_root_ordinal;drop index idx_mod_content_sections_root_system_key`); err != nil {
		t.Fatal(err)
	}
	if err = applySchemaRepairsTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id) values($1,'mod',$2)`, collection, mod); err != nil {
		t.Fatalf("favorite still fails after repair: %v", err)
	}
	if _, err = tx.Exec(ctx, `delete from favorite_collection_items where collection_id=$1`, collection); err != nil {
		t.Fatalf("favorite deletion after repair: %v", err)
	}
}

func TestGovernanceSeedsPreserveAdministratorPoliciesAndUseRealNewlines(t *testing.T) {
	ctx, tx := repairTestTransaction(t)
	if _, err := tx.Exec(ctx, `update ban_reasons set translations='{"en-US":"Administrator reason"}',sort_order=123 where code='other';update license_policies set redistribution_allowed=false,notes='Administrator policy' where spdx_id='MIT';delete from site_page_translations where page_id=(select id from site_pages where code='about') and locale='en-US'`); err != nil {
		t.Fatal(err)
	}
	if err := seedGovernanceAutomationDefaults(ctx, tx); err != nil {
		t.Fatal(err)
	}
	var text string
	var sort int
	if err := tx.QueryRow(ctx, `select translations->>'en-US',sort_order from ban_reasons where code='other'`).Scan(&text, &sort); err != nil || text != "Administrator reason" || sort != 123 {
		t.Fatalf("administrator reason overwritten text=%q sort=%d err=%v", text, sort, err)
	}
	var redistribution bool
	if err := tx.QueryRow(ctx, `select redistribution_allowed,notes from license_policies where spdx_id='MIT'`).Scan(&redistribution, &text); err != nil || redistribution || text != "Administrator policy" {
		t.Fatalf("administrator policy overwritten allowed=%v text=%q err=%v", redistribution, text, err)
	}
	if err := tx.QueryRow(ctx, `select body_markdown from site_page_translations where page_id=(select id from site_pages where code='about') and locale='en-US'`).Scan(&text); err != nil || !strings.Contains(text, "\n\n") || strings.Contains(text, `\n`) {
		t.Fatalf("about page escaped literal newline persisted: %q err=%v", text, err)
	}
}

func TestDefaultUserSeedsPreserveRevocationAndExistingPassword(t *testing.T) {
	ctx, tx := repairTestTransaction(t)
	if _, err := tx.Exec(ctx, `update users set status='banned',email='audit-admin@example.invalid' where username='admin';update user_permissions set allow=false where user_id=(select id from users where username='admin')`); err != nil {
		t.Fatal(err)
	}
	var beforeHash string
	var beforeAuth int64
	if err := tx.QueryRow(ctx, `select password_hash,auth_version from users where username='admin'`).Scan(&beforeHash, &beforeAuth); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaultUsers(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaultUsers(ctx, tx); err != nil {
		t.Fatal(err)
	}
	var status, email, hash string
	var auth int64
	if err := tx.QueryRow(ctx, `select status,email,password_hash,auth_version from users where username='admin'`).Scan(&status, &email, &hash, &auth); err != nil {
		t.Fatal(err)
	}
	if status != "banned" || email != "audit-admin@example.invalid" || hash != beforeHash || auth != beforeAuth {
		t.Fatalf("seed rewrote administrator state status=%s email=%s auth=%d want%d", status, email, auth, beforeAuth)
	}
	var allow int
	if err := tx.QueryRow(ctx, `select count(*) from user_permissions where user_id=(select id from users where username='admin') and allow`).Scan(&allow); err != nil || allow != 0 {
		t.Fatalf("seed reinstated explicit permissions count=%d err=%v", allow, err)
	}
}
