package database_test

import (
	"context"
	"os"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestEveryForeignKeyHasLeadingIndex(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to inspect the development database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Connect(ctx, config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(ctx, `
		select source.relname, constraint_row.conname, pg_get_constraintdef(constraint_row.oid)
		from pg_constraint constraint_row
		join pg_class source on source.oid=constraint_row.conrelid
		join pg_namespace namespace_row on namespace_row.oid=source.relnamespace
		where constraint_row.contype='f'
		  and namespace_row.nspname='public'
		  and not exists (
			select 1
			from pg_index index_row
			where index_row.indrelid=constraint_row.conrelid
			  and index_row.indisvalid
			  and index_row.indisready
			  and index_row.indpred is null
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
}
