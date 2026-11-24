package httpapi

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ossMaintenanceReferences struct {
	Rows     int64
	Complete bool
}

// These are control/history records, not live asset bindings. Their current
// lease/session state is checked separately before apply. All other public
// text/JSON/array columns are conservatively inspected; a budget overflow is
// unknown and blocks changes rather than silently declaring no references.
var ossMaintenanceNonBindingTables = map[string]bool{
	"oss_upload_maintenance_actions": true, "oss_upload_byte_bindings": true,
	"oss_upload_logs": true, "oss_scan_logs": true, "oss_download_stats": true,
	"oss_multipart_sessions": true, "oss_user_upload_quota_reservations": true,
}

func inspectOSSByteMaintenanceReferences(ctx context.Context, tx pgx.Tx, fileID int64, key string, plan OSSByteMaintenancePlan, includeFileFKs bool) (ossMaintenanceReferences, error) {
	var result ossMaintenanceReferences
	withinBudget := true
	if _, err := tx.Exec(ctx, `set local statement_timeout='2s'`); err != nil {
		return result, err
	}
	if includeFileFKs {
		rows, err := tx.Query(ctx, `select distinct child.relname,a.attname from pg_constraint c join pg_class child on child.oid=c.conrelid join pg_namespace n on n.oid=child.relnamespace join pg_attribute a on a.attrelid=child.oid and a.attnum=c.conkey[1] where c.contype='f' and c.confrelid='public.oss_files'::regclass and cardinality(c.conkey)=1 and n.nspname='public' and not child.relispartition order by child.relname,a.attname`)
		if err != nil {
			return result, err
		}
		var refs [][2]string
		for rows.Next() {
			var table, column string
			if err = rows.Scan(&table, &column); err != nil {
				rows.Close()
				return result, err
			}
			if !ossMaintenanceNonBindingTables[table] {
				refs = append(refs, [2]string{table, column})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		for _, ref := range refs {
			var n int64
			query := `select count(*) from (select 1 from ` + pgx.Identifier{"public", ref[0]}.Sanitize() + ` where ` + pgx.Identifier{ref[1]}.Sanitize() + `=$1 limit $2) bounded`
			if err = tx.QueryRow(ctx, query, fileID, plan.MaxReferenceRows+1).Scan(&n); err != nil {
				return result, err
			}
			result.Rows += n
			if n > int64(plan.MaxReferenceRows) {
				withinBudget = false
			}
		}
	}
	columns, err := ossMaintenanceReferenceColumns(ctx, tx)
	if err != nil {
		return result, err
	}
	if len(columns) > plan.MaxReferenceCols {
		return result, nil
	}
	escapedPath := (&url.URL{Path: key}).EscapedPath()
	for _, column := range columns {
		where := "true"
		if column[0] == "oss_files" {
			where = "id<>$1"
		}
		query := `select count(*),count(*) filter(where octet_length(value)>$3),count(*) filter(where octet_length(value)<=$3 and (strpos(value,$4)>0 or strpos(value,$5)>0 or strpos(value,$6)>0)) from (select ` + pgx.Identifier{column[1]}.Sanitize() + `::text value from ` + pgx.Identifier{"public", column[0]}.Sanitize() + ` where ` + where + ` and $1::bigint>0 limit $2) bounded`
		var n, large, matches int64
		if err = tx.QueryRow(ctx, query, fileID, plan.MaxReferenceRows+1, plan.MaxReferenceBytes, key, url.PathEscape(key), escapedPath).Scan(&n, &large, &matches); err != nil {
			return result, err
		}
		result.Rows += matches
		if n > int64(plan.MaxReferenceRows) || large > 0 {
			withinBudget = false
		}
	}
	result.Complete = withinBudget
	return result, nil
}

func ossMaintenanceReferencesStatus(refs ossMaintenanceReferences, err error) string {
	if err != nil {
		return "REFERENCE_QUERY_ERROR"
	}
	if !refs.Complete {
		return "REFERENCE_BUDGET_EXCEEDED"
	}
	if refs.Rows > 0 {
		return "NEEDS_SCOPED_REFERENCE_PLAN"
	}
	return ""
}

func knownOSSLegacySourceKey(file ossMaintenanceFile, explicit string) (string, bool) {
	sourceExt := strings.ToLower(filepath.Ext(file.SourceOriginalName))
	storedExt := strings.ToLower(filepath.Ext(file.OriginalName))
	converted := file.SourceSize != file.Size || (sourceExt != "" && sourceExt != storedExt) || strings.HasSuffix(file.Key, ".render-1024.png")
	explicit = strings.TrimSpace(explicit)
	if explicit == "" {
		if !converted {
			return file.Key, true
		}
		return "", false
	}
	if explicit == file.Key {
		return explicit, !converted
	}
	// A converted output cannot supply a missing original, even if a legacy
	// length field happens to equal the output length. Require the known input
	// format and exact recognized path relationship, never an arbitrary sibling.
	inputExt := strings.ToLower(filepath.Ext(explicit))
	if sourceExt == "" || sourceExt != inputExt {
		return "", false
	}
	rasterInput := inputExt == ".png" || inputExt == ".jpg" || inputExt == ".jpeg"
	webp := storedExt == ".webp" && rasterInput && persistedWebPObjectKey(explicit) == file.Key
	render := (rasterInput || inputExt == ".webp") && strings.HasSuffix(file.Key, ".render-1024.png") && persistedModResourceRenderObjectKey(explicit) == file.Key
	return explicit, webp || render
}
