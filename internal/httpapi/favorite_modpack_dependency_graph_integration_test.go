package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

type favoriteDependencyGraphQueryCounter struct{ queries atomic.Int64 }

func (counter *favoriteDependencyGraphQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == favoriteExportDependencyGraphSQL {
		counter.queries.Add(1)
	}
	return ctx
}

func (*favoriteDependencyGraphQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestFavoriteExportDependencyGraphTraversesCyclesAndReportsLimitsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the MRPack dependency graph")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	counter := &favoriteDependencyGraphQueryCounter{}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
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

	if _, err = pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		select 'b'||lpad(to_hex(value),8,'0'),'perf040-cycle-'||value,'Dependency '||value,'approved'
		from generate_series(1,150) value;
		with ordered as (
			select id,row_number() over(order by slug::text) ordinal
			from mods where slug like 'perf040-cycle-%'
		), chain as (
			select source.id source_id,target.id target_id,source.ordinal
			from ordered source join ordered target on target.ordinal=source.ordinal+1
			union all
			select source.id,target.id,source.ordinal
			from ordered source cross join ordered target where source.ordinal=150 and target.ordinal=1
		)
		insert into mod_relationships(mod_id,relation_type,related_mod_id,display_order)
		select source_id,'dependency',target_id,ordinal from chain`); err != nil {
		t.Fatal(err)
	}
	var seedRouteID int64
	if err = pool.QueryRow(ctx, `select route.id from public_routes route join mods mod on mod.id=route.internal_id
		where route.entity_type='mod' and mod.slug='perf040-cycle-1'`).Scan(&seedRouteID); err != nil {
		t.Fatal(err)
	}
	counter.queries.Store(0)
	server := &Server{db: pool}
	graph, err := server.loadFavoriteExportDependencyGraph(ctx, []int64{seedRouteID}, favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "neoforge"})
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.queries.Load(); got != 1 {
		t.Fatalf("150-node cycle used %d graph queries, want 1", got)
	}
	edges := 0
	for _, adjacency := range graph.Edges {
		edges += len(adjacency)
	}
	if len(graph.Nodes) != 150 || edges != 150 {
		t.Fatalf("cycle graph = %d nodes/%d edges, want 150/150", len(graph.Nodes), edges)
	}
	if _, err = pool.Exec(ctx, `insert into mod_relationships(mod_id,relation_type,related_mod_id,display_order)
		select source_mod.id,'dependency',target_mod.id,(1000+row_number() over(order by target_mod.id))::integer
		from public_routes source_route
		join mods source_mod on source_mod.id=source_route.internal_id
		cross join mods target_mod
		where source_route.id=$1 and target_mod.slug like 'perf040-cycle-%' and target_mod.id<>source_mod.id`, seedRouteID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into project_external_sources(
		project_route_id,source_type,external_project_id,external_project_url,verified_at)
		select route.id,'modrinth','project-'||route.id,'https://modrinth.com/project/project-'||route.id,now()
		from public_routes route join mods mod on mod.id=route.internal_id
		where route.entity_type='mod' and mod.slug like 'perf040-cycle-%'`); err != nil {
		t.Fatal(err)
	}
	var providerActive atomic.Int64
	var providerMaximum atomic.Int64
	var providerRequests atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		providerRequests.Add(1)
		current := providerActive.Add(1)
		for {
			previous := providerMaximum.Load()
			if current <= previous || providerMaximum.CompareAndSwap(previous, current) {
				break
			}
		}
		defer providerActive.Add(-1)
		time.Sleep(5 * time.Millisecond)
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "project" || parts[2] != "version" {
			http.NotFound(response, request)
			return
		}
		projectID := parts[1]
		versionID := "version-" + projectID
		fileName := "dependency-" + projectID + ".jar"
		response.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(response).Encode([]map[string]any{{
			"id": versionID, "project_id": projectID, "name": "Release", "version_number": "1.0.0", "version_type": "release",
			"game_versions": []string{"1.21.1"}, "loaders": []string{"neoforge"}, "date_published": "2026-08-22T00:00:00Z",
			"files": []map[string]any{{"hashes": map[string]string{"sha1": strings.Repeat("a", 40), "sha512": strings.Repeat("b", 128)},
				"url":      "https://cdn.modrinth.com/data/" + projectID + "/versions/" + versionID + "/" + fileName,
				"filename": fileName, "primary": true, "size": 1024}},
		}}); encodeErr != nil {
			t.Errorf("encode provider response: %v", encodeErr)
		}
	}))
	defer provider.Close()
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	server = &Server{db: pool, cache: cache, cfg: config.Load()}
	providerConfig := modImportConfig{UserAgent: "perf040-test", RequestTimeoutSeconds: 5,
		Modrinth: modImportProviderConfig{Enabled: true, BaseURL: provider.URL}}
	items := []favoriteModpackExportItem{{
		SourceProjectRouteID: int64Pointer(seedRouteID), SourceProjectType: "mod", SourceProjectName: "Dependency 1", ResultType: "exported",
	}}
	counter.queries.Store(0)
	items, err = server.appendFavoriteExportDependencies(ctx, items,
		favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "neoforge"},
		providerConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 150 {
		t.Fatalf("batched dependency preview returned %d items, want 150", len(items))
	}
	if got := counter.queries.Load(); got != 1 {
		t.Fatalf("batched dependency preview used %d graph queries, want 1", got)
	}
	if got := providerRequests.Load(); got != 149 {
		t.Fatalf("batched dependency preview used %d provider requests, want 149 unique dependencies", got)
	}
	if got := providerMaximum.Load(); got > maxFavoriteExportFileConcurrency || got < 2 {
		t.Fatalf("provider concurrency = %d, want 2..%d", got, maxFavoriteExportFileConcurrency)
	}
	for index := 1; index < len(items); index++ {
		if items[index].ResultType != "auto_dependency" || items[index].ReasonCode != "" {
			t.Fatalf("dependency %d = %s/%s", index, items[index].ResultType, items[index].ReasonCode)
		}
	}
	var ownerID, seedModID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('perf040-owner','perf040-owner@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select internal_id from public_routes where id=$1 and entity_type='mod'`, seedRouteID).Scan(&seedModID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_collections(public_id,user_id,name)
		values('p040test1',$1,'PERF-040 collection')`, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id)
		select id,'mod',$2 from favorite_collections where public_id=$1`, "p040test1", seedModID); err != nil {
		t.Fatal(err)
	}
	sealedProviderConfig, err := server.sealSystemSetting(providerConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
		on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealedProviderConfig); err != nil {
		t.Fatal(err)
	}
	catalog, err := loadMinecraftVersionConfig(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Loaders {
		if catalog.Loaders[index].Code == "NeoForge" {
			catalog.Loaders[index].Versions = []string{"1.21.1"}
		}
	}
	if err = saveSynchronizedMinecraftVersionConfig(ctx, pool, catalog, []minecraftLoaderArtifactSnapshot{{
		MinecraftVersion: "1.21.1", Loader: "neoforge", LoaderVersion: "21.1.0",
		SourceURL: neoForgeMavenMetadataURL, ObservedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	defer resetMinecraftSourceCacheForTest()
	counter.queries.Store(0)
	preview, err := server.buildFavoriteModpackExportPreview(ctx, security.Claims{Subject: ownerID}, "p040test1",
		favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "neoforge"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.CollectionItemCount != 1 || preview.ExportedModCount != 1 || preview.AutoDependencyCount != 149 ||
		preview.SkippedItemCount != 0 || preview.FailedItemCount != 0 {
		t.Fatalf("preview counts = collection:%d exported:%d dependencies:%d skipped:%d failed:%d",
			preview.CollectionItemCount, preview.ExportedModCount, preview.AutoDependencyCount, preview.SkippedItemCount, preview.FailedItemCount)
	}
	if got := counter.queries.Load(); got != 1 {
		t.Fatalf("full preview used %d dependency graph queries, want 1", got)
	}
	if got := providerRequests.Load(); got != 150 {
		t.Fatalf("full preview provider requests = %d, want one uncached seed plus 149 cached dependencies", got)
	}

	if _, err = pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		select 'c'||lpad(to_hex(value),8,'0'),'perf040-limit-'||value,'Limit dependency '||value,'approved'
		from generate_series(1,1001) value;
		with ordered as (
			select id,row_number() over(order by id) ordinal
			from mods where slug like 'perf040-limit-%'
		)
		insert into mod_relationships(mod_id,relation_type,related_mod_id,display_order)
		select source.id,'dependency',target.id,source.ordinal
		from ordered source join ordered target on target.ordinal=source.ordinal+1`); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select route.id from public_routes route join mods mod on mod.id=route.internal_id
		where route.entity_type='mod' and mod.slug='perf040-limit-1'`).Scan(&seedRouteID); err != nil {
		t.Fatal(err)
	}
	counter.queries.Store(0)
	_, err = server.loadFavoriteExportDependencyGraph(ctx, []int64{seedRouteID}, favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "neoforge"})
	if !errors.Is(err, errFavoriteExportDependencyLimit) {
		t.Fatalf("1001-node graph error = %v", err)
	}
	if got := counter.queries.Load(); got != 1 {
		t.Fatalf("over-limit graph used %d queries, want 1", got)
	}
}

func TestFavoriteExportDependencyGraphStaysIndexedAtMillionRelationshipsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the MRPack dependency graph at scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table public_routes(
		id bigint primary key,public_id text not null,entity_type text not null,internal_id bigint not null);
	create unique index idx_perf040_route_subject on public_routes(entity_type,internal_id);
	create temporary table mods(id bigint primary key,primary_name text not null,review_status text not null);
	create temporary table mod_relationships(
		id bigint primary key,mod_id bigint not null,relation_type text not null,related_mod_id bigint,
		group_id bigint,display_order integer not null);
	create index idx_perf040_relationship_source on mod_relationships(mod_id,display_order,id);
	create temporary table mod_relationship_groups(
		id bigint primary key,minecraft_versions text[] not null,loader text not null);
	create temporary table project_external_sources(
		project_route_id bigint not null,source_type text not null,external_project_id text not null);
	create unique index idx_perf040_external_source on project_external_sources(project_route_id,source_type);
	insert into mods select value,'Dependency '||value,'approved' from generate_series(1,1000000) value;
	insert into public_routes select value,'route-'||value,'mod',value from generate_series(1,1000000) value;
	insert into mod_relationships
	select value,value,'dependency',case when value<1000 then value+1 else value end,null,0
	from generate_series(1,1000000) value;
	analyze public_routes; analyze mods; analyze mod_relationships; analyze mod_relationship_groups; analyze project_external_sources`); err != nil {
		t.Fatal(err)
	}
	request := favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "neoforge"}
	planRows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+favoriteExportDependencyGraphSQL,
		[]int64{1}, request.MinecraftVersion, request.Loader, maxFavoriteExportDependencyNodes+1, maxFavoriteExportDependencyEdges+1)
	if err != nil {
		t.Fatal(err)
	}
	var planBuilder strings.Builder
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			planRows.Close()
			t.Fatal(err)
		}
		planBuilder.WriteString(line)
		planBuilder.WriteByte('\n')
	}
	if err = planRows.Err(); err != nil {
		planRows.Close()
		t.Fatal(err)
	}
	planRows.Close()
	plan := planBuilder.String()
	if strings.Contains(plan, "Seq Scan on mod_relationships") || strings.Contains(plan, "Seq Scan on public_routes") ||
		!strings.Contains(plan, "idx_perf040_relationship_source") {
		t.Fatalf("million-relationship graph did not stay on route/relationship indexes:\n%s", plan)
	}
	started := time.Now()
	graph, err := (&Server{db: pool}).loadFavoriteExportDependencyGraph(ctx, []int64{1}, request)
	if err != nil {
		t.Fatal(err)
	}
	duration := time.Since(started)
	if duration > 2*time.Second {
		t.Fatalf("million-relationship graph took %s, limit 2s", duration)
	}
	if len(graph.Nodes) != 1000 {
		t.Fatalf("million-relationship graph returned %d reachable nodes, want 1000", len(graph.Nodes))
	}
	t.Logf("million-relationship graph: %s\n%s", duration, fmt.Sprintf("%s", plan))
}
