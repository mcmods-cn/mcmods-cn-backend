package httpapi

import (
	"context"
	"testing"
	"time"
)

func TestImportedContentSyncArchivesOnlyOrphansInSelectedVersionIntegration(t *testing.T) {
	pool := openModExportConcurrentTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	revisionID, versionID := createRealKeyMappingImportFacts(t, ctx, tx)
	rows, err := prepareExportRegistryResources(nil, map[string]string{"minecraft": revisionID}, "registries/enchantments.json", []byte(`{"registry":"enchantments","entries":[{"id":"minecraft:orphan_test","min_level":1,"max_level":1}]}`))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	batch := newModExportWriteBatch()
	queueCatalogResource(batch, rows[0])
	if err = batch.flush(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = syncImportedResourcesToContentVersionTx(ctx, tx, []string{revisionID}, versionID, false, 0); err != nil {
		t.Fatal(err)
	}
	var rootID, templateID, modID, otherVersionID int64
	if err = tx.QueryRow(ctx, `select section.id,section.template_id,section.mod_id from mod_content_sections section join mod_content_templates template on template.id=section.template_id where section.version_id=$1 and section.parent_id is null and template.code='enchantment' and section.status='active'`, versionID).Scan(&rootID, &templateID, &modID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status) values($1,'unrelated orphan scope','active') returning id`, modID).Scan(&otherVersionID); err != nil {
		t.Fatal(err)
	}
	insertSection := func(version, parent int64, ordinal int, status string) int64 {
		var id int64
		if err := tx.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,parent_id,default_locale,display_mode,ordinal,status) select $1,$2,id,nullif($4::bigint,0),default_locale,default_display_mode,$5,$6 from mod_content_templates where id=$3 returning id`, modID, version, templateID, parent, ordinal, status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	childID := insertSection(versionID, rootID, 0, "active")
	grandchildID := insertSection(versionID, childID, 0, "active")
	liveRootID := insertSection(versionID, 0, 1, "active")
	liveChildID := insertSection(versionID, liveRootID, 0, "active")
	pendingRootID := insertSection(versionID, 0, 2, "pending")
	pendingChildID := insertSection(versionID, pendingRootID, 0, "active")
	otherRootID := insertSection(otherVersionID, 0, 0, "active")
	otherChildID := insertSection(otherVersionID, otherRootID, 0, "active")
	if _, err = tx.Exec(ctx, `update mod_content_sections set status='archived' where id=any($1::bigint[])`, []int64{rootID, otherRootID}); err != nil {
		t.Fatal(err)
	}
	if err = syncImportedResourcesToContentVersionTx(ctx, tx, []string{revisionID}, versionID, true, 0); err != nil {
		t.Fatal(err)
	}
	for id, expected := range map[int64]string{rootID: "archived", childID: "archived", grandchildID: "archived", liveRootID: "active", liveChildID: "active", pendingRootID: "pending", pendingChildID: "active", otherRootID: "archived", otherChildID: "active"} {
		var actual string
		if err = tx.QueryRow(ctx, `select status from mod_content_sections where id=$1`, id).Scan(&actual); err != nil || actual != expected {
			t.Fatalf("section=%d status=%s want=%s err=%v", id, actual, expected, err)
		}
	}
}
