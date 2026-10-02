package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestUniqueConstraintsOwnCatalogLookupIndexesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify catalog index consolidation")
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
	var publicGenerationBefore int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationBefore); err != nil {
		t.Fatal(err)
	}
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned := false
	defer func() {
		if !cleaned {
			if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
				t.Errorf("drop ephemeral schema: %v", dropErr)
			}
		}
	}()
	var generation int
	if err = pool.QueryRow(ctx, `select generation from schema_metadata where singleton`).Scan(&generation); err != nil || generation != schemaGeneration {
		t.Fatalf("temporary schema generation=%d err=%v", generation, err)
	}

	type indexTarget struct {
		table, columns, query string
	}
	targets := []indexTarget{
		{table: "resource_import_snapshots", columns: "resource_id,revision_id",
			query: `select id from resource_import_snapshots where resource_id=1 and revision_id='missing'`},
		{table: "resource_import_snapshots", columns: "revision_id,registry,resource_id",
			query: `select id from resource_import_snapshots where revision_id='missing' and registry='items' order by resource_id limit 50`},
		{table: "recipe_layout_templates", columns: "recipe_type_id,template_key",
			query: `select entity_id from recipe_layout_templates where recipe_type_id=1 order by template_key`},
		{table: "mod_content_sections", columns: "version_id,parent_id,ordinal",
			query: `select id from mod_content_sections where version_id=1 and parent_id is null order by ordinal`},
	}
	if _, err = pool.Exec(ctx, `set enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		var count int
		var indexName string
		var unique, constraintOwned bool
		err = pool.QueryRow(ctx, `select count(*)::int,min(index_relation.relname),bool_and(index_definition.indisunique),
			bool_and(constraint_row.oid is not null)
			from pg_index index_definition
			join pg_class table_relation on table_relation.oid=index_definition.indrelid
			join pg_namespace namespace on namespace.oid=table_relation.relnamespace
			join pg_class index_relation on index_relation.oid=index_definition.indexrelid
			left join pg_constraint constraint_row on constraint_row.conindid=index_definition.indexrelid and constraint_row.contype='u'
			where namespace.nspname=current_schema() and table_relation.relname=$1
			  and index_definition.indpred is null and index_definition.indexprs is null
			  and (select string_agg(attribute.attname,',' order by key.ordinality)
			       from unnest(index_definition.indkey) with ordinality key(attnum,ordinality)
			       join pg_attribute attribute on attribute.attrelid=table_relation.oid and attribute.attnum=key.attnum)=$2`,
			target.table, target.columns).Scan(&count, &indexName, &unique, &constraintOwned)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 || !unique || !constraintOwned {
			t.Fatalf("%s(%s) indexes=%d unique=%t constraintOwned=%t name=%q",
				target.table, target.columns, count, unique, constraintOwned, indexName)
		}
		planRows, planErr := pool.Query(ctx, "explain (format text) "+target.query)
		if planErr != nil {
			t.Fatal(planErr)
		}
		lines := make([]string, 0)
		for planRows.Next() {
			var line string
			if planErr = planRows.Scan(&line); planErr != nil {
				planRows.Close()
				t.Fatal(planErr)
			}
			lines = append(lines, line)
		}
		if planErr = planRows.Err(); planErr != nil {
			planRows.Close()
			t.Fatal(planErr)
		}
		planRows.Close()
		plan := strings.ToLower(strings.Join(lines, "\n"))
		if !strings.Contains(plan, "index") || strings.Contains(plan, "seq scan") {
			t.Fatalf("query lost its index-backed plan after retaining unique index %s:\n%s", indexName, plan)
		}
		t.Logf("%s(%s) retained constraint index=%s; lookup plan:\n%s", target.table, target.columns, indexName, plan)
	}

	for _, removed := range []string{
		"idx_resource_import_snapshots_resource",
		"idx_resource_import_snapshots_revision_registry",
		"idx_recipe_layout_templates_type",
		"idx_mod_content_sections_tree",
	} {
		var exists bool
		if err = pool.QueryRow(ctx, `select to_regclass(current_schema() || '.' || $1) is not null`, removed).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("redundant index %s still exists", removed)
		}
	}

	if err = DropEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned = true
	var publicGenerationAfter int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationAfter); err != nil {
		t.Fatal(err)
	}
	if publicGenerationAfter != publicGenerationBefore {
		t.Fatalf("public generation changed from %d to %d", publicGenerationBefore, publicGenerationAfter)
	}
}
