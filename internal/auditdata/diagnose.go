// Package auditdata performs read-only, bounded-output diagnosis. Candidate
// counts are not proof of historical intent or private-byte exposure.
package auditdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Section struct {
	Count     int64             `json:"count"`
	PublicIDs []json.RawMessage `json:"public_ids"`
}
type Report struct {
	ReadOnly    bool               `json:"read_only"`
	Sections    map[string]Section `json:"sections"`
	Limitations []string           `json:"limitations"`
}

func Inspect(ctx context.Context, pool *pgxpool.Pool, since, until time.Time, limit int) (Report, error) {
	report := Report{Sections: map[string]Section{}, Limitations: []string{
		"Numeric-recipient collision candidates do not establish sender intent. Never change balances from this report; correlate original request evidence and preserve the ledger.",
		"Unverified source hashes are unknown, not proven corrupt. Shared references are retained. Converted bytes cannot prove a missing original source hash.",
		"Sensitive JSON key matches are triage candidates; values are never returned. PNG chunks, original attachments and third-party downloads are not inspected or recalled.",
		"Historical OSS status, upload logs and file metadata cannot prove an absent activity or intended reward. This report never synthesizes rewards; recover only an existing exact durable intent through its fenced worker.",
		"This tool performs no repair or publication changes. Production execution requires its own authorization and backup/compensation review.",
	}}
	if since.IsZero() || !until.After(since) || limit < 1 || limit > 1000 {
		return report, errors.New("explicit since/until and limit 1..1000 are required")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `set local statement_timeout='30s'`); err != nil {
		return report, err
	}
	var readOnly string
	if err = tx.QueryRow(ctx, `show transaction_read_only`).Scan(&readOnly); err != nil || readOnly != "on" {
		return report, errors.New("read-only transaction was not established")
	}
	report.ReadOnly = true
	run := func(name, query string) error {
		rows, queryErr := tx.Query(ctx, query, since, until, limit)
		if queryErr != nil {
			return fmt.Errorf("diagnose %s: %w", name, queryErr)
		}
		defer rows.Close()
		section := Section{PublicIDs: []json.RawMessage{}}
		for rows.Next() {
			var raw []byte
			if scanErr := rows.Scan(&section.Count, &raw); scanErr != nil {
				return scanErr
			}
			section.PublicIDs = append(section.PublicIDs, json.RawMessage(raw))
		}
		if rows.Err() != nil {
			return rows.Err()
		}
		report.Sections[name] = section
		return nil
	}
	// Leading zeros are normalized as strings, never cast to an integer. Only
	// active/historical identity collisions are candidates; original intent is absent.
	if err = run("transfer_numeric_identity_collision_candidates", `with candidates as (
  select ledger.id,sender.public_id sender_id,recipient.public_id recipient_id,jsonb_agg(distinct possible.public_id) possible_ids
  from currency_transactions ledger join users sender on sender.id=ledger.user_id join users recipient on recipient.id=ledger.counterparty_user_id
  join users possible on possible.id<>recipient.id and (
   (possible.public_id ~ '^[0-9]+$' and ltrim(possible.public_id,'0')=recipient.id::text)
   or (possible.username ~ '^[0-9]+$' and ltrim(possible.username,'0')=recipient.id::text))
  where ledger.transaction_type='transfer_out' and ledger.created_at>=$1 and ledger.created_at<$2
  group by ledger.id,sender.public_id,recipient.public_id)
 select count(*) over(),jsonb_build_object('senderPublicId',sender_id,'actualRecipientPublicId',recipient_id,'possiblePublicIds',possible_ids) from candidates order by id limit $3`); err != nil {
		return report, err
	}
	const fileScope = `file.status='active' and file.created_at>=$1 and file.created_at<$2 and not file.source_hash_verified`
	if err = run("active_unverified_source_files", `select count(*) over(),to_jsonb(file.public_id) from oss_files file where `+fileScope+` order by file.id limit $3`); err != nil {
		return report, err
	}
	if err = run("private_skin_historical_exposure_candidates", `select count(*) over(),to_jsonb(public_id) from skin_assets where status='active' and visibility='private' and created_at>=$1 and created_at<$2 order by id limit $3`); err != nil {
		return report, err
	}
	if err = run("import_json_sensitive_key_candidates", `select count(*) over(),to_jsonb(entity.public_id) from resource_import_snapshots snapshot join catalog_entities entity on entity.id=snapshot.resource_id
  where snapshot.created_at>=$1 and snapshot.created_at<$2 and snapshot.data::text ~* '"(password|token|secret|access_token|refresh_token|private_key|session_id|user_home|absolute_path)"[[:space:]]*:' order by snapshot.id limit $3`); err != nil {
		return report, err
	}
	if err = run("import_png_byte_inspection_required", `select count(*) over(),to_jsonb(file.public_id) from catalog_import_job_artifacts artifact join oss_files file on file.id=artifact.oss_file_id where file.status='active' and file.content_type='image/png' and artifact.created_at>=$1 and artifact.created_at<$2 order by file.id limit $3`); err != nil {
		return report, err
	}
	if err = run("rejected_active_import_projection_candidates", `select count(*) over(),to_jsonb(mod.project_code) from catalog_import_revisions revision join mods mod on mod.id=revision.mod_id where revision.status='rejected' and revision.is_active and revision.created_at>=$1 and revision.created_at<$2 order by revision.id limit $3`); err != nil {
		return report, err
	}
	if err = inspectOSSLifecycle(run); err != nil {
		return report, err
	}
	// Count real FK references without returning object paths, titles or private
	// payloads. Identifiers come from PostgreSQL metadata and are always quoted.
	fkRows, err := tx.Query(ctx, `select child.relname,attribute.attname from pg_constraint constraint_row join pg_class child on child.oid=constraint_row.conrelid join pg_namespace namespace on namespace.oid=child.relnamespace join pg_attribute attribute on attribute.attrelid=child.oid and attribute.attnum=constraint_row.conkey[1] where constraint_row.contype='f' and constraint_row.confrelid='public.oss_files'::regclass and cardinality(constraint_row.conkey)=1 and namespace.nspname='public' order by child.relname,attribute.attname`)
	if err != nil {
		return report, err
	}
	var references [][2]string
	for fkRows.Next() {
		var table, column string
		if err = fkRows.Scan(&table, &column); err != nil {
			fkRows.Close()
			return report, err
		}
		references = append(references, [2]string{table, column})
	}
	err = fkRows.Err()
	fkRows.Close()
	if err != nil {
		return report, err
	}
	for _, ref := range references {
		var count int64
		query := `select count(*) from ` + pgx.Identifier{"public", ref[0]}.Sanitize() + ` linked join public.oss_files file on file.id=linked.` + pgx.Identifier{ref[1]}.Sanitize() + ` where ` + fileScope
		if err = tx.QueryRow(ctx, query, since, until).Scan(&count); err != nil {
			return report, err
		}
		report.Sections["unverified_source_reference_rows:"+ref[0]+"."+ref[1]] = Section{Count: count, PublicIDs: []json.RawMessage{}}
	}
	return report, tx.Commit(ctx)
}
