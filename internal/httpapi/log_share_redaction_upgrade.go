package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var logShareRedactionUpgradeSlots = make(chan struct{}, 2)

// Existing public codes remain valid. Reprocess only already-sanitized text;
// never fetch the original private OSS file or restore previously hidden data.
func (s *Server) upgradeLogShareRedaction(ctx context.Context, shareID int64) error {
	select {
	case logShareRedactionUpgradeSlots <- struct{}{}:
		defer func() { <-logShareRedactionUpgradeSlots }()
	default:
		return errors.New("log share redaction upgrade is busy")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `set local lock_timeout='2s'`); err != nil {
		return err
	}
	var applied int
	var countsJSON []byte
	err = tx.QueryRow(ctx, `select greatest(redaction_version,redaction_applied_version),redaction_counts from log_shares
		where id=$1 and status='ready' and deleted_at is null and expires_at>now() for update`, shareID).Scan(&applied, &countsJSON)
	if err != nil {
		return err
	}
	if applied >= logRedactionVersion {
		return tx.Commit(ctx)
	}
	counts := map[string]int{}
	if len(countsJSON) > 16<<10 || json.Unmarshal(countsJSON, &counts) != nil {
		return errors.New("invalid log share redaction counts")
	}
	if counts == nil {
		counts = map[string]int{}
	}
	var count int
	var size, largest int64
	var inline bool
	if err = tx.QueryRow(ctx, `select count(*),coalesce(sum(octet_length(sanitized_text)),0),coalesce(max(octet_length(sanitized_text)),0),coalesce(bool_and(sanitized_text is not null),false)
		from log_share_entries where log_share_id=$1 and status='ready'`, shareID).Scan(&count, &size, &largest, &inline); err != nil {
		return err
	}
	if count < 1 || count > maxLogArchiveFiles || size > maxLogUncompressed || largest > maxLogSourceBytes || !inline {
		return errors.New("log share exceeds redaction upgrade bounds")
	}
	rows, err := tx.Query(ctx, `select entry_index from log_share_entries where log_share_id=$1 and status='ready' order by entry_index`, shareID)
	if err != nil {
		return err
	}
	indices := make([]int, 0, count)
	for rows.Next() {
		var index int
		if err = rows.Scan(&index); err != nil {
			rows.Close()
			return err
		}
		indices = append(indices, index)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, index := range indices {
		var text string
		if err = tx.QueryRow(ctx, `select sanitized_text from log_share_entries where log_share_id=$1 and entry_index=$2 and status='ready' for update`, shareID, index).Scan(&text); err != nil {
			return err
		}
		if int64(len(text)) > maxLogSourceBytes {
			return errors.New("log share entry exceeds redaction upgrade bound")
		}
		sanitized, extra := redactLogText(text)
		entry := makeLogShareEntry("", "", sanitized)
		for key, value := range extra {
			counts[key] += value
		}
		command, writeErr := tx.Exec(ctx, `update log_share_entries set sanitized_text=$3,byte_size=$4,line_count=$5,checksum=$6 where log_share_id=$1 and entry_index=$2 and status='ready'`, shareID, index, entry.Text, entry.Size, entry.Lines, entry.Checksum)
		if writeErr != nil {
			return writeErr
		}
		if command.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
	}
	countsJSON, err = json.Marshal(counts)
	if err != nil {
		return err
	}
	if len(countsJSON) > 16<<10 {
		return errors.New("log share redaction counts exceed bound")
	}
	command, err := tx.Exec(ctx, `update log_shares set redaction_applied_version=$2,redaction_counts=$3::jsonb where id=$1`, shareID, logRedactionVersion, string(countsJSON))
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return tx.Commit(ctx)
}
