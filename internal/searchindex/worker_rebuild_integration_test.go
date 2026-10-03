package searchindex

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestLoadProjectDocumentsUsesBatchedPreaggregationsIntegration(t *testing.T) {
	pool, ctx := openSearchRebuildIntegrationPool(t, 30*time.Second)
	if _, err := pool.Exec(ctx, `
		create temporary table mods (
			id bigint primary key, project_code text not null, slug text not null,
			primary_name text not null, secondary_name text not null, abbreviation text not null,
			summary text not null, body_markdown text not null, search_keywords text[] not null,
			review_status text not null, submitted_by bigint, updated_at timestamptz not null
		);
		create temporary table content_localizations (
			subject_type text not null, subject_id bigint not null, locale text not null,
			name text not null, summary text not null, content_markdown text not null
		);
		create temporary table mod_identifiers (
			id bigint primary key, mod_id bigint not null, identifier text not null, display_order integer not null
		);
		create temporary table content_creator_bindings (
			subject_type text not null, subject_id bigint not null, creator_id bigint not null
		);
		create temporary table creators (id bigint primary key, name text not null);
		create temporary table mod_tags (mod_id bigint not null, tag text not null);
		create temporary table mod_loader_compatibilities (
			mod_id bigint not null, loader text not null, minecraft_version text not null
		);
		insert into mods values (
			42,'perf037a','perf-037','Primary','Secondary','P37','Summary','Body',
			array['search-one','search-two'],'approved',7,timestamptz '2025-02-03 04:05:06+00'
		);
		insert into content_localizations values
			('mod',42,'en','English name','English summary','English body'),
			('mod',42,'zh-CN','Chinese name','Chinese summary','Chinese body');
		insert into mod_identifiers values (1,42,'alpha',1),(2,42,'beta',2);
		insert into creators values (1,'Alice'),(2,'Bob');
		insert into content_creator_bindings values ('mod',42,1),('mod',42,2);
		insert into mod_tags values (42,'library'),(42,'technology');
		insert into mod_loader_compatibilities values
			(42,'fabric','1.21.1'),(42,'forge','1.20.1'),(42,'forge','1.21.1')`); err != nil {
		t.Fatal(err)
	}

	worker := NewWorker(pool, nil)
	ids, err := worker.loadDocumentIDPage(ctx, "mod", 0, searchRebuildPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != "[42]" {
		t.Fatalf("ID page = %v, want [42]", ids)
	}
	documents, err := worker.loadTypedDocuments(ctx, "mod", ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 {
		t.Fatalf("project documents = %d, want 1", len(documents))
	}
	document := documents[0]
	for field, expected := range map[string][]string{
		"names":              {"Primary", "Secondary", "P37", "English name", "Chinese name"},
		"identifiers":        {"alpha", "beta"},
		"creators":           {"Alice", "Bob"},
		"categories":         {"library", "technology"},
		"minecraft_versions": {"1.20.1", "1.21.1"},
		"loaders":            {"fabric", "forge"},
	} {
		actual, ok := document[field].([]string)
		if !ok || fmt.Sprint(actual) != fmt.Sprint(expected) {
			t.Errorf("%s = %#v, want %#v", field, document[field], expected)
		}
	}
	if _, err = pool.Exec(ctx, `update mods set secondary_name='',abbreviation='',summary='' where id=42`); err != nil {
		t.Fatal(err)
	}
	documents, err = worker.loadTypedDocuments(ctx, "mod", ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 {
		t.Fatalf("project documents = %d, want 1", len(documents))
	}
	if got := fmt.Sprint(documents[0]["names"]); got != "[Primary English name Chinese name]" {
		t.Fatalf("optional empty names hid localizations: %s", got)
	}
	if got := fmt.Sprint(documents[0]["text"]); got != "[Body English summary English body Chinese summary Chinese body]" {
		t.Fatalf("empty summary hid body text: %s", got)
	}
}

func TestSearchRebuildKeysetPagingScaleIntegration(t *testing.T) {
	pool, ctx := openSearchRebuildIntegrationPool(t, 2*time.Minute)
	if _, err := pool.Exec(ctx, `create temporary table mods (id bigint primary key)`); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(pool, nil)
	query, err := searchDocumentIDPageQuery("mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range []int{100_000, 1_000_000} {
		if _, err = pool.Exec(ctx, `truncate mods`); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into mods select generate_series(1,$1)`, scale); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `analyze mods`); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		var afterID int64
		total := 0
		for {
			ids, pageErr := worker.loadDocumentIDPage(ctx, "mod", afterID, searchRebuildPageSize)
			if pageErr != nil {
				t.Fatal(pageErr)
			}
			if len(ids) > searchRebuildPageSize {
				t.Fatalf("%d-row scale returned oversized page %d", scale, len(ids))
			}
			if len(ids) == 0 {
				break
			}
			total += len(ids)
			afterID = ids[len(ids)-1]
		}
		if total != scale || afterID != int64(scale) {
			t.Fatalf("%d-row scale traversed %d rows through ID %d", scale, total, afterID)
		}
		var plan string
		rows, queryErr := pool.Query(ctx, `explain (analyze,format text) `+query, scale/2, searchRebuildPageSize)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		for rows.Next() {
			var line string
			if queryErr = rows.Scan(&line); queryErr != nil {
				rows.Close()
				t.Fatal(queryErr)
			}
			plan += line + "\n"
		}
		rows.Close()
		if queryErr = rows.Err(); queryErr != nil {
			t.Fatal(queryErr)
		}
		if strings.Contains(plan, "Seq Scan") || (!strings.Contains(plan, "Index Only Scan") && !strings.Contains(plan, "Index Scan")) {
			t.Fatalf("%d-row keyset plan did not use the primary-key index:\n%s", scale, plan)
		}
		t.Logf("PERF037 %d rows streamed in pages of at most %d in %s", scale, searchRebuildPageSize, time.Since(started))
	}
}

func openSearchRebuildIntegrationPool(t *testing.T, timeout time.Duration) (*pgxpool.Pool, context.Context) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate search rebuild paging against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
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
	t.Cleanup(pool.Close)
	return pool, ctx
}
