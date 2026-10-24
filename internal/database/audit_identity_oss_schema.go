package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Expansion only: old hashes remain untrusted until their source bytes are
// independently reverified. No balances, assets, audit rows or references change.
func auditIdentityOSSSchemaStatements() []string {
	return []string{
		`alter table oss_files add column if not exists source_hash_verified boolean not null default false`,
		`alter table oss_files add column if not exists stored_sha256 text not null default '' check(stored_sha256='' or stored_sha256 ~ '^[0-9a-f]{64}$')`,
		`alter table oss_object_deletion_outbox add column if not exists public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$')`,
		`alter table oss_rehome_jobs add column if not exists public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$')`,
		`alter table oss_multipart_sessions add column if not exists public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$')`,
	}
}

type AuditIdentityOSSState struct {
	Database              string          `json:"database"`
	Generation            int             `json:"generation"`
	Columns               map[string]bool `json:"columns"`
	UnverifiedActiveFiles int64           `json:"unverified_active_files"`
}

type auditIdentityOSSQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func inspectAuditIdentityOSS(ctx context.Context, q auditIdentityOSSQuery) (AuditIdentityOSSState, error) {
	state := AuditIdentityOSSState{Columns: map[string]bool{}}
	if err := q.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).Scan(&state.Database, &state.Generation); err != nil {
		return state, err
	}
	if state.Generation != schemaGeneration {
		return state, errors.New("identity/OSS expansion requires generation 168")
	}
	for table, column := range map[string]string{"oss_files": "source_hash_verified", "oss_object_deletion_outbox": "public_id", "oss_rehome_jobs": "public_id", "oss_multipart_sessions": "public_id"} {
		var present bool
		if err := q.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name=$1 and column_name=$2)`, table, column).Scan(&present); err != nil {
			return state, err
		}
		state.Columns[table+"."+column] = present
	}
	var storedHashPresent bool
	if err := q.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name='oss_files' and column_name='stored_sha256')`).Scan(&storedHashPresent); err != nil {
		return state, err
	}
	state.Columns["oss_files.stored_sha256"] = storedHashPresent
	if state.Columns["oss_files.source_hash_verified"] {
		if err := q.QueryRow(ctx, `select count(*) from public.oss_files where status='active' and not source_hash_verified`).Scan(&state.UnverifiedActiveFiles); err != nil {
			return state, err
		}
	} else if err := q.QueryRow(ctx, `select count(*) from public.oss_files where status='active'`).Scan(&state.UnverifiedActiveFiles); err != nil {
		return state, err
	}
	return state, nil
}

func InspectAuditIdentityOSS(ctx context.Context, pool *pgxpool.Pool) (AuditIdentityOSSState, error) {
	return inspectAuditIdentityOSS(ctx, pool)
}

// ApplyAuditIdentityOSSUpgrade requires a caller-supplied exact database name and
// recorded backup reference. The reference is an operator assertion, not a backup
// made by this tool. All expansion statements commit together and are repeatable.
func ApplyAuditIdentityOSSUpgrade(ctx context.Context, pool *pgxpool.Pool, expectedDatabase, backupReference string) (AuditIdentityOSSState, error) {
	if strings.TrimSpace(expectedDatabase) == "" || strings.TrimSpace(backupReference) == "" {
		return AuditIdentityOSSState{}, errors.New("exact database name and backup reference are required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return AuditIdentityOSSState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `set local search_path=public`); err != nil {
		return AuditIdentityOSSState{}, err
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods:audit-identity-oss-expansion-v1'))`); err != nil {
		return AuditIdentityOSSState{}, err
	}
	before, err := inspectAuditIdentityOSS(ctx, tx)
	if err != nil {
		return before, err
	}
	if before.Database != expectedDatabase {
		return before, errors.New("database name does not match explicit confirmation")
	}
	for _, statement := range auditIdentityOSSSchemaStatements() {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return before, fmt.Errorf("expand identity/OSS schema: %w", err)
		}
	}
	after, err := inspectAuditIdentityOSS(ctx, tx)
	if err != nil {
		return after, err
	}
	if err = tx.Commit(ctx); err != nil {
		return after, err
	}
	return after, nil
}

func requireAuditIdentityOSSSchema(ctx context.Context, queryer auditIdentityOSSQuery) error {
	state, err := inspectAuditIdentityOSS(ctx, queryer)
	if err != nil {
		return err
	}
	for _, present := range state.Columns {
		if !present {
			return errors.New("identity/OSS schema expansion required: run cmd/audit-identity-oss explicitly before starting this backend")
		}
	}
	return nil
}
