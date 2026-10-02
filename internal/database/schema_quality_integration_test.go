package database_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestEveryForeignKeyHasLeadingIndex(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to inspect the current ephemeral schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var publicGenerationBefore int
	if err = db.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationBefore); err != nil {
		t.Fatal(err)
	}
	if err = database.InstallEphemeralSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	cleaned := false
	defer func() {
		if !cleaned {
			if dropErr := database.DropEphemeralSchema(context.Background(), db); dropErr != nil {
				t.Errorf("drop ephemeral schema: %v", dropErr)
			}
		}
	}()

	rows, err := db.Query(ctx, `
		select source.relname, constraint_row.conname, pg_get_constraintdef(constraint_row.oid)
		from pg_constraint constraint_row
		join pg_class source on source.oid=constraint_row.conrelid
		join pg_namespace namespace_row on namespace_row.oid=source.relnamespace
		where constraint_row.contype='f'
		  and namespace_row.nspname=current_schema()
		  and not exists (
			select 1
			from pg_index index_row
			where index_row.indrelid=constraint_row.conrelid
			  and index_row.indisvalid
			  and index_row.indisready
			  and (
				index_row.indpred is null
				or (
				  cardinality(constraint_row.conkey)=1
				  and pg_get_expr(index_row.indpred,index_row.indrelid)=format(
					'(%I IS NOT NULL)',
					(select attribute.attname from pg_attribute attribute
					 where attribute.attrelid=constraint_row.conrelid
					   and attribute.attnum=constraint_row.conkey[1])
				  )
				)
			  )
			  and index_row.indexprs is null
			  and index_row.indnkeyatts>=cardinality(constraint_row.conkey)
			  and not exists (
				select 1
				from generate_subscripts(constraint_row.conkey,1) position
				where (index_row.indkey::smallint[])[position-1]<>constraint_row.conkey[position]
			  )
		  )
		order by source.relname,constraint_row.conname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type missingIndex struct {
		table      string
		constraint string
		definition string
	}
	missing := make([]missingIndex, 0)
	for rows.Next() {
		var item missingIndex
		if err := rows.Scan(&item.table, &item.constraint, &item.definition); err != nil {
			t.Fatal(err)
		}
		missing = append(missing, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, item := range missing {
		t.Errorf("foreign key lacks a leading index: %s.%s (%s)", item.table, item.constraint, item.definition)
	}

	var ownerIndexCount int
	var ownerIndexName, ownerPredicate string
	if err = db.QueryRow(ctx, `select count(*)::int,min(index_relation.relname),min(pg_get_expr(index_row.indpred,index_row.indrelid))
		from pg_index index_row
		join pg_class table_relation on table_relation.oid=index_row.indrelid
		join pg_namespace namespace_row on namespace_row.oid=table_relation.relnamespace
		join pg_class index_relation on index_relation.oid=index_row.indexrelid
		join pg_attribute owner_attribute on owner_attribute.attrelid=table_relation.oid
			and owner_attribute.attname='owner_user_id'
		where namespace_row.nspname=current_schema() and table_relation.relname='log_shares'
		  and (index_row.indkey::smallint[])[0]=owner_attribute.attnum`,
	).Scan(&ownerIndexCount, &ownerIndexName, &ownerPredicate); err != nil {
		t.Fatal(err)
	}
	if ownerIndexCount != 1 || ownerIndexName != "idx_log_shares_owner_created" || ownerPredicate != "(owner_user_id IS NOT NULL)" {
		t.Fatalf("log_shares owner indexes=%d name=%q predicate=%q", ownerIndexCount, ownerIndexName, ownerPredicate)
	}
	if _, err = db.Exec(ctx, `set enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := db.Query(ctx, `explain (format text) select id from log_shares where owner_user_id=1`)
	if err != nil {
		t.Fatal(err)
	}
	planLines := make([]string, 0)
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			planRows.Close()
			t.Fatal(err)
		}
		planLines = append(planLines, line)
	}
	if err = planRows.Err(); err != nil {
		planRows.Close()
		t.Fatal(err)
	}
	planRows.Close()
	plan := strings.ToLower(strings.Join(planLines, "\n"))
	if !strings.Contains(plan, "idx_log_shares_owner_created") || strings.Contains(plan, "seq scan") {
		t.Fatalf("owner foreign-key lookup did not use the retained partial prefix:\n%s", plan)
	}

	if err = database.DropEphemeralSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	cleaned = true
	var publicGenerationAfter int
	if err = db.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationAfter); err != nil {
		t.Fatal(err)
	}
	if publicGenerationAfter != publicGenerationBefore {
		t.Fatalf("public generation changed from %d to %d", publicGenerationBefore, publicGenerationAfter)
	}
}
