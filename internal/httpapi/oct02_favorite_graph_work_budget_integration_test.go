package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

type favoriteGraphExecutionPlan struct {
	NodeType     string                       `json:"Node Type"`
	RelationName string                       `json:"Relation Name"`
	ActualRows   float64                      `json:"Actual Rows"`
	ActualLoops  float64                      `json:"Actual Loops"`
	Plans        []favoriteGraphExecutionPlan `json:"Plans"`
}

func (plan favoriteGraphExecutionPlan) work(nodeType, relation string) float64 {
	var total float64
	if (nodeType == "" || plan.NodeType == nodeType) && (relation == "" || plan.RelationName == relation) {
		total += plan.ActualRows * plan.ActualLoops
	}
	for _, child := range plan.Plans {
		total += child.work(nodeType, relation)
	}
	return total
}

func TestOCT02FavoriteDependencyGraphBoundsTraversalWorkIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	for _, shape := range []string{"long-chain", "dense-cycle"} {
		t.Run(shape, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
			if err != nil {
				t.Fatal("parse owned database configuration")
			}
			poolConfig.MaxConns = 1
			pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
			if err != nil {
				t.Fatal("connect owned database")
			}
			defer pool.Close()
			// Every fixture object is connection-local and disappears on Close.
			if _, err = pool.Exec(ctx, `create temporary table public_routes(
				id bigint primary key,public_id text not null,entity_type text not null,internal_id bigint not null);
			create unique index oct02_graph_route_subject on public_routes(entity_type,internal_id);
			create temporary table mods(id bigint primary key,primary_name text not null,review_status text not null);
			create temporary table mod_relationships(id bigint primary key,mod_id bigint not null,
				relation_type text not null,related_mod_id bigint,group_id bigint,display_order integer not null);
			create index oct02_graph_relationship_source on mod_relationships(mod_id,display_order,id);
			create temporary table mod_relationship_groups(id bigint primary key,minecraft_versions text[] not null,loader text not null);
			create temporary table project_external_sources(project_route_id bigint not null,source_type text not null,external_project_id text not null);
			create unique index oct02_graph_external_source on project_external_sources(project_route_id,source_type)`); err != nil {
				t.Fatal(err)
			}
			nodes := 8000
			if shape == "dense-cycle" {
				nodes = 200
			}
			if _, err = pool.Exec(ctx, `insert into mods select value,'Dependency '||value,'approved' from generate_series(1,$1::integer) value`, nodes); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `insert into public_routes select id,'route-'||id,'mod',id from mods`); err != nil {
				t.Fatal(err)
			}
			if shape == "long-chain" {
				_, err = pool.Exec(ctx, `insert into mod_relationships select id,id,'dependency',id+1,null,0 from mods where id<$1`, nodes)
			} else {
				_, err = pool.Exec(ctx, `insert into mod_relationships
				select row_number() over(),source.id,'dependency',target.id,null,0
				from mods source cross join mods target where source.id<>target.id`)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `analyze public_routes; analyze mods; analyze mod_relationships;
			analyze mod_relationship_groups; analyze project_external_sources`); err != nil {
				t.Fatal(err)
			}
			request := favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "neoforge"}
			var encodedPlan []byte
			if err = pool.QueryRow(ctx, "explain (analyze,buffers,format json) "+favoriteExportDependencyGraphSQL,
				[]int64{1}, request.MinecraftVersion, request.Loader,
				maxFavoriteExportDependencyNodes+1, maxFavoriteExportDependencyEdges+1).Scan(&encodedPlan); err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				Plan favoriteGraphExecutionPlan `json:"Plan"`
			}
			if err = json.Unmarshal(encodedPlan, &plans); err != nil || len(plans) != 1 {
				t.Fatalf("decode execution plan: %v", err)
			}
			plan := plans[0].Plan
			recursiveWork := plan.work("Recursive Union", "")
			relationshipWork := plan.work("", "mod_relationships")
			t.Logf("%s: recursive rows %.0f, relationship rows*loops %.0f", shape, recursiveWork, relationshipWork)
			if recursiveWork > maxFavoriteExportDependencyNodes+1 {
				t.Errorf("traversal visited %.0f recursive rows beyond the node budget", recursiveWork)
			}
			// EXPLAIN rounds per-loop row counts. Include that rounding and a
			// second read of accepted edge IDs, while rejecting a full closure.
			workBudget := float64(2*(maxFavoriteExportDependencyEdges+1) + 2*(maxFavoriteExportDependencyNodes+1))
			if relationshipWork > workBudget {
				t.Errorf("traversal read %.0f relationship rows beyond work budget %.0f", relationshipWork, workBudget)
			}
			_, err = (&Server{db: pool}).loadFavoriteExportDependencyGraph(ctx, []int64{1}, request)
			if !errors.Is(err, errFavoriteExportDependencyLimit) {
				t.Fatalf("over-limit %s graph error = %v", shape, err)
			}
		})
	}
}
