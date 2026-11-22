package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func definitionFingerprint(statements []string) string {
	hash := sha256.New()
	for _, statement := range statements {
		_, _ = hash.Write([]byte(statement))
		_, _ = hash.Write([]byte{0})
	}
	_, _ = hash.Write([]byte(schemaMetadataDDL))
	return hex.EncodeToString(hash.Sum(nil))
}

// The fingerprint follows schema definitions, never operator data, OIDs,
// sequence values or planner statistics. Catalog definitions are resolved
// with the same search path at install and subsequent startup.
func catalogFingerprint(ctx context.Context, tx pgx.Tx, schema string) (string, error) {
	var fingerprint string
	err := tx.QueryRow(ctx, `with relations as (
		select oid, relname, relkind, relrowsecurity, relforcerowsecurity
		from pg_class where relnamespace=to_regnamespace($1)
	), definitions as (
		select jsonb_build_array('relation',relname,relkind,relrowsecurity,relforcerowsecurity) as definition from relations
		union all select jsonb_build_array('column',r.relname,a.attname,a.attnum,format_type(a.atttypid,a.atttypmod),
			a.attnotnull,a.attidentity,a.attgenerated,pg_get_expr(d.adbin,d.adrelid))
			from relations r join pg_attribute a on a.attrelid=r.oid
			left join pg_attrdef d on d.adrelid=a.attrelid and d.adnum=a.attnum
			where r.relkind in ('r','p','v','m','f','c') and a.attnum>0 and not a.attisdropped
		union all select jsonb_build_array('constraint',r.relname,c.conname,c.convalidated,pg_get_constraintdef(c.oid))
			from relations r join pg_constraint c on c.conrelid=r.oid
		union all select jsonb_build_array('index',r.relname,i.indisvalid,i.indisready,pg_get_indexdef(i.indexrelid))
			from relations r join pg_index i on i.indexrelid=r.oid
		union all select jsonb_build_array('trigger',r.relname,t.tgname,t.tgenabled,pg_get_triggerdef(t.oid))
			from relations r join pg_trigger t on t.tgrelid=r.oid where not t.tgisinternal
		union all select jsonb_build_array('view',relname,pg_get_viewdef(oid)) from relations where relkind in ('v','m')
		union all select jsonb_build_array('routine',pg_get_functiondef(oid))
			from pg_proc where pronamespace=to_regnamespace($1) and prokind<>'a'
		union all select jsonb_build_array('extension',extname,extversion) from pg_extension where extnamespace=to_regnamespace($1)
	)
	select encode(sha256(convert_to(coalesce(string_agg(definition::text,E'\n' order by definition::text collate "C"),''),'UTF8')),'hex')
	from definitions`, schema).Scan(&fingerprint)
	if err != nil {
		return "", fmt.Errorf("read database catalog fingerprint: %w", err)
	}
	return fingerprint, nil
}
