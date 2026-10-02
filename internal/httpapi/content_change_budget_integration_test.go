package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestContentChangeStorageStaysOneBatchAndBoundedForLargeJSONIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify bounded content change storage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
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
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table content_revisions(id bigint primary key,snapshot jsonb not null);
		create temporary table content_change_items(
			id bigint generated always as identity primary key,revision_id bigint not null,path text not null,operation text not null,
			before_value jsonb,after_value jsonb)`); err != nil {
		t.Fatal(err)
	}

	arrayBefore := make([]any, 10000)
	arrayAfter := make([]any, 10000)
	beforeFields := make(map[string]any, 1000)
	afterFields := make(map[string]any, 1000)
	for index := range arrayBefore {
		arrayBefore[index] = strings.Repeat("a", 128)
		arrayAfter[index] = strings.Repeat("b", 128)
	}
	for index := 0; index < 1000; index++ {
		key := fmt.Sprintf("field%04d", index)
		beforeFields[key] = index
		afterFields[key] = index + 1
	}
	largeBefore, err := json.Marshal(map[string]any{"array": arrayBefore, "fields": beforeFields})
	if err != nil {
		t.Fatal(err)
	}
	largeAfter, err := json.Marshal(map[string]any{"array": arrayAfter, "fields": afterFields})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_revisions values(1,'{"a":1}'::jsonb),(2,$1::jsonb)`, string(largeBefore)); err != nil {
		t.Fatal(err)
	}

	store := func(revisionID, baseID int64, snapshot []byte, commit bool) int64 {
		t.Helper()
		tx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		defer tx.Rollback(ctx)
		counter.queries.Store(0)
		if storeErr := storeContentChangesTx(ctx, tx, revisionID, &baseID, snapshot); storeErr != nil {
			t.Fatal(storeErr)
		}
		queries := counter.queries.Load()
		if commit {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				t.Fatal(commitErr)
			}
		}
		return queries
	}
	smallQueries := store(10, 1, []byte(`{"a":2,"b":3}`), false)
	largeQueries := store(20, 2, largeAfter, true)
	if smallQueries != 2 || largeQueries != smallQueries {
		t.Fatalf("content change SQL small=%d large=%d want constant 2 (base read + batch insert)", smallQueries, largeQueries)
	}

	var rows, storedBytes, rootSummaries, arraySummaries, storedArrays int64
	if err = pool.QueryRow(ctx, `select count(*),
		coalesce(sum(coalesce(pg_column_size(before_value),0)+coalesce(pg_column_size(after_value),0)),0),
		count(*) filter(where path='/' and before_value->>'$summary'='object' and after_value->>'$summary'='object'),
		count(*) filter(where path='/array' and before_value->>'$summary'='array' and after_value->>'$summary'='array'
			and (before_value->>'items')::int=10000 and (after_value->>'items')::int=10000),
		count(*) filter(where jsonb_typeof(before_value)='array' or jsonb_typeof(after_value)='array')
		from content_change_items where revision_id=20`).Scan(&rows, &storedBytes, &rootSummaries, &arraySummaries, &storedArrays); err != nil {
		t.Fatal(err)
	}
	if rows != maximumStoredContentChangeItems || storedBytes > 128<<10 || rootSummaries != 1 || arraySummaries != 1 || storedArrays != 0 {
		t.Fatalf("rows=%d bytes=%d rootSummaries=%d arraySummaries=%d storedArrays=%d",
			rows, storedBytes, rootSummaries, arraySummaries, storedArrays)
	}
	t.Logf("PERF055 large snapshots=%d/%d bytes stored change rows=%d bytes=%d SQL=%d",
		len(largeBefore), len(largeAfter), rows, storedBytes, largeQueries)
}
