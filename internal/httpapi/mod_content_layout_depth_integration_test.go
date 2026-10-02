package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestPublishModContentLayoutSafelyRebuildsCategoryTreeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify category-tree layout publication")
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

	var actorID, modID, versionID, templateID int64
	var versionPublicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('layout_depth_actor','layout-depth@example.invalid','test-only',true) returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('depth0002','layout-depth','Layout depth','approved',$1) returning id`, actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by)
		values($1,'Layout depth','active',$2,$2) returning id,public_id`, modID, actorID).Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from mod_content_templates where builtin and status='active' order by id limit 1`).Scan(&templateID); err != nil {
		t.Fatal(err)
	}

	type section struct {
		id       int64
		publicID string
	}
	insertSection := func(parentID *int64, ordinal int, systemKey string) section {
		t.Helper()
		var result section
		if err := pool.QueryRow(ctx, `insert into mod_content_sections(
			mod_id,version_id,template_id,parent_id,system_key,display_mode,ordinal,status,created_by,updated_by)
			values($1,$2,$3,$4,$5,'compact',$6,'active',$7,$7) returning id,public_id`,
			modID, versionID, templateID, parentID, systemKey, ordinal, actorID).Scan(&result.id, &result.publicID); err != nil {
			t.Fatal(err)
		}
		return result
	}
	parent := func(id int64) *int64 { return &id }

	root := insertSection(nil, 0, "")
	target := insertSection(parent(root.id), 0, "target")
	moving := insertSection(parent(root.id), 1, "repeat")
	removed1 := insertSection(parent(moving.id), 0, "repeat")
	removed2 := insertSection(parent(removed1.id), 0, "repeat")
	removed3 := insertSection(parent(removed2.id), 0, "repeat")

	var revisionID int64
	if err = pool.QueryRow(ctx, `insert into content_revisions(
		entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source)
		select route.entity_type,route.internal_id,'mod_content_section',$1,1,'{}'::jsonb,'layout-depth',$2,'test'
		from public_routes route where route.public_id=$1 and route.entity_type='mod_content_section' returning id`,
		root.publicID, actorID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}

	layout := modContentLayoutEdit{
		VersionPublicID:     versionPublicID,
		RootSectionPublicID: root.publicID,
		DisplayMode:         "compact",
		Categories: []modContentLayoutCategoryEdit{
			{PublicID: target.publicID, ParentPublicID: root.publicID, DefaultLocale: "en-US", Ordinal: 0},
			{PublicID: moving.publicID, ParentPublicID: target.publicID, DefaultLocale: "en-US", Ordinal: 0},
		},
	}
	if err = validateModContentCategoryTree(root.publicID, layout.Categories); err != nil {
		t.Fatalf("valid target layout rejected: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = publishModContentLayoutTx(ctx, tx, revisionID, modContentSnapshot{
		Kind: "layout", Operation: "edit", ModID: modID, PublicID: root.publicID, Layout: &layout,
	}, actorID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var movingParentID int64
	var movingSystemKey string
	if err = pool.QueryRow(ctx, `select parent_id,system_key from mod_content_sections where id=$1`, moving.id).
		Scan(&movingParentID, &movingSystemKey); err != nil {
		t.Fatal(err)
	}
	if movingParentID != target.id || movingSystemKey != "repeat" {
		t.Fatalf("moving category parent=%d systemKey=%q; want parent=%d systemKey=repeat", movingParentID, movingSystemKey, target.id)
	}
	var archivedCount, stagingKeyCount, maximumDepth int
	if err = pool.QueryRow(ctx, `select count(*)::int from mod_content_sections
		where id=any($1::bigint[]) and status='archived'`, []int64{removed1.id, removed2.id, removed3.id}).Scan(&archivedCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from mod_content_sections
		where id=any($1::bigint[]) and system_key like '__layout_staging_%'`,
		[]int64{target.id, moving.id, removed1.id, removed2.id, removed3.id}).Scan(&stagingKeyCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `with recursive tree as (
		select id,0 depth from mod_content_sections where id=$1 and status='active'
		union all
		select child.id,parent.depth+1 from mod_content_sections child join tree parent on child.parent_id=parent.id
		where child.status='active'
	)
	select max(depth) from tree`, root.id).Scan(&maximumDepth); err != nil {
		t.Fatal(err)
	}
	if archivedCount != 3 || stagingKeyCount != 0 || maximumDepth != 2 {
		t.Fatalf("archived=%d stagingKeys=%d maximumDepth=%d; want 3, 0, 2", archivedCount, stagingKeyCount, maximumDepth)
	}
}
