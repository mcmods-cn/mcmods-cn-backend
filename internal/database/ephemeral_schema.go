package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InstallEphemeralSchema installs the current schema in the PostgreSQL
// session's temporary namespace. It is intended for integration tests that
// must exercise committed multi-transaction call paths without creating a
// database or leaving any persistent objects behind. The caller must provide
// a one-connection pool and close it after the test.
func InstallEphemeralSchema(ctx context.Context, db *pgxpool.Pool) error {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		return errors.New("ephemeral schema installation is restricted to database integration tests")
	}
	if db == nil {
		return errors.New("ephemeral schema requires a database pool")
	}
	if db.Config().MaxConns != 1 {
		return fmt.Errorf("ephemeral schema requires MaxConns=1, got %d", db.Config().MaxConns)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire ephemeral schema connection: %w", err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `create temporary table ephemeral_schema_bootstrap(value boolean)`); err != nil {
		return fmt.Errorf("materialize ephemeral schema: %w", err)
	}
	var namespace string
	if err = conn.QueryRow(ctx, `select nspname from pg_namespace where oid=pg_my_temp_schema()`).Scan(&namespace); err != nil {
		return fmt.Errorf("resolve ephemeral schema: %w", err)
	}
	if _, err = conn.Exec(ctx, `select set_config('search_path',quote_ident($1)||',pg_catalog',false)`, namespace); err != nil {
		return fmt.Errorf("select ephemeral schema: %w", err)
	}
	if _, err = conn.Exec(ctx, `drop table ephemeral_schema_bootstrap`); err != nil {
		return fmt.Errorf("remove ephemeral schema bootstrap: %w", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ephemeral schema installation: %w", err)
	}
	defer tx.Rollback(ctx)
	statements := schemaInstallationStatements()
	functionNames := ephemeralSchemaFunctionNames(statements)
	for _, raw := range statements {
		statement := ephemeralTableDeclarationPattern.ReplaceAllString(raw, "create temporary table")
		statement = strings.ReplaceAll(statement, "on public.%I", "on %I")
		statement = strings.ReplaceAll(statement, "namespace_row.nspname='public'", "namespace_row.oid=pg_my_temp_schema()")
		for _, name := range functionNames {
			call := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\s*\(`)
			statement = call.ReplaceAllString(statement, namespace+"."+name+"(")
		}
		if _, err = tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("install ephemeral schema generation %d: %w", schemaGeneration, err)
		}
	}
	if _, err = tx.Exec(ctx, `create temporary table schema_metadata (
		singleton boolean primary key default true check(singleton),
		generation integer not null,
		installed_at timestamptz not null default now()
	)`); err != nil {
		return fmt.Errorf("create ephemeral schema metadata: %w", err)
	}
	if _, err = tx.Exec(ctx, `insert into schema_metadata(singleton,generation) values(true,$1)`, schemaGeneration); err != nil {
		return fmt.Errorf("record ephemeral schema generation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ephemeral schema generation: %w", err)
	}
	return nil
}

var ephemeralFunctionDeclarationPattern = regexp.MustCompile(`(?i)create\s+(?:or\s+replace\s+)?function\s+([a-z_][a-z0-9_]*)\s*\(`)
var ephemeralTableDeclarationPattern = regexp.MustCompile(`(?i)\bcreate\s+table\b`)

func ephemeralSchemaFunctionNames(statements []string) []string {
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, statement := range statements {
		matches := ephemeralFunctionDeclarationPattern.FindAllStringSubmatch(statement, -1)
		for _, match := range matches {
			name := strings.ToLower(match[1])
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}

// DropEphemeralSchema removes every relation and routine installed by
// InstallEphemeralSchema from the current one-connection pool session.
func DropEphemeralSchema(ctx context.Context, db *pgxpool.Pool) error {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		return errors.New("ephemeral schema cleanup is restricted to database integration tests")
	}
	if db == nil || db.Config().MaxConns != 1 {
		return errors.New("ephemeral schema cleanup requires a one-connection database pool")
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire ephemeral schema cleanup connection: %w", err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `do $$
	declare object record; target_namespace oid := pg_my_temp_schema();
	begin
		for object in
			select namespace_row.nspname,relation.relname,
				case relation.relkind
					when 'v' then 'view'
					when 'm' then 'materialized view'
					when 'S' then 'sequence'
					when 'f' then 'foreign table'
					else 'table'
				end object_kind
			from pg_class relation
			join pg_namespace namespace_row on namespace_row.oid=relation.relnamespace
			where relation.relnamespace=target_namespace
			  and relation.relkind in ('r','p','v','m','S','f')
			order by case relation.relkind when 'v' then 0 when 'm' then 0 else 1 end,relation.relname
		loop
			execute format('drop %s if exists %I.%I cascade',object.object_kind,object.nspname,object.relname);
		end loop;
		for object in
			select namespace_row.nspname,routine.proname,pg_get_function_identity_arguments(routine.oid) arguments,
				case routine.prokind when 'p' then 'procedure' else 'function' end object_kind
			from pg_proc routine join pg_namespace namespace_row on namespace_row.oid=routine.pronamespace
			where routine.pronamespace=target_namespace
			order by routine.proname,routine.oid
		loop
			execute format('drop %s if exists %I.%I(%s) cascade',object.object_kind,object.nspname,object.proname,object.arguments);
		end loop;
	end $$`); err != nil {
		return fmt.Errorf("drop ephemeral schema objects: %w", err)
	}
	if _, err = conn.Exec(ctx, `select set_config('search_path','pg_catalog,public',false)`); err != nil {
		return fmt.Errorf("restore schema search path: %w", err)
	}
	return nil
}
