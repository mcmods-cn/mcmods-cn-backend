package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestModContentCategoryDepthInvariantCoversSubtreeMovesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the category-depth invariant")
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
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var actorID, modID, versionID, templateID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('category_depth_actor','category-depth@example.invalid','test-only',true) returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('depth0001','category-depth','Category depth','approved',$1) returning id`, actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by)
		values($1,'Depth invariant','active',$2,$2) returning id`, modID, actorID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from mod_content_templates where builtin and status='active' order by id limit 1`).Scan(&templateID); err != nil {
		t.Fatal(err)
	}

	insertSection := func(parentID *int64, ordinal int) int64 {
		t.Helper()
		var sectionID int64
		if err := pool.QueryRow(ctx, `insert into mod_content_sections(
			mod_id,version_id,template_id,parent_id,display_mode,ordinal,status,created_by,updated_by)
			values($1,$2,$3,$4,'compact',$5,'active',$6,$6) returning id`,
			modID, versionID, templateID, parentID, ordinal, actorID).Scan(&sectionID); err != nil {
			t.Fatal(err)
		}
		return sectionID
	}
	parent := func(id int64) *int64 { return &id }

	rootID := insertSection(nil, 0)
	a1 := insertSection(parent(rootID), 0)
	a2 := insertSection(parent(a1), 0)
	_ = insertSection(parent(a2), 0)
	b1 := insertSection(parent(rootID), 1)
	b2 := insertSection(parent(b1), 0)

	if _, err = pool.Exec(ctx, `update mod_content_sections set parent_id=$2 where id=$1`, a1, b2); err == nil {
		t.Fatal("moving a category with descendants into a fifth-level result unexpectedly succeeded")
	}
	var actualParentID int64
	if err = pool.QueryRow(ctx, `select parent_id from mod_content_sections where id=$1`, a1).Scan(&actualParentID); err != nil {
		t.Fatal(err)
	}
	if actualParentID != rootID {
		t.Fatalf("rejected subtree move changed parent to %d; want %d", actualParentID, rootID)
	}

	c1 := insertSection(parent(rootID), 2)
	c2 := insertSection(parent(c1), 0)
	c3 := insertSection(parent(c2), 0)
	c4 := insertSection(parent(c3), 0)
	if _, err = pool.Exec(ctx, `insert into mod_content_sections(
		mod_id,version_id,template_id,parent_id,display_mode,ordinal,status,created_by,updated_by)
		values($1,$2,$3,$4,'compact',0,'active',$5,$5)`, modID, versionID, templateID, c4, actorID); err == nil {
		t.Fatal("direct fifth-level category insert unexpectedly succeeded")
	}

	var maximumDepth int
	if err = pool.QueryRow(ctx, `with recursive tree as (
		select id,0 depth from mod_content_sections where id=$1
		union all
		select child.id,parent.depth+1 from mod_content_sections child join tree parent on child.parent_id=parent.id
	)
	select max(depth) from tree`, rootID).Scan(&maximumDepth); err != nil {
		t.Fatal(err)
	}
	if maximumDepth != 4 {
		t.Fatalf("maximum category depth=%d want 4", maximumDepth)
	}
}
