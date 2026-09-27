package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestStickerReferenceProjectionIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify sticker reference triggers")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `set local search_path=public,pg_temp`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `select column_info.table_name,column_info.column_name,
		exists(
			select 1 from pg_constraint constraint_row
			where constraint_row.contype='p'
			  and constraint_row.conrelid=format('%I.%I',column_info.table_schema,column_info.table_name)::regclass
		) has_primary_key
		from information_schema.columns column_info
		where column_info.table_schema='public'
		  and column_info.data_type in ('text','jsonb')
		  and (
			column_info.column_name like '%\_markdown' escape '\'
			or (column_info.table_name='comments' and column_info.column_name='body')
			or (column_info.table_name='content_revisions' and column_info.column_name='snapshot')
		  )
		order by column_info.table_name,column_info.column_name`)
	if err != nil {
		t.Fatal(err)
	}
	var publicSources []string
	for rows.Next() {
		var tableName, columnName string
		var hasPrimaryKey bool
		if err = rows.Scan(&tableName, &columnName, &hasPrimaryKey); err != nil {
			t.Fatal(err)
		}
		if !hasPrimaryKey {
			t.Fatalf("public sticker reference source %s.%s has no primary key", tableName, columnName)
		}
		publicSources = append(publicSources, tableName+"."+columnName)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(publicSources) < 10 {
		t.Fatalf("only %d public Markdown/history source columns were discovered: %v", len(publicSources), publicSources)
	}
	t.Logf("validated %d public Markdown/history source columns: %v", len(publicSources), publicSources)

	statements := stickerReferenceSchemaStatements()
	temporaryReferenceTable := strings.Replace(statements[0], "create table", "create temp table", 1)
	for _, statement := range []string{temporaryReferenceTable, statements[1], statements[2], statements[3]} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatalf("install temporary sticker reference contract: %v", err)
		}
	}
	for _, statement := range []string{
		`create temp table sticker_reference_current_test(
			id bigint primary key,body_markdown text not null
		) on commit drop`,
		`create temp table content_revisions(
			id bigint primary key,snapshot jsonb not null
		) on commit drop`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(ctx, `set local search_path=pg_temp,public`); err != nil {
		t.Fatal(err)
	}
	installer := strings.Replace(statements[4], "column_info.table_schema='public'", "column_info.table_schema like 'pg_temp_%'", 1)
	installer = strings.ReplaceAll(installer, "on public.%I", "on %I")
	if _, err = tx.Exec(ctx, installer); err != nil {
		t.Fatalf("install generated temporary triggers: %v", err)
	}

	if _, err = tx.Exec(ctx, `insert into sticker_reference_current_test(id,body_markdown)
		values(1,'[sticker:animals:happy] duplicate [sticker:animals:happy]')`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into content_revisions(id,snapshot)
		values(9,$1::jsonb)`, `{"body":"[sticker:history:kept]"}`); err != nil {
		t.Fatal(err)
	}
	assertStickerReferenceCount(t, ctx, tx, "animals", "happy", 1)
	assertStickerReferenceCount(t, ctx, tx, "history", "kept", 1)
	if _, err = tx.Exec(ctx, `set local enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := tx.Query(ctx, `explain select exists(select 1 from sticker_content_references
		where pack_code='history' and sticker_code='kept')`)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	planRows.Close()
	if !strings.Contains(plan.String(), "idx_sticker_content_references_token") {
		t.Fatalf("sticker lookup did not use the token index:\n%s", plan.String())
	}

	if _, err = tx.Exec(ctx, `update sticker_reference_current_test
		set body_markdown='[sticker:animals:sad]' where id=1`); err != nil {
		t.Fatal(err)
	}
	assertStickerReferenceCount(t, ctx, tx, "animals", "happy", 0)
	assertStickerReferenceCount(t, ctx, tx, "animals", "sad", 1)
	if _, err = tx.Exec(ctx, `delete from sticker_reference_current_test where id=1`); err != nil {
		t.Fatal(err)
	}
	assertStickerReferenceCount(t, ctx, tx, "animals", "sad", 0)
	assertStickerReferenceCount(t, ctx, tx, "history", "kept", 1)

	contender, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer contender.Rollback(context.Background())
	lockContext, cancelLock := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancelLock()
	if _, lockErr := contender.Exec(lockContext, `select pg_advisory_xact_lock(
		hashtext('sticker-reference'),hashtext('history:kept'))`); lockErr == nil {
		t.Fatal("reference trigger did not hold the shared sticker deletion lock")
	}
}

func assertStickerReferenceCount(t *testing.T, ctx context.Context, queryer pgx.Tx, packCode, code string, want int) {
	t.Helper()
	var got int
	if err := queryer.QueryRow(ctx, `select count(*) from sticker_content_references
		where pack_code=$1 and sticker_code=$2`, packCode, code).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("reference count for %s/%s = %d, want %d", packCode, code, got, want)
	}
}
