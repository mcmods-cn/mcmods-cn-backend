package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditIdentityOSSState struct {
	Database              string          `json:"database"`
	Generation            int             `json:"generation"`
	Columns               map[string]bool `json:"columns"`
	UnverifiedActiveFiles int64           `json:"unverified_active_files"`
}

func inspectAuditIdentityOSS(ctx context.Context, q schemaQueryer) (AuditIdentityOSSState, error) {
	state := AuditIdentityOSSState{Columns: map[string]bool{}}
	if err := q.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).Scan(&state.Database, &state.Generation); err != nil {
		return state, err
	}
	if state.Generation != schemaGeneration {
		return state, fmt.Errorf("identity and OSS audit requires schema generation %d", schemaGeneration)
	}
	for table, column := range map[string]string{"oss_files": "source_hash_verified", "oss_object_deletion_outbox": "public_id", "oss_rehome_jobs": "public_id", "oss_multipart_sessions": "public_id"} {
		var present bool
		if err := q.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name=$1 and column_name=$2)`, table, column).Scan(&present); err != nil {
			return state, err
		}
		state.Columns[table+"."+column] = present
		if present {
			var dataType, nullable, defaultValue string
			if err := q.QueryRow(ctx, `select data_type,is_nullable,coalesce(column_default,'') from information_schema.columns where table_schema='public' and table_name=$1 and column_name=$2`, table, column).Scan(&dataType, &nullable, &defaultValue); err != nil {
				return state, err
			}
			if nullable != "NO" || (table == "oss_files" && (dataType != "boolean" || defaultValue != "false")) || (table != "oss_files" && (dataType != "text" || !strings.Contains(defaultValue, "new_public_id()"))) {
				return state, fmt.Errorf("incompatible identity/OSS column definition: %s.%s", table, column)
			}
			if table != "oss_files" {
				var unique bool
				if err := q.QueryRow(ctx, `select exists(select 1 from pg_index index_row join pg_attribute attribute on attribute.attrelid=index_row.indrelid and attribute.attname=$2 where index_row.indrelid=to_regclass('public.'||$1) and index_row.indisunique and index_row.indisvalid and index_row.indisready and index_row.indpred is null and index_row.indexprs is null and index_row.indnkeyatts=1 and index_row.indkey[0]=attribute.attnum)`, table, column).Scan(&unique); err != nil {
					return state, err
				}
				if !unique {
					return state, fmt.Errorf("public OSS queue identity is missing its unique constraint: %s", table)
				}
			}
		}

	}
	var storedHashPresent bool
	if err := q.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name='oss_files' and column_name='stored_sha256')`).Scan(&storedHashPresent); err != nil {
		return state, err
	}
	state.Columns["oss_files.stored_sha256"] = storedHashPresent
	if storedHashPresent {
		var valid bool
		if err := q.QueryRow(ctx, `select data_type='text' and is_nullable='NO' and column_default=$1 from information_schema.columns where table_schema='public' and table_name='oss_files' and column_name='stored_sha256'`, "''::text").Scan(&valid); err != nil {
			return state, err
		}
		if !valid {
			return state, errors.New("incompatible stored byte-hash column definition")
		}
	}

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
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AuditIdentityOSSState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select set_config('search_path',$1,true)`, applicationSearchPath); err != nil {
		return AuditIdentityOSSState{}, err
	}
	state, err := inspectAuditIdentityOSS(ctx, tx)
	if err != nil {
		return state, err
	}
	return state, tx.Commit(ctx)
}
