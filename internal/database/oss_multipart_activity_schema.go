package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL 18.6 is the repository's CI database. Compare its actual parsed
// expression while normalizing whitespace only: operator grouping must remain
// exact, and an identically named CHECK(true) must never satisfy preflight.
const ossMultipartAbortActivityExpectedExpression = `((jsonb_typeof(abort_activity_intent) = 'object'::text) AND (octet_length((abort_activity_intent)::text) <= 4096) AND ((abort_activity_intent = '{}'::jsonb) OR COALESCE((((abort_activity_intent ->> 'version'::text) = '1'::text) AND ((abort_activity_intent ->> 'kind'::text) = 'oss_multipart_abort'::text) AND (jsonb_typeof((abort_activity_intent -> 'event'::text)) = 'object'::text) AND (jsonb_typeof((abort_activity_intent -> 'id'::text)) = 'string'::text) AND (jsonb_typeof((abort_activity_intent -> 'queued'::text)) = 'boolean'::text)), false)))`

// The bounded envelope contains an original typed activity, never upload URLs,
// provider credentials, file bytes or HTTP cookies. Empty means no such intent.
func OSSMultipartActivitySchemaStatements() []string {
	return []string{
		`alter table oss_multipart_sessions add column if not exists abort_activity_intent jsonb not null default '{}'::jsonb`,
		`do $$ begin if not exists(select 1 from pg_constraint where conrelid='oss_multipart_sessions'::regclass and conname='oss_multipart_abort_activity_shape') then alter table oss_multipart_sessions add constraint oss_multipart_abort_activity_shape check(jsonb_typeof(abort_activity_intent)='object' and octet_length(abort_activity_intent::text)<=4096 and (abort_activity_intent='{}'::jsonb or coalesce((abort_activity_intent->>'version'='1' and abort_activity_intent->>'kind'='oss_multipart_abort' and jsonb_typeof(abort_activity_intent->'event')='object' and jsonb_typeof(abort_activity_intent->'id')='string' and jsonb_typeof(abort_activity_intent->'queued')='boolean'),false))); end if; end $$`,
	}
}

type OSSMultipartActivitySchemaState struct {
	Database              string `json:"database"`
	Generation            int    `json:"generation"`
	Present               bool   `json:"present"`
	PendingIntents        int64  `json:"pending_intents"`
	QueuedIntents         int64  `json:"queued_intents"`
	LegacyAbortedSessions int64  `json:"legacy_aborted_sessions_without_reconstructable_intent"`
}

type ossMultipartActivityQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func inspectOSSMultipartActivity(ctx context.Context, q ossMultipartActivityQuery, withCounts bool) (OSSMultipartActivitySchemaState, error) {
	var state OSSMultipartActivitySchemaState
	if err := q.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).Scan(&state.Database, &state.Generation); err != nil {
		return state, err
	}
	if state.Generation != schemaGeneration {
		return state, errors.New("OSS multipart activity expansion requires schema generation 168")
	}
	var compatible bool
	if err := q.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name='oss_multipart_sessions' and column_name='abort_activity_intent'),exists(select 1 from information_schema.columns where table_schema='public' and table_name='oss_multipart_sessions' and column_name='abort_activity_intent' and data_type='jsonb' and is_nullable='NO' and column_default=$1)`, "'{}'::jsonb").Scan(&state.Present, &compatible); err != nil {
		return state, err
	}
	if state.Present {
		var definition string
		if err := q.QueryRow(ctx, `select coalesce((select pg_get_expr(conbin,conrelid) from pg_constraint where conrelid='public.oss_multipart_sessions'::regclass and conname='oss_multipart_abort_activity_shape' and contype='c' and convalidated),'')`).Scan(&definition); err != nil {
			return state, err
		}
		constrained := strings.Join(strings.Fields(definition), " ") == strings.Join(strings.Fields(ossMultipartAbortActivityExpectedExpression), " ")
		if !compatible || !constrained {
			return state, errors.New("incompatible OSS multipart activity column or shape constraint")
		}
		if withCounts {
			if err := q.QueryRow(ctx, `select count(*) filter(where abort_activity_intent<>'{}'::jsonb and abort_activity_intent->>'queued'='false' and coalesce(abort_activity_intent->>'actorDeleted','false')<>'true'),count(*) filter(where abort_activity_intent->>'queued'='true'),count(*) filter(where status='aborted' and abort_activity_intent='{}'::jsonb) from public.oss_multipart_sessions`).Scan(&state.PendingIntents, &state.QueuedIntents, &state.LegacyAbortedSessions); err != nil {
				return state, err
			}
		}
	} else if withCounts {
		if err := q.QueryRow(ctx, `select count(*) from public.oss_multipart_sessions where status='aborted'`).Scan(&state.LegacyAbortedSessions); err != nil {
			return state, err
		}
	}
	return state, nil
}

func InspectOSSMultipartActivity(ctx context.Context, pool *pgxpool.Pool) (OSSMultipartActivitySchemaState, error) {
	return inspectOSSMultipartActivity(ctx, pool, true)
}

func RequireOSSMultipartActivitySchema(ctx context.Context, q ossMultipartActivityQuery) error {
	state, err := inspectOSSMultipartActivity(ctx, q, false)
	if err != nil {
		return err
	}
	if !state.Present {
		return errors.New("OSS multipart activity extension required: inspect and explicitly apply cmd/db-oss-multipart-activity-upgrade before starting this backend")
	}
	return nil
}

func ApplyOSSMultipartActivityUpgrade(ctx context.Context, pool *pgxpool.Pool, expectedDatabase, backupReference string) (OSSMultipartActivitySchemaState, error) {
	if strings.TrimSpace(expectedDatabase) == "" || strings.TrimSpace(backupReference) == "" {
		return OSSMultipartActivitySchemaState{}, errors.New("exact database name and recorded backup reference are required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return OSSMultipartActivitySchemaState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `set local search_path=pg_catalog,public,pg_temp`); err != nil {
		return OSSMultipartActivitySchemaState{}, err
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods:oss-multipart-activity-expansion-v1'))`); err != nil {
		return OSSMultipartActivitySchemaState{}, err
	}
	before, err := inspectOSSMultipartActivity(ctx, tx, true)
	if err != nil {
		return before, err
	}
	if before.Database != expectedDatabase {
		return before, errors.New("database name does not match explicit confirmation")
	}
	for _, statement := range OSSMultipartActivitySchemaStatements() {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return before, fmt.Errorf("expand OSS multipart activity schema: %w", err)
		}
	}
	after, err := inspectOSSMultipartActivity(ctx, tx, true)
	if err != nil {
		return after, err
	}
	return after, tx.Commit(ctx)
}
