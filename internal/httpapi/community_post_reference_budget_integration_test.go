package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestCommunityPostReferenceResolutionAndReplacementStayConstantAtMaximumIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify community reference batch writes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if _, err = pool.Exec(ctx, `create temporary table public_routes(
		id bigint primary key,internal_id bigint not null,public_id text not null,entity_type text not null,canonical_path text not null);
	create temporary table mods(
		id bigint primary key,primary_name text not null,review_status text not null,submitted_by bigint not null,updated_at timestamptz not null);
	create temporary table modpacks(
		id bigint primary key,primary_name text not null,review_status text not null,submitted_by bigint not null,updated_at timestamptz not null);
	create temporary table simple_projects(
		id bigint primary key,project_type text not null,primary_name text not null,review_status text not null,
		submitted_by bigint not null,updated_at timestamptz not null);
	create temporary table minecraft_servers(
		id bigint primary key,name text not null,review_status text not null,submitted_by bigint not null,updated_at timestamptz not null);
	create temporary table community_posts(
		id bigint primary key,title text not null,status text not null,review_status text not null,author_id bigint not null,updated_at timestamptz not null);
	create temporary table blueprints(
		id bigint primary key,title text not null,status text not null,review_status text not null,owner_id bigint not null,updated_at timestamptz not null);
	create temporary table skin_assets(
		id bigint primary key,display_name text not null,status text not null,visibility text not null,
		review_status text not null,owner_id bigint not null,updated_at timestamptz not null);
	create temporary table catalog_entities(id bigint primary key,public_id text not null,status text not null);
	create temporary table game_resources(entity_id bigint primary key,kind_code text not null);
	create temporary table resource_kinds(code text primary key);
	create temporary table community_post_project_refs(
		id bigint generated always as identity primary key,post_id bigint not null,target_type text not null,
		target_id bigint,raw_identifier text not null,display_order integer not null);
	create temporary table community_post_resource_refs(
		id bigint generated always as identity primary key,post_id bigint not null,resource_id bigint,
		kind_code text not null,raw_resource_id text not null,display_order integer not null);
	create temporary table unresolved_references(
		id bigint generated always as identity primary key,source_type text not null,source_id bigint not null,
		field_path text not null,reference_type text not null,raw_identifier text not null,
		normalized_identifier text not null,status text not null default 'pending',resolved_type text not null default '',
		resolved_id bigint,resolved_at timestamptz,metadata jsonb not null default '{}'::jsonb,updated_at timestamptz not null default now(),
		unique(source_type,source_id,field_path,reference_type,normalized_identifier));
	insert into resource_kinds values('minecraft.item');
	insert into public_routes
	select value,value,'m'||lpad(value::text,8,'0'),'mod','/mods/m'||lpad(value::text,8,'0')
	from generate_series(1,16) value;
	insert into mods
	select value,'mod '||value,'approved',1,now() from generate_series(1,16) value;
	insert into catalog_entities
	select value,'r'||lpad(value::text,8,'0'),'active' from generate_series(1,32) value;
	insert into game_resources
	select value,'minecraft.item' from generate_series(1,32) value`); err != nil {
		t.Fatal(err)
	}

	makeSnapshot := func(resolvedProjects, rawProjects, resolvedResources, rawResources int) communityPostSnapshot {
		snapshot := communityPostSnapshot{Kind: "tutorial", Category: "general", Title: "batch", SourceLocale: "en-US", BodyMarkdown: "body"}
		for index := 1; index <= resolvedProjects; index++ {
			snapshot.Projects = append(snapshot.Projects, communityPostReference{Type: "mod", PublicID: fmt.Sprintf("m%08d", index)})
		}
		for index := 1; index <= rawProjects; index++ {
			snapshot.Projects = append(snapshot.Projects, communityPostReference{Type: "mod", Identifier: fmt.Sprintf("raw_mod_%d", index)})
		}
		for index := 1; index <= resolvedResources; index++ {
			snapshot.Resources = append(snapshot.Resources, communityPostReference{PublicID: fmt.Sprintf("r%08d", index)})
		}
		for index := 1; index <= rawResources; index++ {
			snapshot.Resources = append(snapshot.Resources, communityPostReference{Kind: "minecraft.item", Identifier: fmt.Sprintf("minecraft:raw_%d", index)})
		}
		if err := normalizeCommunityPostSnapshot(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	execute := func(postID int64, snapshot communityPostSnapshot, commit bool) int64 {
		t.Helper()
		tx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		defer tx.Rollback(ctx)
		counter.queries.Store(0)
		if resolveErr := resolveCommunityPostReferences(ctx, tx, security.Claims{Subject: 1}, &snapshot); resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if replaceErr := replaceCommunityPostReferencesTx(ctx, tx, postID, snapshot, security.Claims{Subject: 1}); replaceErr != nil {
			t.Fatal(replaceErr)
		}
		queries := counter.queries.Load()
		if commit {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				t.Fatal(commitErr)
			}
		}
		return queries
	}

	smallQueries := execute(1, makeSnapshot(1, 1, 1, 1), false)
	maximumQueries := execute(2, makeSnapshot(16, 16, 32, 32), true)
	if smallQueries != 9 || maximumQueries != smallQueries {
		t.Fatalf("community reference SQL small=%d maximum=%d want constant 9", smallQueries, maximumQueries)
	}
	var projects, resources, unresolved int
	if err = pool.QueryRow(ctx, `select
		(select count(*) from community_post_project_refs where post_id=2),
		(select count(*) from community_post_resource_refs where post_id=2),
		(select count(*) from unresolved_references)`).Scan(&projects, &resources, &unresolved); err != nil {
		t.Fatal(err)
	}
	if projects != 32 || resources != 64 || unresolved != 48 {
		t.Fatalf("persisted projects=%d resources=%d unresolved=%d", projects, resources, unresolved)
	}
	t.Logf("PERF053 reference SQL is constant at 9 for 2 and 96 mixed references")
}
