package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LogRedactionSchemaState contains schema metadata only, never shared log text.
// The record is evidence of the pre-change schema, not a database data backup.
type LogRedactionSchemaState struct {
	Version    int    `json:"version"`
	Database   string `json:"database"`
	Generation int    `json:"generation"`
	Present    bool   `json:"column_present"`
	Validated  bool   `json:"constraint_validated"`
}

type logRedactionQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func inspectLogRedactionSchema(ctx context.Context, queryer logRedactionQueryer) (LogRedactionSchemaState, error) {
	state := LogRedactionSchemaState{Version: 1}
	if err := queryer.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).
		Scan(&state.Database, &state.Generation); err != nil {
		return state, fmt.Errorf("inspect log redaction schema generation: %w", err)
	}
	if state.Generation != schemaGeneration {
		return state, errors.New("log redaction upgrade requires schema generation 168")
	}
	var tableExists bool
	if err := queryer.QueryRow(ctx, `select exists(select 1 from pg_class
		where oid=to_regclass('public.log_shares') and relkind='r')`).Scan(&tableExists); err != nil {
		return state, fmt.Errorf("inspect log shares table: %w", err)
	}
	if !tableExists {
		return state, errors.New("generation 168 log shares table is missing")
	}
	var validDefinition bool
	err := queryer.QueryRow(ctx, `select attribute.atttypid='integer'::regtype and attribute.attnotnull
		and attribute.attgenerated='' and attribute.attidentity=''
		and pg_get_expr(default_value.adbin,default_value.adrelid)='1'
		from pg_attribute attribute left join pg_attrdef default_value
		on default_value.adrelid=attribute.attrelid and default_value.adnum=attribute.attnum
		where attribute.attrelid='public.log_shares'::regclass
		and attribute.attname='redaction_applied_version' and not attribute.attisdropped`).Scan(&validDefinition)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("inspect applied redaction version: %w", err)
	}
	state.Present = true
	if !validDefinition {
		return state, errors.New("applied redaction version has an incompatible column definition")
	}
	var validCheck bool
	if err := queryer.QueryRow(ctx, `select convalidated,
		contype='c' and pg_get_expr(conbin,conrelid)='(redaction_applied_version >= 1)'
		from pg_constraint where conrelid='public.log_shares'::regclass
		and conname='log_shares_redaction_applied_version_check'`).Scan(&state.Validated, &validCheck); err != nil {
		return state, fmt.Errorf("inspect applied redaction version constraint: %w", err)
	}
	if !validCheck {
		return state, errors.New("applied redaction version has an incompatible check constraint")
	}
	return state, nil
}

func InspectLogRedactionSchema(ctx context.Context, pool *pgxpool.Pool) (LogRedactionSchemaState, error) {
	return inspectLogRedactionSchema(ctx, pool)
}

func requireLogRedactionSchema(ctx context.Context, queryer logRedactionQueryer) error {
	state, err := inspectLogRedactionSchema(ctx, queryer)
	if err != nil {
		return err
	}
	if !state.Present || !state.Validated {
		return errors.New("log redaction schema upgrade required: run cmd/db-log-redaction-upgrade explicitly before starting this backend")
	}
	return nil
}

// ApplyLogRedactionSchemaUpgrade expands only the generation-168 log table.
// ADD COLUMN and NOT VALID check commit together; validation uses a second
// transaction so the table scan does not retain the ADD COLUMN exclusive lock.
// If validation is interrupted, rerunning resumes it. No down operation drops
// the new marker, and existing log rows and the original deduplication identity
// remain intact. The caller must persist the supplied metadata before any DDL.
func ApplyLogRedactionSchemaUpgrade(ctx context.Context, pool *pgxpool.Pool, confirmedDatabase string,
	persistState func(LogRedactionSchemaState) error) error {
	if confirmedDatabase == "" || persistState == nil {
		return errors.New("log redaction upgrade requires an exact target and a metadata backup writer")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire log redaction upgrade connection: %w", err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `select pg_advisory_lock(hashtext('mcmods-cn-schema-migrations'))`); err != nil {
		return fmt.Errorf("lock log redaction upgrade: %w", err)
	}
	defer conn.Exec(context.Background(), `select pg_advisory_unlock(hashtext('mcmods-cn-schema-migrations'))`)
	state, err := inspectLogRedactionSchema(ctx, conn)
	if err != nil {
		return err
	}
	if state.Database != confirmedDatabase {
		return errors.New("log redaction confirmation does not match the connected database")
	}
	if err = persistState(state); err != nil {
		return fmt.Errorf("persist pre-upgrade schema metadata: %w", err)
	}
	if !state.Present {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin applied redaction column installation: %w", err)
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(ctx, `alter table public.log_shares add column redaction_applied_version integer not null default 1,
			add constraint log_shares_redaction_applied_version_check check(redaction_applied_version>=1) not valid`); err != nil {
			return fmt.Errorf("install applied redaction version: %w", err)
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit applied redaction column installation: %w", err)
		}
	}
	if !state.Validated {
		if _, err = conn.Exec(ctx, `alter table public.log_shares validate constraint log_shares_redaction_applied_version_check`); err != nil {
			return fmt.Errorf("validate applied redaction version; column is installed and validation must be resumed: %w", err)
		}
	}
	return requireLogRedactionSchema(ctx, conn)
}
