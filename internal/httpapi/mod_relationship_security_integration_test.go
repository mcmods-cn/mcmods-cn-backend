package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func openModRelationshipSecurityTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run Mod relationship security tests")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertModRelationshipSecurityFixture(t *testing.T, ctx context.Context, tx pgx.Tx, status string) (int64, string) {
	t.Helper()
	publicID := randomCatalogPublicID()
	var modID int64
	if err := tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,$3,$4) returning id`, publicID, "sec013-"+publicID, "SEC-013 "+publicID, status).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	return modID, publicID
}

func TestIncomingModRelationshipSnapshotCannotRewriteSourceProject(t *testing.T) {
	db := openModRelationshipSecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sourceID, sourcePublicID := insertModRelationshipSecurityFixture(t, ctx, tx, "approved")
	targetID, _ := insertModRelationshipSecurityFixture(t, ctx, tx, "approved")
	var groupID, relationshipID int64
	if err = tx.QueryRow(ctx, `insert into mod_relationship_groups(mod_id,label,minecraft_versions) values($1,'source-owned','{}'::text[]) returning id`, sourceID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_relationships(mod_id,group_id,relation_type,related_mod_id,related_mod_name)
		values($1,$2,'dependency',$3,'Target') returning id`, sourceID, groupID, targetID).Scan(&relationshipID); err != nil {
		t.Fatal(err)
	}

	err = insertModRelationshipGroups(ctx, tx, targetID, []modRelationshipGroupPayload{{
		Direction:         "incoming",
		Label:             "forged-by-target",
		MinecraftVersions: []string{},
		Relationships: []modRelationshipPayload{{
			Type: "conflict", RelatedModPublicID: sourcePublicID,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var originalCount, forgedCount int
	if err = tx.QueryRow(ctx, `select count(*) from mod_relationships where id=$1 and mod_id=$2 and relation_type='dependency'`, relationshipID, sourceID).Scan(&originalCount); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*) from mod_relationships where mod_id=$1 and related_mod_id=$2 and relation_type='conflict'`, sourceID, targetID).Scan(&forgedCount); err != nil {
		t.Fatal(err)
	}
	if originalCount != 1 || forgedCount != 0 {
		t.Fatalf("incoming snapshot changed source-owned relationships: original=%d forged=%d", originalCount, forgedCount)
	}
}

func TestDifferentProjectEditorsCannotRewriteIncomingRelationshipOwnershipIntegration(t *testing.T) {
	db := openModRelationshipSecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sourceID, sourcePublicID := insertModRelationshipSecurityFixture(t, ctx, tx, "approved")
	targetID, targetPublicID := insertModRelationshipSecurityFixture(t, ctx, tx, "approved")
	sourceIdentity := modIdentityRecord{ID: sourceID, UniqueID: sourcePublicID}
	targetIdentity := modIdentityRecord{ID: targetID, UniqueID: targetPublicID}
	sourceEditor := security.Claims{Subject: 7001, PermissionRules: []security.PermissionRule{{
		Code: "project.edit." + sourcePublicID, Allow: true, Priority: 100,
	}}}
	targetEditor := security.Claims{Subject: 7002, PermissionRules: []security.PermissionRule{{
		Code: "project.edit." + targetPublicID, Allow: true, Priority: 100,
	}}}
	if !canEditMod(sourceEditor, sourceIdentity) || canEditMod(sourceEditor, targetIdentity) ||
		!canEditMod(targetEditor, targetIdentity) || canEditMod(targetEditor, sourceIdentity) {
		t.Fatal("fixture editors are not restricted to their respective projects")
	}

	var groupID, relationshipID int64
	if err = tx.QueryRow(ctx, `insert into mod_relationship_groups(mod_id,label,minecraft_versions)
		values($1,'source-owned','{}'::text[]) returning id`, sourceID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_relationships(mod_id,group_id,relation_type,related_mod_id,related_mod_name)
		values($1,$2,'dependency',$3,'Target') returning id`, sourceID, groupID, targetID).Scan(&relationshipID); err != nil {
		t.Fatal(err)
	}

	request := createModRequest{
		SiteID: targetPublicID, PrimaryName: "Target", Environment: "bothRequired", PrimaryCategory: "technology",
		OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual",
		RelationshipGroups: []modRelationshipGroupPayload{{
			Direction: "incoming", Label: "forged-by-target",
			Relationships: []modRelationshipPayload{{Type: "conflict", RelatedModPublicID: sourcePublicID}},
		}},
	}
	if err = normalizeAndValidateModRequest(&request); err != nil {
		t.Fatal(err)
	}
	if len(request.RelationshipGroups) != 0 {
		t.Fatalf("incoming groups survived the authoritative write payload: %#v", request.RelationshipGroups)
	}
	if err = insertModRelationshipGroups(ctx, tx, targetID, request.RelationshipGroups); err != nil {
		t.Fatal(err)
	}

	var originalCount, forgedCount int
	if err = tx.QueryRow(ctx, `select count(*) from mod_relationships where id=$1 and mod_id=$2`, relationshipID, sourceID).Scan(&originalCount); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*) from mod_relationships
		where mod_id=$1 and related_mod_id=$2 and relation_type='conflict'`, sourceID, targetID).Scan(&forgedCount); err != nil {
		t.Fatal(err)
	}
	if originalCount != 1 || forgedCount != 0 {
		t.Fatalf("target editor changed source-owned relationships: original=%d forged=%d", originalCount, forgedCount)
	}
}

func TestSymmetricModRelationshipDoesNotDeleteTargetOwnedReverse(t *testing.T) {
	db := openModRelationshipSecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sourceID, _ := insertModRelationshipSecurityFixture(t, ctx, tx, "approved")
	targetID, targetPublicID := insertModRelationshipSecurityFixture(t, ctx, tx, "approved")
	var targetGroupID, reverseRelationshipID int64
	if err = tx.QueryRow(ctx, `insert into mod_relationship_groups(mod_id,label,minecraft_versions) values($1,'target-owned','{}'::text[]) returning id`, targetID).Scan(&targetGroupID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into mod_relationships(mod_id,group_id,relation_type,related_mod_id,related_mod_name)
		values($1,$2,'conflict',$3,'Source') returning id`, targetID, targetGroupID, sourceID).Scan(&reverseRelationshipID); err != nil {
		t.Fatal(err)
	}

	err = insertModRelationshipGroups(ctx, tx, sourceID, []modRelationshipGroupPayload{{
		Direction:         "outgoing",
		Label:             "source-owned",
		MinecraftVersions: []string{},
		Relationships: []modRelationshipPayload{{
			Type: "conflict", RelatedModPublicID: targetPublicID,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var reverseCount int
	if err = tx.QueryRow(ctx, `select count(*) from mod_relationships where id=$1 and mod_id=$2`, reverseRelationshipID, targetID).Scan(&reverseCount); err != nil {
		t.Fatal(err)
	}
	if reverseCount != 1 {
		t.Fatal("saving a symmetric relationship deleted the target project's declaration")
	}
}

func TestOutgoingModRelationshipRejectsUnpublishedTarget(t *testing.T) {
	db := openModRelationshipSecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sourceID, _ := insertModRelationshipSecurityFixture(t, ctx, tx, "approved")
	_, targetPublicID := insertModRelationshipSecurityFixture(t, ctx, tx, "pending")
	err = insertModRelationshipGroups(ctx, tx, sourceID, []modRelationshipGroupPayload{{
		Direction:         "outgoing",
		MinecraftVersions: []string{},
		Relationships: []modRelationshipPayload{{
			Type: "dependency", RelatedModPublicID: targetPublicID,
		}},
	}})
	if err == nil {
		t.Fatal("an outgoing relationship referenced an unpublished target")
	}
}
