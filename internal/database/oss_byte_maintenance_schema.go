package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const ossMaintenanceShapeBody = `select coalesce(
 p_file>0 and p_key<>'' and p_sha ~ '^[0-9a-f]{64}$' and btrim(p_backup)<>''
 and ((p_operation='promote' and p_state in ('preparing','promoted','failed'))
   or (p_operation='cleanup_raw' and p_state in ('cleanup_queued','processing','completed','failed'))),false)`

// The journal contains private preimages and exact storage keys. It is not an
// API listing or an instruction to run production data changes.
func OSSByteMaintenanceSchemaStatements() []string {
	return []string{
		`create or replace function oss_upload_maintenance_shape(p_operation text,p_state text,p_file bigint,p_key text,p_sha text,p_backup text) returns boolean language sql immutable parallel safe as $$` + ossMaintenanceShapeBody + `$$`,
		`create table if not exists oss_upload_maintenance_actions (
 id uuid primary key,operation text not null,state text not null,
 file_id bigint not null references oss_files(id) on delete restrict,
 bucket text not null,endpoint text not null,region text not null,use_cname boolean not null default false,
 binding_id uuid references oss_upload_byte_bindings(id) on delete restrict,
 target_key text not null,source_key text not null default '',
 plan_sha256 text not null,backup_reference text not null,file_preimage jsonb not null,
 reference_schema_sha256 text not null default '',quiescence_declared boolean not null default false,
 quiesced_reference_tables text[] not null default '{}'::text[],write_fence_tables text[] not null default '{}'::text[],
 run_token text not null,lease_expires_at timestamptz not null,
 created_at timestamptz not null default now(),updated_at timestamptz not null default now(),
 constraint oss_upload_maintenance_shape check(oss_upload_maintenance_shape(operation,state,file_id,target_key,plan_sha256,backup_reference))
)`,
		`create index if not exists oss_upload_maintenance_raw_guard on oss_upload_maintenance_actions(target_key) where operation='cleanup_raw'`,
	}
}

func inspectOSSByteMaintenanceSchema(ctx context.Context, q schemaQueryer) (bool, error) {
	var present bool
	if err := q.QueryRow(ctx, `select to_regclass('public.oss_upload_maintenance_actions') is not null`).Scan(&present); err != nil || !present {
		return false, err
	}
	var raw []byte
	if err := q.QueryRow(ctx, `select jsonb_object_agg(a.attname,jsonb_build_object('type',format_type(a.atttypid,a.atttypmod),'notNull',a.attnotnull,'default',coalesce(pg_get_expr(d.adbin,d.adrelid),''))) from pg_attribute a left join pg_attrdef d on d.adrelid=a.attrelid and d.adnum=a.attnum where a.attrelid='public.oss_upload_maintenance_actions'::regclass and a.attnum>0 and not a.attisdropped`).Scan(&raw); err != nil {
		return false, err
	}
	var columns map[string]ossUploadBindingColumn
	if err := json.Unmarshal(raw, &columns); err != nil {
		return false, err
	}
	types := map[string]string{"id": "uuid", "operation": "text", "state": "text", "file_id": "bigint", "bucket": "text", "endpoint": "text", "region": "text", "use_cname": "boolean", "binding_id": "uuid", "target_key": "text", "source_key": "text", "plan_sha256": "text", "backup_reference": "text", "file_preimage": "jsonb", "run_token": "text", "lease_expires_at": "timestamp with time zone", "created_at": "timestamp with time zone", "updated_at": "timestamp with time zone"}
	defaults := map[string]string{"use_cname": "false", "source_key": "''::text", "created_at": "now()", "updated_at": "now()"}
	for name, kind := range types {
		c, ok := columns[name]
		if !ok || c.Type != kind || c.NotNull != (name != "binding_id") || c.Default != defaults[name] {
			return false, fmt.Errorf("incompatible OSS maintenance column: %s", name)
		}
	}
	scopeReady := true
	for name, kind := range map[string]string{"reference_schema_sha256": "text", "quiescence_declared": "boolean", "quiesced_reference_tables": "text[]", "write_fence_tables": "text[]"} {
		c, ok := columns[name]
		if !ok {
			scopeReady = false
			continue
		}
		want := "'{}'::text[]"
		if kind == "boolean" {
			want = "false"
		}
		if kind == "text" {
			want = "''::text"
		}
		if c.Type != kind || !c.NotNull || c.Default != want {
			return false, fmt.Errorf("incompatible OSS maintenance scope column: %s", name)
		}
	}
	var body, expression string
	var valid bool
	if err := q.QueryRow(ctx, `select coalesce((select prosrc from pg_proc where oid=to_regprocedure('public.oss_upload_maintenance_shape(text,text,bigint,text,text,text)')),''),coalesce((select pg_get_expr(conbin,conrelid) from pg_constraint where conrelid='public.oss_upload_maintenance_actions'::regclass and conname='oss_upload_maintenance_shape' and convalidated),''),exists(select 1 from pg_proc where oid=to_regprocedure('public.oss_upload_maintenance_shape(text,text,bigint,text,text,text)') and provolatile='i' and not prosecdef and proparallel='s' and prorettype='boolean'::regtype)`).Scan(&body, &expression, &valid); err != nil {
		return false, err
	}
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	if !valid || normalize(body) != normalize(ossMaintenanceShapeBody) || normalize(expression) != "oss_upload_maintenance_shape(operation, state, file_id, target_key, plan_sha256, backup_reference)" {
		return false, errors.New("incompatible OSS maintenance proof constraint")
	}
	var keysValid bool
	if err := q.QueryRow(ctx, `select exists(select 1 from pg_constraint c where c.conrelid='public.oss_upload_maintenance_actions'::regclass and c.contype='p' and c.conkey=array[(select attnum from pg_attribute where attrelid=c.conrelid and attname='id')])
 and exists(select 1 from pg_constraint c where c.conrelid='public.oss_upload_maintenance_actions'::regclass and c.contype='f' and c.confrelid='public.oss_files'::regclass and c.confdeltype='r' and c.convalidated and c.conkey=array[(select attnum from pg_attribute where attrelid=c.conrelid and attname='file_id')])
 and exists(select 1 from pg_constraint c where c.conrelid='public.oss_upload_maintenance_actions'::regclass and c.contype='f' and c.confrelid='public.oss_upload_byte_bindings'::regclass and c.confdeltype='r' and c.convalidated and c.conkey=array[(select attnum from pg_attribute where attrelid=c.conrelid and attname='binding_id')])
 and exists(select 1 from pg_index i where i.indexrelid=to_regclass('public.oss_upload_maintenance_raw_guard') and i.indisvalid and i.indrelid='public.oss_upload_maintenance_actions'::regclass and i.indnkeyatts=1 and pg_get_indexdef(i.indexrelid,1,true)='target_key' and pg_get_expr(i.indpred,i.indrelid)=$1)`, "(operation = 'cleanup_raw'::text)").Scan(&keysValid); err != nil {
		return false, err
	}
	if !keysValid {
		return false, errors.New("incompatible OSS maintenance identity, references or raw guard index")
	}
	return scopeReady, nil
}

func RequireOSSByteMaintenanceSchema(ctx context.Context, q schemaQueryer) error {
	ready, err := inspectOSSByteMaintenanceSchema(ctx, q)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("OSS maintenance journal is missing from the installed database baseline")
	}
	return nil
}
