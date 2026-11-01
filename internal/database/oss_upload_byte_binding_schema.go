package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This is a typed audit/cleanup receipt, not a promise inferred from old
// metadata. Only a caller that hashes the private copied bytes may commit it.
const ossUploadByteBindingProofBody = `select coalesce(
 p_status in ('preparing','committed','cleanup_pending','discarded')
 and p_owner>0 and p_declared_size>0 and p_declared_size<=2147483648
 and p_declared_sha ~ '^[0-9a-f]{64}$'
 and p_upload_key<>'' and p_sealed_key<>p_upload_key
 and strpos('/'||p_sealed_key||'/', '/_mcmods_sealed_v1/')>0
 and (p_status<>'committed' or (
  p_source_sha=p_declared_sha and p_source_size=p_declared_size
  and p_stored_sha ~ '^[0-9a-f]{64}$'
  and p_stored_size>0 and p_stored_size<=2147483648
  and p_stored_key<>p_upload_key
  and strpos('/'||p_stored_key||'/', '/_mcmods_sealed_v1/')>0
  and p_file_id is not null and p_file_id>0)),false)`
const ossUploadByteBindingProofSignature = `public.oss_upload_byte_binding_shape(text,bigint,text,bigint,text,text,text,text,bigint,text,bigint,bigint)`
const ossUploadByteBindingProofExpression = `oss_upload_byte_binding_shape(status, owner_id, declared_sha256, declared_size_bytes, upload_object_key, sealed_source_key, stored_object_key, source_verified_sha256, source_verified_size_bytes, stored_verified_sha256, stored_verified_size_bytes, file_id)`

func OSSUploadByteBindingSchemaStatements() []string {
	statements := []string{
		`create or replace function oss_upload_byte_binding_shape(p_status text,p_owner bigint,p_declared_sha text,p_declared_size bigint,p_upload_key text,p_sealed_key text,p_stored_key text,p_source_sha text,p_source_size bigint,p_stored_sha text,p_stored_size bigint,p_file_id bigint) returns boolean language sql immutable parallel safe as $$` + ossUploadByteBindingProofBody + `$$`,
		`create table if not exists oss_upload_byte_bindings (
   id uuid primary key,
   owner_id bigint not null,
   bucket text not null,endpoint text not null,region text not null,
   file_endpoint text not null default '',
   use_cname boolean not null default false,
   upload_object_key text not null,logical_category text not null,
   source text not null default '',original_name text not null,
   declared_sha256 text not null,declared_size_bytes bigint not null,
   sealed_source_key text not null,stored_object_key text not null default '',
   source_verified_sha256 text not null default '',source_verified_size_bytes bigint not null default 0,
   stored_verified_sha256 text not null default '',stored_verified_size_bytes bigint not null default 0,
   file_id bigint constraint oss_upload_byte_bindings_file_fk references oss_files(id) on delete restrict,
   status text not null default 'preparing',run_token text not null,lease_expires_at timestamptz not null,
   created_at timestamptz not null default now(),updated_at timestamptz not null default now(),
   constraint oss_upload_byte_bindings_shape check(oss_upload_byte_binding_shape(status,owner_id,declared_sha256,declared_size_bytes,upload_object_key,sealed_source_key,stored_object_key,source_verified_sha256,source_verified_size_bytes,stored_verified_sha256,stored_verified_size_bytes,file_id))
  )`,
		`create unique index if not exists oss_upload_byte_bindings_committed_upload on oss_upload_byte_bindings(bucket,endpoint,upload_object_key) where status='committed'`,
		`create index if not exists oss_upload_byte_bindings_file on oss_upload_byte_bindings(file_id) where status='committed'`,
		`create index if not exists oss_upload_byte_bindings_cleanup on oss_upload_byte_bindings(lease_expires_at,id) where status in ('preparing','cleanup_pending')`,
		`create index if not exists oss_upload_byte_bindings_reconcile on oss_upload_byte_bindings(lease_expires_at,id) where status in ('cleanup_pending','discarded')`,
		`alter table oss_upload_byte_bindings add column if not exists file_endpoint text not null default ''`,
	}
	return append(statements, OSSByteMaintenanceSchemaStatements()...)
}

type OSSUploadByteBindingState struct {
	Database             string `json:"database"`
	Generation           int    `json:"generation"`
	Present              bool   `json:"present"`
	Committed            int64  `json:"committed"`
	Preparing            int64  `json:"preparing"`
	CleanupPending       int64  `json:"cleanup_pending"`
	Discarded            int64  `json:"discarded"`
	ReconciliationReady  bool   `json:"reconciliation_ready"`
	EndpointMappingReady bool   `json:"endpoint_mapping_ready"`
	MaintenanceReady     bool   `json:"maintenance_ready"`
	LegacyActiveFiles    int64  `json:"legacy_active_files_without_binding"`
}
type ossUploadByteBindingQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type ossUploadBindingColumn struct {
	Type    string `json:"type"`
	NotNull bool   `json:"notNull"`
	Default string `json:"default"`
}

func inspectOSSUploadByteBindings(ctx context.Context, q ossUploadByteBindingQuery, counts bool) (OSSUploadByteBindingState, error) {
	var state OSSUploadByteBindingState
	if err := q.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).Scan(&state.Database, &state.Generation); err != nil {
		return state, err
	}
	if state.Generation != schemaGeneration {
		return state, errors.New("OSS byte binding expansion requires generation168")
	}
	if err := q.QueryRow(ctx, `select to_regclass('public.oss_upload_byte_bindings') is not null`).Scan(&state.Present); err != nil {
		return state, err
	}
	if !state.Present {
		if counts {
			if err := q.QueryRow(ctx, `select count(*) from public.oss_files where status='active'`).Scan(&state.LegacyActiveFiles); err != nil {
				return state, err
			}
		}
		return state, nil
	}
	var raw []byte
	if err := q.QueryRow(ctx, `select coalesce(jsonb_object_agg(attribute.attname,jsonb_build_object('type',format_type(attribute.atttypid,attribute.atttypmod),'notNull',attribute.attnotnull,'default',coalesce(pg_get_expr(default_row.adbin,default_row.adrelid),''))),'{}'::jsonb) from pg_attribute attribute left join pg_attrdef default_row on default_row.adrelid=attribute.attrelid and default_row.adnum=attribute.attnum where attribute.attrelid='public.oss_upload_byte_bindings'::regclass and attribute.attnum>0 and not attribute.attisdropped`).Scan(&raw); err != nil {
		return state, err
	}
	var columns map[string]ossUploadBindingColumn
	if err := json.Unmarshal(raw, &columns); err != nil {
		return state, err
	}
	types := map[string]string{"id": "uuid", "owner_id": "bigint", "bucket": "text", "endpoint": "text", "region": "text", "use_cname": "boolean", "upload_object_key": "text", "logical_category": "text", "source": "text", "original_name": "text", "declared_sha256": "text", "declared_size_bytes": "bigint", "sealed_source_key": "text", "stored_object_key": "text", "source_verified_sha256": "text", "source_verified_size_bytes": "bigint", "stored_verified_sha256": "text", "stored_verified_size_bytes": "bigint", "file_id": "bigint", "status": "text", "run_token": "text", "lease_expires_at": "timestamp with time zone", "created_at": "timestamp with time zone", "updated_at": "timestamp with time zone"}
	defaults := map[string]string{"use_cname": "false", "source": "''::text", "stored_object_key": "''::text", "source_verified_sha256": "''::text", "source_verified_size_bytes": "0", "stored_verified_sha256": "''::text", "stored_verified_size_bytes": "0", "status": "'preparing'::text", "created_at": "now()", "updated_at": "now()"}
	for name, kind := range types {
		column, exists := columns[name]
		if !exists || column.Type != kind || column.NotNull != (name != "file_id") || column.Default != defaults[name] {
			return state, fmt.Errorf("incompatible OSS byte binding column: %s", name)
		}
	}
	if endpointColumn, exists := columns["file_endpoint"]; exists {
		if endpointColumn.Type != "text" || !endpointColumn.NotNull || endpointColumn.Default != "''::text" {
			return state, errors.New("incompatible OSS byte binding file endpoint column")
		}
		state.EndpointMappingReady = true
	}
	var body, expression string
	var functionValid, fkValid, indexValid bool
	if err := q.QueryRow(ctx, `select coalesce((select prosrc from pg_proc where oid=to_regprocedure($1)),''),coalesce((select provolatile='i' and not prosecdef and proparallel='s' and prorettype='boolean'::regtype from pg_proc where oid=to_regprocedure($1)),false),coalesce((select pg_get_expr(conbin,conrelid) from pg_constraint where conrelid='public.oss_upload_byte_bindings'::regclass and conname='oss_upload_byte_bindings_shape' and contype='c' and convalidated),''),exists(select 1 from pg_constraint where conrelid='public.oss_upload_byte_bindings'::regclass and conname='oss_upload_byte_bindings_file_fk' and contype='f' and confrelid='public.oss_files'::regclass and confdeltype='r' and convalidated)`, ossUploadByteBindingProofSignature).Scan(&body, &functionValid, &expression, &fkValid); err != nil {
		return state, err
	}
	normalize := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	if !functionValid || normalize(body) != normalize(ossUploadByteBindingProofBody) || normalize(expression) != normalize(ossUploadByteBindingProofExpression) || !fkValid {
		return state, errors.New("incompatible OSS byte binding proof function or constraint")
	}
	if err := q.QueryRow(ctx, `select exists(select 1 from pg_index index_row where index_row.indexrelid=to_regclass('public.oss_upload_byte_bindings_committed_upload') and index_row.indisunique and index_row.indisvalid and pg_get_indexdef(index_row.indexrelid,1,true)='bucket' and pg_get_indexdef(index_row.indexrelid,2,true)='endpoint' and pg_get_indexdef(index_row.indexrelid,3,true)='upload_object_key' and index_row.indnkeyatts=3 and pg_get_expr(index_row.indpred,index_row.indrelid)=$1)`, "(status = 'committed'::text)").Scan(&indexValid); err != nil {
		return state, err
	}
	if !indexValid {
		return state, errors.New("incompatible OSS committed-upload identity index")
	}
	var reconcileIndexPresent bool
	if err := q.QueryRow(ctx, `select to_regclass('public.oss_upload_byte_bindings_reconcile') is not null`).Scan(&reconcileIndexPresent); err != nil {
		return state, err
	}
	if reconcileIndexPresent {
		var predicate string
		if err := q.QueryRow(ctx, `select index_row.indisvalid and index_row.indrelid='public.oss_upload_byte_bindings'::regclass and index_row.indnkeyatts=2 and pg_get_indexdef(index_row.indexrelid,1,true)='lease_expires_at' and pg_get_indexdef(index_row.indexrelid,2,true)='id',pg_get_expr(index_row.indpred,index_row.indrelid) from pg_index index_row where index_row.indexrelid='public.oss_upload_byte_bindings_reconcile'::regclass`).Scan(&state.ReconciliationReady, &predicate); err != nil {
			return state, err
		}
		if !state.ReconciliationReady || normalize(predicate) != normalize("(status = ANY (ARRAY['cleanup_pending'::text, 'discarded'::text]))") {
			return state, errors.New("incompatible OSS byte binding reconciliation index")
		}
	}
	if counts {
		locationProof := "false"
		if state.EndpointMappingReady {
			locationProof = "binding.bucket=file.bucket and binding.file_endpoint<>'' and binding.file_endpoint=file.endpoint"
		}
		if err := q.QueryRow(ctx, `select count(*) filter(where status='committed'),count(*) filter(where status='preparing'),count(*) filter(where status='cleanup_pending'),count(*) filter(where status='discarded'),(select count(*) from public.oss_files file where file.status='active' and not exists(select 1 from public.oss_upload_byte_bindings binding where binding.file_id=file.id and binding.status='committed' and binding.stored_object_key=file.object_key and binding.stored_verified_sha256=file.stored_sha256 and `+locationProof+`)) from public.oss_upload_byte_bindings`).Scan(&state.Committed, &state.Preparing, &state.CleanupPending, &state.Discarded, &state.LegacyActiveFiles); err != nil {
			return state, err
		}
	}
	var maintenanceErr error
	state.MaintenanceReady, maintenanceErr = inspectOSSByteMaintenanceSchema(ctx, q)
	return state, maintenanceErr
}
func InspectOSSUploadByteBindings(ctx context.Context, pool *pgxpool.Pool) (OSSUploadByteBindingState, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return OSSUploadByteBindingState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `set local search_path=pg_catalog,public,pg_temp`); err != nil {
		return OSSUploadByteBindingState{}, err
	}
	state, err := inspectOSSUploadByteBindings(ctx, tx, true)
	if err != nil {
		return state, err
	}
	return state, tx.Commit(ctx)
}
func RequireOSSUploadByteBindingSchema(ctx context.Context, q ossUploadByteBindingQuery) error {
	state, err := inspectOSSUploadByteBindings(ctx, q, false)
	if err != nil {
		return err
	}
	if !state.Present || !state.ReconciliationReady || !state.EndpointMappingReady {
		return errors.New("OSS upload byte binding expansion required: inspect and explicitly apply cmd/db-oss-byte-binding-upgrade")
	}
	return nil
}
func ApplyOSSUploadByteBindingUpgrade(ctx context.Context, pool *pgxpool.Pool, expectedDatabase, backupReference string) (OSSUploadByteBindingState, error) {
	if strings.TrimSpace(expectedDatabase) == "" || strings.TrimSpace(backupReference) == "" {
		return OSSUploadByteBindingState{}, errors.New("exact database and recorded backup reference are required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return OSSUploadByteBindingState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `set local search_path=pg_catalog,public,pg_temp`); err != nil {
		return OSSUploadByteBindingState{}, err
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods:oss-upload-byte-binding-v1'))`); err != nil {
		return OSSUploadByteBindingState{}, err
	}
	before, err := inspectOSSUploadByteBindings(ctx, tx, true)
	if err != nil {
		return before, err
	}
	if before.Database != expectedDatabase {
		return before, errors.New("database name does not match explicit confirmation")
	}
	for _, statement := range OSSUploadByteBindingSchemaStatements() {
		// Installation statements follow the installation schema, including
		// isolated test schemas. An explicit live expansion always targets
		// public, independent of pg_temp and the connection search_path.
		statement = strings.Replace(statement, "function oss_upload_byte_binding_shape(", "function public.oss_upload_byte_binding_shape(", 1)
		statement = strings.Replace(statement, "table if not exists oss_upload_byte_bindings (", "table if not exists public.oss_upload_byte_bindings (", 1)
		statement = strings.Replace(statement, "references oss_files(id)", "references public.oss_files(id)", 1)
		statement = strings.Replace(statement, "check(oss_upload_byte_binding_shape(", "check(public.oss_upload_byte_binding_shape(", 1)
		statement = strings.Replace(statement, "function oss_upload_maintenance_shape(", "function public.oss_upload_maintenance_shape(", 1)
		statement = strings.Replace(statement, "table if not exists oss_upload_maintenance_actions (", "table if not exists public.oss_upload_maintenance_actions (", 1)
		statement = strings.Replace(statement, "references oss_upload_byte_bindings(id)", "references public.oss_upload_byte_bindings(id)", 1)
		statement = strings.Replace(statement, "check(oss_upload_maintenance_shape(", "check(public.oss_upload_maintenance_shape(", 1)
		statement = strings.Replace(statement, "on oss_upload_maintenance_actions(", "on public.oss_upload_maintenance_actions(", 1)
		statement = strings.Replace(statement, "alter table oss_upload_byte_bindings ", "alter table public.oss_upload_byte_bindings ", 1)
		statement = strings.Replace(statement, "on oss_upload_byte_bindings(", "on public.oss_upload_byte_bindings(", 1)
		if _, err = tx.Exec(ctx, statement); err != nil {
			return before, fmt.Errorf("expand OSS byte binding schema: %w", err)
		}
	}
	after, err := inspectOSSUploadByteBindings(ctx, tx, true)
	if err != nil {
		return after, err
	}
	if !after.Present || !after.ReconciliationReady || !after.EndpointMappingReady {
		return after, errors.New("OSS upload byte binding expansion did not create the public receipt table, endpoint mapping and reconciliation index")
	}
	return after, tx.Commit(ctx)
}
