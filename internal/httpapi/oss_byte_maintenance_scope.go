package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

type ossMaintenanceRowsQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type OSSByteMaintenanceReferenceScope struct {
	SchemaSHA                string   `json:"referenceSchemaSha256"`
	PotentialTables          []string `json:"potentialReferenceTables"`
	DeclaredLockTables       []string `json:"declaredLockTables"`
	OperatorQuiescedTables   []string `json:"operatorQuiescedTables"`
	UncoveredTables          []string `json:"uncoveredTables"`
	QuiescenceDeclared       bool     `json:"operatorWritesAndDDLQuiescenceDeclared"`
	MutationPreconditionsMet bool     `json:"mutationPreconditionsMet"`
	CatalogComplete          bool     `json:"catalogComplete"`
	OnlineRaceSafe           bool     `json:"onlineRaceSafe"`
}

type ossMaintenanceScopeError struct{ status string }

func (e *ossMaintenanceScopeError) Error() string { return e.status }

func ossMaintenanceReferenceColumns(ctx context.Context, q ossMaintenanceRowsQuery) ([][5]string, error) {
	excluded := make([]string, 0, len(ossMaintenanceNonBindingTables))
	for table := range ossMaintenanceNonBindingTables {
		excluded = append(excluded, table)
	}
	sort.Strings(excluded)
	rows, err := q.Query(ctx, `select c.relname,a.attname,format_type(a.atttypid,a.atttypmod),coalesce(pg_get_expr(d.adbin,d.adrelid),''),c.oid::text
 from pg_class c join pg_namespace n on n.oid=c.relnamespace join pg_attribute a on a.attrelid=c.oid join pg_type typ on typ.oid=a.atttypid left join pg_attrdef d on d.adrelid=a.attrelid and d.adnum=a.attnum
 where n.nspname='public' and c.relkind in ('r','p','f') and not c.relispartition and not c.relname=any($1::text[]) and a.attnum>0 and not a.attisdropped
 and (typ.oid in ('json'::regtype,'jsonb'::regtype) or typ.typcategory in ('S','A') or typ.typtype='d') order by c.relname,a.attnum limit 2049`, excluded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns [][5]string
	for rows.Next() {
		var col [5]string
		if err = rows.Scan(&col[0], &col[1], &col[2], &col[3], &col[4]); err != nil {
			return nil, err
		}
		if !ossMaintenanceNonBindingTables[col[0]] {
			columns = append(columns, col)
		}
	}
	return columns, rows.Err()
}

func inspectOSSByteMaintenanceWriteScope(ctx context.Context, q ossMaintenanceRowsQuery, plan OSSByteMaintenancePlan) (OSSByteMaintenanceReferenceScope, string, error) {
	scope := OSSByteMaintenanceReferenceScope{DeclaredLockTables: append([]string{}, plan.ReferenceWriteTables...), OperatorQuiescedTables: append([]string{}, plan.QuiescedReferenceTables...), QuiescenceDeclared: plan.ReferenceWritesQuiesced, PotentialTables: []string{}, UncoveredTables: []string{}, OnlineRaceSafe: false}
	columns, err := ossMaintenanceReferenceColumns(ctx, q)
	if err != nil {
		return scope, "", err
	}
	inheritRows, err := q.Query(ctx, `select pn.nspname||'.'||p.relname||':'||p.oid::text,cn.nspname||'.'||c.relname||':'||c.oid::text from pg_inherits i join pg_class p on p.oid=i.inhparent join pg_namespace pn on pn.oid=p.relnamespace join pg_class c on c.oid=i.inhrelid join pg_namespace cn on cn.oid=c.relnamespace where pn.nspname='public' order by pn.nspname,p.relname,cn.nspname,c.relname limit 2049`)
	if err != nil {
		return scope, "", err
	}
	var inherits [][2]string
	for inheritRows.Next() {
		var edge [2]string
		if err = inheritRows.Scan(&edge[0], &edge[1]); err != nil {
			inheritRows.Close()
			return scope, "", err
		}
		inherits = append(inherits, edge)
	}
	err = inheritRows.Err()
	inheritRows.Close()
	if err != nil {
		return scope, "", err
	}
	raw, _ := json.Marshal(struct {
		Columns     [][5]string
		Inheritance [][2]string
	}{columns, inherits})
	hash := sha256.Sum256(raw)
	scope.SchemaSHA = hex.EncodeToString(hash[:])
	if len(columns) > plan.MaxReferenceCols || len(inherits) > 2048 {
		return scope, "REFERENCE_BUDGET_EXCEEDED", nil
	}
	scope.CatalogComplete = true
	all := map[string]bool{}
	covered := map[string]bool{}
	for _, col := range columns {
		all[col[0]] = true
	}
	for _, table := range plan.ReferenceWriteTables {
		if !all[table] {
			return scope, "REFERENCE_WRITE_SCOPE_UNKNOWN_TABLE", nil
		}
		covered[table] = true
	}
	for _, table := range plan.QuiescedReferenceTables {
		if !all[table] {
			return scope, "REFERENCE_WRITE_SCOPE_UNKNOWN_TABLE", nil
		}
		covered[table] = true
	}
	for table := range all {
		scope.PotentialTables = append(scope.PotentialTables, table)
		if !covered[table] {
			scope.UncoveredTables = append(scope.UncoveredTables, table)
		}
	}
	sort.Strings(scope.PotentialTables)
	sort.Strings(scope.UncoveredTables)
	status := ""
	if !plan.ReferenceWritesQuiesced {
		status = "REFERENCE_WRITES_NOT_QUIESCED"
	} else if normalizeSHA256(plan.ReferenceSchemaSHA) != scope.SchemaSHA {
		status = "REFERENCE_SCHEMA_CHANGED"
	} else if len(scope.UncoveredTables) > 0 {
		status = "REFERENCE_WRITE_SCOPE_GAP"
	}
	scope.MutationPreconditionsMet = status == ""
	return scope, status, nil
}

// These bounded locks cover only the reviewed list. Every other discovered
// potential reference table must be explicitly listed as operator-quiesced;
// that prerequisite is not represented as database-enforced online safety.
func (run *ossMaintenanceRun) freezeReferenceWriteScope(ctx context.Context, tx pgx.Tx) ([]string, error) {
	acquire, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := tx.Exec(acquire, `set local lock_timeout='5s'; set local statement_timeout='2s'`); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(acquire, `select pg_advisory_xact_lock(hashtext('mcmods-cn-schema-migrations'))`); err != nil {
		return nil, &ossMaintenanceScopeError{"REFERENCE_WRITE_FENCE_UNAVAILABLE"}
	}
	_, status, err := inspectOSSByteMaintenanceWriteScope(acquire, tx, run.plan)
	if err != nil {
		return nil, err
	}
	if status != "" {
		return nil, &ossMaintenanceScopeError{status}
	}
	// Expand descendants explicitly, so sixteen selected partition parents
	// cannot silently become an unbounded number of physical table locks.
	rows, err := tx.Query(acquire, `with recursive targets(oid) as (
 select c.oid from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname='public' and c.relname=any($1::text[]) and c.relkind in ('r','p','f')
 union select i.inhrelid from pg_inherits i join targets p on p.oid=i.inhparent)
 select n.nspname,c.relname,c.relkind::text from targets t join pg_class c on c.oid=t.oid join pg_namespace n on n.oid=c.relnamespace order by n.nspname,c.relname limit 17`, run.plan.ReferenceWriteTables)
	if err != nil {
		return nil, err
	}
	var tables []string
	unsupported := false
	for rows.Next() {
		var schema, name, kind string
		if err = rows.Scan(&schema, &name, &kind); err != nil {
			rows.Close()
			return nil, err
		}
		if schema != "public" || (kind != "r" && kind != "p") {
			unsupported = true
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if unsupported || len(tables) < 1 || len(tables) > 16 {
		return nil, &ossMaintenanceScopeError{"REFERENCE_LOCK_SCOPE_TOO_WIDE"}
	}
	for _, table := range tables {
		if _, err = tx.Exec(acquire, `lock table only `+pgx.Identifier{"public", table}.Sanitize()+` in share row exclusive mode`); err != nil {
			return nil, &ossMaintenanceScopeError{"REFERENCE_WRITE_FENCE_UNAVAILABLE"}
		}
	}
	_, status, err = inspectOSSByteMaintenanceWriteScope(acquire, tx, run.plan)
	if err != nil {
		return nil, err
	}
	if status != "" {
		return nil, &ossMaintenanceScopeError{status}
	}
	if _, err = tx.Exec(ctx, `set local lock_timeout='2s'`); err != nil {
		return nil, err
	}
	return tables, nil
}

func ossMaintenanceScopeStatus(err error) string {
	var scope *ossMaintenanceScopeError
	if errors.As(err, &scope) {
		return scope.status
	}
	return ""
}
