package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type preparedFileLogShare struct {
	FileID, SourceSize int64
	CommentScopeID     *int64
	Name, Digest       string
	Counts             map[string]int
	Entries            []logShareEntryWrite
	TextPaths          []string
	AlreadyReady       bool
}

type publishedLogShare struct {
	id       int64
	created  bool
	response map[string]any
}

type fileLogShareConflict string

func (e fileLogShareConflict) Error() string { return string(e) }

type logShareQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const eligibleLogShareSourceSQL = `status='active' and (scan_status in ('clean','trusted_generated') or (source in ('log_share','comment') and scan_status='pending'))`

type logSourceReader func(context.Context, string, int64, string) ([]byte, error)

func prepareFileLogShare(ctx context.Context, db logShareQueryRower, readSource logSourceReader, ownerID int64, publicID string, commentScopeID *int64) (preparedFileLogShare, error) {
	p := preparedFileLogShare{CommentScopeID: commentScopeID}
	var objectKey, contentType string
	err := db.QueryRow(ctx, `select id,object_key,coalesce(nullif(source_original_name,''),original_name),content_type,source_size_bytes,sha256
		from oss_files where public_id=$1 and uploader_id=$2 and `+eligibleLogShareSourceSQL,
		strings.ToLower(strings.TrimSpace(publicID)), ownerID).Scan(&p.FileID, &objectKey, &p.Name, &contentType, &p.SourceSize, &p.Digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, errors.New("文件不存在、未完成安全检查或不属于当前用户")
	}
	if err != nil {
		return p, errors.New("读取日志文件记录失败，请稍后重试")
	}
	if p.SourceSize <= 0 || p.SourceSize > maxLogSourceBytes {
		return p, fmt.Errorf("文件超过 %d MiB 限制", maxLogSourceBytes>>20)
	}
	_, err = readyLogShareForSource(ctx, db, ownerID, p.FileID, p.CommentScopeID)
	if err == nil {
		p.AlreadyReady = true
		return p, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return p, errors.New("读取已有日志分享失败，请稍后重试")
	}
	raw, err := readSource(ctx, objectKey, p.SourceSize, p.Digest)
	if err != nil {
		return p, err
	}
	p.Entries, p.Counts, err = sanitizeLogFile(p.Name, contentType, raw)
	return p, err
}

func readyLogShareForSource(ctx context.Context, db logShareQueryRower, ownerID, fileID int64, commentScopeID *int64) (publishedLogShare, error) {
	var share publishedLogShare
	var code string
	err := db.QueryRow(ctx, `select id,public_code from log_shares
		where owner_user_id=$1 and source_file_id=$2 and redaction_version=$3 and status='ready'
		and comment_scope_id is not distinct from $4 and deleted_at is null and expires_at>now() for share`,
		ownerID, fileID, logRedactionVersion, commentScopeID).Scan(&share.id, &code)
	if err == nil {
		share.response = map[string]any{"publicCode": code, "url": "/log/s/" + code, "status": "ready", "reused": true}
	}
	return share, err
}

// The caller commits the publication with either its request activity or the
// attachment binding and job lease. Provider I/O and redaction precede it.
func publishFileLogShareTx(ctx context.Context, tx pgx.Tx, ownerID int64, p preparedFileLogShare, expiresAt time.Time) (publishedLogShare, error) {
	var valid bool
	err := tx.QueryRow(ctx, `select uploader_id=$2 and source_size_bytes=$3 and sha256=$4 and `+eligibleLogShareSourceSQL+`
		from oss_files where id=$1 for share`, p.FileID, ownerID, p.SourceSize, p.Digest).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !valid {
		return publishedLogShare{}, fileLogShareConflict("日志文件状态已变化，请重试")
	}
	if err != nil {
		return publishedLogShare{}, err
	}
	share, err := readyLogShareForSource(ctx, tx, ownerID, p.FileID, p.CommentScopeID)
	if !errors.Is(err, pgx.ErrNoRows) {
		return share, err
	}
	if p.AlreadyReady {
		return publishedLogShare{}, fileLogShareConflict("已有日志分享状态已变化，请重试")
	}
	entries, err := p.loadEntries(ctx)
	if err != nil {
		return publishedLogShare{}, err
	}
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return publishedLogShare{}, err
	}
	share, persistErr := persistReadyLogShareTx(ctx, savepoint, ownerID, "file", p.FileID, p.CommentScopeID, p.Name, p.Name, expiresAt, p.Counts, entries)
	if persistErr == nil {
		return share, savepoint.Commit(ctx)
	}
	if err = savepoint.Rollback(ctx); err != nil {
		return publishedLogShare{}, err
	}
	if !isUniqueViolation(persistErr) {
		return publishedLogShare{}, persistErr
	}
	// Re-read the concurrent winner after rolling back the savepoint. The
	// same unique index can be occupied by an elapsed or processing share.
	share, err = readyLogShareForSource(ctx, tx, ownerID, p.FileID, p.CommentScopeID)
	if err == nil {
		return share, nil
	}
	var conflict *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) && errors.As(persistErr, &conflict) && conflict.ConstraintName == "idx_log_shares_active_source_file" {
		return publishedLogShare{}, fileLogShareConflict("该日志文件已有正在处理或已过期的分享，请稍后重试")
	}
	return publishedLogShare{}, persistErr
}

func (p preparedFileLogShare) loadEntries(ctx context.Context) ([]logShareEntryWrite, error) {
	if len(p.TextPaths) == 0 {
		return p.Entries, nil
	}
	entries := append([]logShareEntryWrite(nil), p.Entries...)
	for i, name := range p.TextPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := os.Open(name)
		if err != nil {
			return nil, err
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
	return entries, nil
}
