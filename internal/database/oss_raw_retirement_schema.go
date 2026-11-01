package database

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

const ossUploadCapabilityShapeBody = `select coalesce(p_bucket<>'' and p_endpoint<>'' and p_key<>'' and p_actor>0
 and p_kind in ('put','multipart') and ((p_kind='put' and p_upload='') or (p_kind='multipart' and p_upload<>''))
 and isfinite(p_expiry) and isfinite(p_first) and isfinite(p_last)
 and p_expiry>p_last and p_last>=p_first and p_count>0,false)`
const ossUploadCapabilityShapeSignature = `public.oss_upload_capability_shape(text,text,text,bigint,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,bigint)`
const ossUploadCapabilityShapeExpression = `oss_upload_capability_shape(bucket, endpoint, object_key, last_actor_id, grant_kind, upload_id, max_expires_at, first_issued_at, last_issued_at, grant_count)`

// No historical authorization is inferred or backfilled. Legacy deleted files
// can acquire a durable HOLD intent, but cannot pass the absent-capability guard.
func OSSRawRetirementSchemaStatements() []string {
	return []string{
		`create or replace function oss_upload_capability_shape(p_bucket text,p_endpoint text,p_key text,p_actor bigint,p_kind text,p_upload text,p_expiry timestamptz,p_first timestamptz,p_last timestamptz,p_count bigint) returns boolean language sql immutable parallel safe as $$` + ossUploadCapabilityShapeBody + `$$`,
		`create table if not exists oss_upload_capabilities (
 bucket text not null,endpoint text not null,object_key text not null,
 last_actor_id bigint not null,grant_kind text not null,upload_id text not null default '',
 max_expires_at timestamptz not null,first_issued_at timestamptz not null default clock_timestamp(),
 last_issued_at timestamptz not null default clock_timestamp(),grant_count bigint not null default 1,
 primary key(bucket,endpoint,object_key),
 constraint oss_upload_capabilities_shape check(oss_upload_capability_shape(bucket,endpoint,object_key,last_actor_id,grant_kind,upload_id,max_expires_at,first_issued_at,last_issued_at,grant_count))
 )`,
		`alter table oss_object_deletion_outbox add column if not exists raw_upload_binding_id uuid`,
		`do $$ begin if not exists(select 1 from pg_constraint where conrelid='oss_object_deletion_outbox'::regclass and conname='oss_deletion_raw_binding_fk') then alter table oss_object_deletion_outbox add constraint oss_deletion_raw_binding_fk foreign key(raw_upload_binding_id) references oss_upload_byte_bindings(id) on delete restrict; end if; end $$`,
		`create unique index if not exists oss_deletion_raw_binding on oss_object_deletion_outbox(raw_upload_binding_id) where raw_upload_binding_id is not null`,
		`alter table oss_object_deletion_outbox add column if not exists raw_hold_reason text not null default ''`,
	}
}

func inspectOSSRawRetirementSchema(ctx context.Context, query logRedactionQueryer) (bool, error) {
	var present bool
	if err := query.QueryRow(ctx, `select to_regclass('public.oss_upload_capabilities') is not null and exists(select 1 from information_schema.columns where table_schema='public' and table_name='oss_object_deletion_outbox' and column_name='raw_upload_binding_id') and exists(select 1 from information_schema.columns where table_schema='public' and table_name='oss_object_deletion_outbox' and column_name='raw_hold_reason')`).Scan(&present); err != nil || !present {
		return false, err
	}
	var raw []byte
	if err := query.QueryRow(ctx, `select coalesce(jsonb_object_agg(attribute.attname,jsonb_build_object('type',format_type(attribute.atttypid,attribute.atttypmod),'notNull',attribute.attnotnull,'default',coalesce(pg_get_expr(default_row.adbin,default_row.adrelid),''))),'{}'::jsonb) from pg_attribute attribute left join pg_attrdef default_row on default_row.adrelid=attribute.attrelid and default_row.adnum=attribute.attnum where attribute.attrelid='public.oss_upload_capabilities'::regclass and attribute.attnum>0 and not attribute.attisdropped`).Scan(&raw); err != nil {
		return false, err
	}
	var columns map[string]ossUploadBindingColumn
	if err := json.Unmarshal(raw, &columns); err != nil {
		return false, err
	}
	types := map[string]string{"bucket": "text", "endpoint": "text", "object_key": "text", "last_actor_id": "bigint", "grant_kind": "text", "upload_id": "text", "max_expires_at": "timestamp with time zone", "first_issued_at": "timestamp with time zone", "last_issued_at": "timestamp with time zone", "grant_count": "bigint"}
	defaults := map[string]string{"upload_id": "''::text", "first_issued_at": "clock_timestamp()", "last_issued_at": "clock_timestamp()", "grant_count": "1"}
	for name, kind := range types {
		column, exists := columns[name]
		if !exists || column.Type != kind || !column.NotNull || column.Default != defaults[name] {
			return false, errors.New("incompatible OSS issued capability column: " + name)
		}
	}
	var functionValid, primaryValid, deletionLinkValid, indexValid bool
	var body, expression string
	if err := query.QueryRow(ctx, `select coalesce((select prosrc from pg_proc where oid=to_regprocedure($1)),''),coalesce((select provolatile='i' and not prosecdef and proparallel='s' and prorettype='boolean'::regtype from pg_proc where oid=to_regprocedure($1)),false),coalesce((select pg_get_expr(conbin,conrelid) from pg_constraint where conrelid='public.oss_upload_capabilities'::regclass and conname='oss_upload_capabilities_shape' and contype='c' and convalidated),''),exists(select 1 from pg_constraint con join pg_index idx on idx.indexrelid=con.conindid where con.conrelid='public.oss_upload_capabilities'::regclass and con.contype='p' and idx.indisunique and idx.indisvalid and idx.indnkeyatts=3 and pg_get_indexdef(idx.indexrelid,1,true)='bucket' and pg_get_indexdef(idx.indexrelid,2,true)='endpoint' and pg_get_indexdef(idx.indexrelid,3,true)='object_key')`, ossUploadCapabilityShapeSignature).Scan(&body, &functionValid, &expression, &primaryValid); err != nil {
		return false, err
	}
	normalize := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	if !functionValid || normalize(body) != normalize(ossUploadCapabilityShapeBody) || normalize(expression) != normalize(ossUploadCapabilityShapeExpression) || !primaryValid {
		return false, errors.New("incompatible OSS issued capability shape or identity")
	}
	if err := query.QueryRow(ctx, `select exists(select 1 from pg_attribute where attrelid='public.oss_object_deletion_outbox'::regclass and attname='raw_upload_binding_id' and atttypid='uuid'::regtype and not attnotnull and not atthasdef and not attisdropped) and exists(select 1 from pg_constraint where conrelid='public.oss_object_deletion_outbox'::regclass and conname='oss_deletion_raw_binding_fk' and contype='f' and confrelid='public.oss_upload_byte_bindings'::regclass and confdeltype='r' and confupdtype='a' and convalidated and conkey=array[(select attnum from pg_attribute where attrelid='public.oss_object_deletion_outbox'::regclass and attname='raw_upload_binding_id')]::smallint[] and confkey=array[(select attnum from pg_attribute where attrelid='public.oss_upload_byte_bindings'::regclass and attname='id')]::smallint[])`).Scan(&deletionLinkValid); err != nil {
		return false, err
	}
	if err := query.QueryRow(ctx, `select exists(select 1 from pg_index idx where idx.indexrelid=to_regclass('public.oss_deletion_raw_binding') and idx.indrelid='public.oss_object_deletion_outbox'::regclass and idx.indisunique and idx.indisvalid and idx.indnkeyatts=1 and pg_get_indexdef(idx.indexrelid,1,true)='raw_upload_binding_id' and pg_get_expr(idx.indpred,idx.indrelid)='(raw_upload_binding_id IS NOT NULL)')`).Scan(&indexValid); err != nil {
		return false, err
	}
	if !deletionLinkValid || !indexValid {
		return false, errors.New("incompatible OSS terminal raw deletion link or index")
	}
	var holdValid bool
	if err := query.QueryRow(ctx, `select exists(select 1 from pg_attribute a join pg_attrdef d on d.adrelid=a.attrelid and d.adnum=a.attnum where a.attrelid='public.oss_object_deletion_outbox'::regclass and a.attname='raw_hold_reason' and a.atttypid='text'::regtype and a.attnotnull and not a.attisdropped and pg_get_expr(d.adbin,d.adrelid)=$1)`, "''::text").Scan(&holdValid); err != nil {
		return false, err
	}
	if !holdValid {
		return false, errors.New("incompatible OSS terminal raw hold reason column")
	}
	return true, nil
}
