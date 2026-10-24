package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type preparedFileLogShare struct {
	FileID, SourceSize int64
	Name, Digest       string
	Counts             map[string]int
	Entries            []logShareEntryWrite
	Ready              map[string]any
}
type stagedFileLogShare struct {
	preparedFileLogShare
	Index        int
	PublicFileID string
	TextPaths    []string
}

func (s *Server) prepareFileLogShare(ctx context.Context, userID int64, publicID string) (preparedFileLogShare, error) {
	var p preparedFileLogShare
	var objectKey, contentType string
	err := s.db.QueryRow(ctx, `select id,object_key,coalesce(nullif(source_original_name,''),original_name),content_type,source_size_bytes,sha256
 from oss_files where public_id=$1 and uploader_id=$2 and status='active'
 and (scan_status in ('clean','trusted_generated') or (source in ('log_share','comment') and scan_status='pending'))`, strings.ToLower(strings.TrimSpace(publicID)), userID).Scan(&p.FileID, &objectKey, &p.Name, &contentType, &p.SourceSize, &p.Digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, errors.New("文件不存在、未完成安全检查或不属于当前用户")
	}
	if err != nil {
		return p, errors.New("读取日志文件记录失败，请稍后重试")
	}
	if p.SourceSize <= 0 || p.SourceSize > maxLogSourceBytes {
		return p, fmt.Errorf("文件超过 %d MiB 限制", maxLogSourceBytes>>20)
	}
	var code string
	err = s.db.QueryRow(ctx, `select public_code from log_shares where source_file_id=$1 and redaction_version=$2 and status='ready' and deleted_at is null and expires_at>now()`, p.FileID, logRedactionVersion).Scan(&code)
	if err == nil {
		p.Ready = readyFileLogShare(code)
		return p, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return p, errors.New("读取已有日志分享失败，请稍后重试")
	}
	raw, err := s.readLogOSSObject(ctx, objectKey, p.SourceSize, p.Digest)
	if err != nil {
		return p, err
	}
	p.Entries, p.Counts, err = sanitizeLogFile(p.Name, contentType, raw)
	return p, err
}
func readyFileLogShare(code string) map[string]any {
	return map[string]any{"publicCode": code, "url": "/log/s/" + code, "status": "ready", "reused": true}
}

// Every existing per-file bound is retained. Stage only already-redacted text
// privately, so ten accepted ZIPs do not multiply the live in-memory payload.
// Provider reads/redaction finish before the single business transaction begins.
func (s *Server) createFileLogShareBatch(r *http.Request, publicIDs []string, expiresAt time.Time) ([]map[string]any, error) {
	owner := currentClaims(r).Subject
	items := make([]map[string]any, len(publicIDs))
	prepared := make([]stagedFileLogShare, 0, len(publicIDs))
	var directory string
	var directoryInfo os.FileInfo
	defer func() {
		if directory == "" {
			return
		}
		if directoryInfo == nil {
			slog.Error("sanitized log stage ownership was not established; cleanup refused")
			return
		}
		current, err := os.Lstat(directory)
		if err != nil && !os.IsNotExist(err) {
			slog.Error("sanitized log stage ownership check failed")
			return
		}
		if os.IsNotExist(err) {
			return
		}
		if !current.IsDir() || current.Mode().Perm()&0077 != 0 || !os.SameFile(directoryInfo, current) {
			slog.Error("sanitized log stage ownership changed; cleanup refused")
			return
		}
		if err = os.RemoveAll(directory); err != nil {
			slog.Error("sanitized log stage cleanup failed")
		}
	}()
	for index, publicID := range publicIDs {
		items[index] = map[string]any{"fileId": publicID, "status": "failed"}
		p, err := s.prepareFileLogShare(r.Context(), owner, publicID)
		if err != nil {
			items[index]["error"] = err.Error()
			continue
		}
		staged := stagedFileLogShare{preparedFileLogShare: p, Index: index, PublicFileID: publicID}
		if p.Ready == nil {
			if directory == "" {
				directory, err = os.MkdirTemp("", "mcmods-sanitized-log-batch-")
				if err != nil {
					return nil, err
				}
				directoryInfo, err = os.Lstat(directory)
				if err != nil {
					return nil, err
				}
			}
			for entryIndex := range staged.Entries {
				name := filepath.Join(directory, fmt.Sprintf("%d-%d.txt", index, entryIndex))
				file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					return nil, err
				}
				_, writeErr := file.WriteString(staged.Entries[entryIndex].Text)
				closeErr := file.Close()
				if writeErr != nil {
					return nil, writeErr
				}
				if closeErr != nil {
					return nil, closeErr
				}
				staged.TextPaths = append(staged.TextPaths, name)
				staged.Entries[entryIndex].Text = ""
			}
		}
		prepared = append(prepared, staged)
	}
	if len(prepared) == 0 {
		skipRequestActivity(r)
		return items, nil
	}
	// All batches acquire source/unique-share locks in the same order, independent
	// of input order. Result slots retain the caller's original order and aliases.
	sort.SliceStable(prepared, func(i, j int) bool { return prepared[i].FileID < prepared[j].FileID })
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(r.Context())
	created := 0
	for _, p := range prepared {
		var valid bool
		err = tx.QueryRow(r.Context(), `select uploader_id=$2 and status='active' and source_size_bytes=$3 and sha256=$4 and (scan_status in ('clean','trusted_generated') or (source in ('log_share','comment') and scan_status='pending')) from oss_files where id=$1 for share`, p.FileID, owner, p.SourceSize, p.Digest).Scan(&valid)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !valid {
			items[p.Index]["error"] = "日志文件状态已变化，请重试"
			continue
		}
		if err != nil {
			return nil, err
		}
		var code string
		err = tx.QueryRow(r.Context(), `select public_code from log_shares where source_file_id=$1 and redaction_version=$2 and status='ready' and deleted_at is null and expires_at>now()`, p.FileID, logRedactionVersion).Scan(&code)
		if err == nil {
			items[p.Index] = readyFileLogShare(code)
			items[p.Index]["fileId"] = p.PublicFileID
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if p.Ready != nil {
			items[p.Index]["error"] = "已有日志分享状态已变化，请重试"
			continue
		}
		entries := append([]logShareEntryWrite(nil), p.Entries...)
		for i, name := range p.TextPaths {
			if err = r.Context().Err(); err != nil {
				return nil, err
			}
			file, openErr := os.Open(name)
			if openErr != nil {
				return nil, openErr
			}
			data, readErr := io.ReadAll(io.LimitReader(file, entries[i].Size+1))
			closeErr := file.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			hash := sha256.Sum256(data)
			if int64(len(data)) != entries[i].Size || hex.EncodeToString(hash[:]) != entries[i].Checksum {
				return nil, errors.New("sanitized log stage changed")
			}
			entries[i].Text = string(data)
		}
		savepoint, beginErr := tx.Begin(r.Context())
		if beginErr != nil {
			return nil, beginErr
		}
		result, persistErr := s.persistReadyLogShareTx(r.Context(), savepoint, owner, "file", p.FileID, p.Name, p.Name, expiresAt, p.Counts, entries)
		if persistErr != nil {
			if rollbackErr := savepoint.Rollback(r.Context()); rollbackErr != nil {
				return nil, rollbackErr
			}
			if !isUniqueViolation(persistErr) {
				return nil, persistErr
			}
			// A concurrent winner is visible to the next Read Committed statement.
			if err = tx.QueryRow(r.Context(), `select public_code from log_shares where source_file_id=$1 and redaction_version=$2 and status='ready' and deleted_at is null and expires_at>now()`, p.FileID, logRedactionVersion).Scan(&code); err != nil {
				var conflict *pgconn.PgError
				// The active-source index also covers processing and elapsed ready
				// rows. Preserve their per-file conflict without changing existing
				// shares or discarding an unrelated accepted file in this batch.
				if errors.Is(err, pgx.ErrNoRows) && errors.As(persistErr, &conflict) && conflict.ConstraintName == "idx_log_shares_active_source_file" {
					items[p.Index]["error"] = "该日志文件已有正在处理或已过期的分享，请稍后重试"
					continue
				}
				return nil, persistErr
			}
			result = readyFileLogShare(code)
		} else {
			if err = savepoint.Commit(r.Context()); err != nil {
				return nil, err
			}
			created++
		}
		items[p.Index] = result
		items[p.Index]["fileId"] = p.PublicFileID
	}
	if created == 0 {
		skipRequestActivity(r)
	} else if err = enqueueRequestActivityTx(r, tx); err != nil {
		return nil, err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return nil, err
	}
	return items, nil
}
