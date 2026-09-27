package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestContentReviewQueueScalesToOneHundredThousandPendingItemsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the 100k unified review queue plan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	counter := &integrationQueryCounter{}
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

	var submitterID, reviewerID, modID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('perf012_submitter','perf012-submitter@example.invalid','test-only',true) returning id`).Scan(&submitterID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('perf012_reviewer','perf012-reviewer@example.invalid','test-only',true) returning id`).Scan(&reviewerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('perf01201','perf-012-scale','PERF-012 scale','approved',$1) returning id`, submitterID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_revisions(
		public_id,entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source,created_at)
		select 'q'||lpad(value::text,8,'0'),'mod',$1,'mod','perf012-'||lpad(value::text,8,'0'),1,
			jsonb_build_object('value',value),'perf012-hash-'||value,$2,'test',
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 millisecond'
		from generate_series(1,100000) value`, modID, submitterID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(
		public_id,entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,reason,submitted_by,submitted_at)
		select 'r'||substr(revision.public_id,2),'mod',$1,'mod',revision.aggregate_key,revision.id,
			'pending','PERF-012 reason',$2,revision.created_at
		from content_revisions revision where revision.aggregate_key like 'perf012-%'`, modID, submitterID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `analyze content_revisions; analyze change_requests; analyze mods; analyze users`); err != nil {
		t.Fatal(err)
	}

	args := []any{[]string{}, true, true, reviewerID, "", "", "", "", 100, int64(99900)}
	counter.queries.Store(0)
	var itemsRaw, facetsRaw []byte
	var total int64
	if err = pool.QueryRow(ctx, contentReviewQueueQuery, args...).Scan(&itemsRaw, &total, &facetsRaw); err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load() != 1 {
		t.Fatalf("review queue executed %d SQL statements, want 1", counter.queries.Load())
	}
	var items []modContentReviewItem
	if err = json.Unmarshal(itemsRaw, &items); err != nil {
		t.Fatal(err)
	}
	var facets struct {
		Categories []struct {
			Value string `json:"value"`
			Count int64  `json:"count"`
		} `json:"categories"`
	}
	if err = json.Unmarshal(facetsRaw, &facets); err != nil {
		t.Fatal(err)
	}
	firstID, lastID := "", ""
	if len(items) > 0 {
		firstID, lastID = items[0].ID, items[len(items)-1].ID
	}
	if total != 100000 || len(items) != 100 || firstID != "q00099901" || lastID != "q00100000" ||
		len(facets.Categories) != 1 || facets.Categories[0].Value != "mod" || facets.Categories[0].Count != 100000 {
		t.Fatalf("100k queue result changed: total=%d items=%d first/last=%q/%q facets=%s",
			total, len(items), firstID, lastID, facetsRaw)
	}

	var rawPlan []byte
	if err = pool.QueryRow(ctx, `explain (analyze,buffers,format json) `+contentReviewQueueQuery, args...).Scan(&rawPlan); err != nil {
		t.Fatal(err)
	}
	facts, err := reviewQueueScalePlanFacts(rawPlan)
	if err != nil {
		t.Fatal(err)
	}
	if facts.RevisionRowsExamined > 150000 || facts.RequestRowsExamined > 150000 {
		t.Fatalf("100k queue repeatedly scanned base rows: revisions=%.0f requests=%.0f",
			facts.RevisionRowsExamined, facts.RequestRowsExamined)
	}
	if facts.ExecutionMS > 5000 {
		t.Fatalf("100k queue execution %.3fms exceeds 5s regression ceiling", facts.ExecutionMS)
	}
	t.Logf("execution_ms=%.3f revisions_examined=%.0f requests_examined=%.0f temp_read=%0.f temp_written=%.0f",
		facts.ExecutionMS, facts.RevisionRowsExamined, facts.RequestRowsExamined, facts.TempReadBlocks, facts.TempWrittenBlocks)
}

func TestContentReviewQueueScalesToOneMillionPendingItemsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the 1M unified review queue plan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	counter := &integrationQueryCounter{}
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
	createReviewQueueScaleShadowTables(t, ctx, pool)
	if _, err = pool.Exec(ctx, `insert into users values(1,'user00001','perf012_submitter'),(2,'user00002','perf012_reviewer');
		insert into mods values(1,'perf01201','perf-012-scale','PERF-012 scale')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_revisions(
		id,public_id,entity_type,entity_id,aggregate_type,aggregate_key,revision_no,created_at)
		select value,'q'||lpad(value::text,8,'0'),'mod',1,'mod','perf012-'||lpad(value::text,8,'0'),1,
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 millisecond'
		from generate_series(1,1000000) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(
		proposed_revision_id,base_revision_id,submitted_by,status,reason,metadata)
		select revision.id,null,1,'pending','PERF-012 reason','{}'
		from content_revisions revision`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `analyze content_revisions; analyze change_requests`); err != nil {
		t.Fatal(err)
	}

	args := []any{[]string{}, true, true, int64(2), "", "", "", "", 100, int64(999900)}
	counter.queries.Store(0)
	var itemsRaw, facetsRaw []byte
	var total int64
	if err = pool.QueryRow(ctx, contentReviewQueueQuery, args...).Scan(&itemsRaw, &total, &facetsRaw); err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load() != 1 {
		t.Fatalf("1M review queue executed %d SQL statements, want 1", counter.queries.Load())
	}
	var items []modContentReviewItem
	if err = json.Unmarshal(itemsRaw, &items); err != nil {
		t.Fatal(err)
	}
	firstID, lastID := "", ""
	if len(items) > 0 {
		firstID, lastID = items[0].ID, items[len(items)-1].ID
	}
	if total != 1000000 || len(items) != 100 || firstID != "q00999901" || lastID != "q01000000" {
		t.Fatalf("1M queue result changed: total=%d items=%d first/last=%q/%q facets=%s",
			total, len(items), firstID, lastID, facetsRaw)
	}

	var rawPlan []byte
	if err = pool.QueryRow(ctx, `explain (analyze,buffers,format json) `+contentReviewQueueQuery, args...).Scan(&rawPlan); err != nil {
		t.Fatal(err)
	}
	facts, err := reviewQueueScalePlanFacts(rawPlan)
	if err != nil {
		t.Fatal(err)
	}
	if facts.RevisionRowsExamined > 1100000 || facts.RequestRowsExamined > 1100000 {
		t.Fatalf("1M queue repeatedly scanned base rows: revisions=%.0f requests=%.0f",
			facts.RevisionRowsExamined, facts.RequestRowsExamined)
	}
	if facts.ExecutionMS > 10000 {
		t.Fatalf("1M queue execution %.3fms exceeds 10s regression ceiling", facts.ExecutionMS)
	}
	t.Logf("execution_ms=%.3f revisions_examined=%.0f requests_examined=%.0f temp_read=%.0f temp_written=%.0f",
		facts.ExecutionMS, facts.RevisionRowsExamined, facts.RequestRowsExamined, facts.TempReadBlocks, facts.TempWrittenBlocks)
}

func createReviewQueueScaleShadowTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `create temp table content_revisions(
		id bigint primary key,public_id text not null,entity_type text,entity_id bigint,aggregate_type text not null,
		aggregate_key text not null,revision_no bigint not null,created_at timestamptz not null);
		create index review_scale_revision_aggregate on content_revisions(aggregate_type,aggregate_key,revision_no desc);
		create temp table change_requests(
		proposed_revision_id bigint not null unique,base_revision_id bigint,submitted_by bigint,status text not null,reason text not null,metadata jsonb not null);
		create index review_scale_request_status on change_requests(status,proposed_revision_id);
		create temp table mods(id bigint primary key,project_code text,slug text,primary_name text);
		create temp table modpacks(id bigint primary key,public_id text,slug text,primary_name text);
		create temp table simple_projects(id bigint primary key,project_type text,public_id text,slug text,primary_name text);
		create temp table blueprints(public_id text primary key,title text);
		create temp table content_change_items(id bigserial primary key,revision_id bigint,path text,before_value jsonb,after_value jsonb);
		create temp table skin_assets(public_id text primary key,display_name text);
		create temp table creators(public_id text primary key,name text,kind text);
		create temp table community_posts(public_id text primary key,kind text,title text);
		create temp table project_changelogs(public_id text primary key,object_route_id bigint,project_version text);
		create temp table public_routes(id bigint primary key,entity_type text,public_id text,canonical_path text,internal_id bigint);
		create temp table minecraft_servers(id bigint primary key,name text);
		create temp table catalog_import_revisions(
		id text primary key,mod_id bigint,submitted_by bigint,minecraft_version text,loader text,source_namespace text,
		revision_no bigint,created_at timestamptz,status text,is_active boolean);
		create temp table users(id bigint primary key,public_id text,username text)`); err != nil {
		t.Fatal(err)
	}
}

type reviewQueueScaleFacts struct {
	ExecutionMS          float64
	RevisionRowsExamined float64
	RequestRowsExamined  float64
	TempReadBlocks       float64
	TempWrittenBlocks    float64
}

func reviewQueueScalePlanFacts(raw []byte) (reviewQueueScaleFacts, error) {
	var documents []map[string]any
	if err := json.Unmarshal(raw, &documents); err != nil {
		return reviewQueueScaleFacts{}, fmt.Errorf("decode review queue plan: %w", err)
	}
	if len(documents) != 1 {
		return reviewQueueScaleFacts{}, fmt.Errorf("decode review queue plan: got %d documents, want 1", len(documents))
	}
	facts := reviewQueueScaleFacts{ExecutionMS: reviewQueueNumber(documents[0]["Execution Time"])}
	root, _ := documents[0]["Plan"].(map[string]any)
	facts.TempReadBlocks = reviewQueueNumber(root["Temp Read Blocks"])
	facts.TempWrittenBlocks = reviewQueueNumber(root["Temp Written Blocks"])
	var walk func(map[string]any)
	walk = func(node map[string]any) {
		relation, _ := node["Relation Name"].(string)
		loops := reviewQueueNumber(node["Actual Loops"])
		if loops == 0 {
			loops = 1
		}
		examined := (reviewQueueNumber(node["Actual Rows"]) + reviewQueueNumber(node["Rows Removed by Filter"])) * loops
		switch relation {
		case "content_revisions":
			facts.RevisionRowsExamined += examined
		case "change_requests":
			facts.RequestRowsExamined += examined
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(root)
	return facts, nil
}

func reviewQueueNumber(value any) float64 {
	number, _ := value.(float64)
	return number
}
