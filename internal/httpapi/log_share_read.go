package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	logShareChunkCursorVersion       = 1
	maxLogShareChunkRunes            = 32 << 10
	maxLogShareChunkResponseBytes    = 256 << 10
	maxLogShareMetadataResponseBytes = 256 << 10
	maxLogShareChunkCursorBytes      = 1024
	maxLogShareChunkOffset           = int(maxLogUncompressed)
	maxConcurrentLogShareBodyReads   = 8
)

type logShareChunkCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	Offset  int    `json:"o"`
}

type logShareEntryMetadata struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	ByteSize    int64  `json:"byteSize"`
	LineCount   int64  `json:"lineCount"`
	Checksum    string `json:"checksum"`
}

type logShareChunkPage struct {
	Text            string `json:"text"`
	CharacterOffset int    `json:"characterOffset"`
	HasMore         bool   `json:"hasMore"`
	NextCursor      string `json:"nextCursor"`
}

type logShareRecord struct {
	ID               int64
	OwnerID          *int64
	PublicCode       string
	SourceType       string
	Title            string
	OriginalName     string
	Status           string
	RedactionVersion int
	RedactionCounts  map[string]int
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

type publicLogShareResponse struct {
	PublicCode       string                  `json:"publicCode"`
	SourceType       string                  `json:"sourceType"`
	Title            string                  `json:"title"`
	OriginalName     string                  `json:"originalName"`
	Status           string                  `json:"status"`
	RedactionVersion int                     `json:"redactionVersion"`
	RedactionCounts  map[string]int          `json:"redactionCounts"`
	CreatedAt        time.Time               `json:"createdAt"`
	ExpiresAt        time.Time               `json:"expiresAt"`
	Downloadable     bool                    `json:"downloadable"`
	Entries          []logShareEntryMetadata `json:"entries"`
}

func logShareChunkCursorScope(code string, entryIndex int) string {
	material, _ := json.Marshal(struct {
		Version    int    `json:"version"`
		PublicCode string `json:"publicCode"`
		EntryIndex int    `json:"entryIndex"`
	}{logShareChunkCursorVersion, strings.TrimSpace(code), entryIndex})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeLogShareChunkCursor(scope string, offset int) string {
	raw, _ := json.Marshal(logShareChunkCursor{Version: logShareChunkCursorVersion, Scope: scope, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeLogShareChunkCursor(raw, scope string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if len(raw) > maxLogShareChunkCursorBytes {
		return 0, errors.New("invalid log share chunk cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, errors.New("invalid log share chunk cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor logShareChunkCursor
	if err = decoder.Decode(&cursor); err != nil {
		return 0, errors.New("invalid log share chunk cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != logShareChunkCursorVersion ||
		cursor.Scope != scope || cursor.Offset <= 0 || cursor.Offset > maxLogShareChunkOffset {
		return 0, errors.New("invalid log share chunk cursor")
	}
	return cursor.Offset, nil
}

func makeLogShareChunkPage(raw string, characterOffset int, scope string) logShareChunkPage {
	runes := []rune(raw)
	hasMore := len(runes) > maxLogShareChunkRunes
	if hasMore {
		runes = runes[:maxLogShareChunkRunes]
	}
	page := logShareChunkPage{Text: string(runes), CharacterOffset: characterOffset, HasMore: hasMore}
	if hasMore {
		page.NextCursor = encodeLogShareChunkCursor(scope, characterOffset+len(runes))
	}
	return page
}

func (s *Server) loadLogShareRecord(ctx context.Context, code string, ownerOnly bool, viewerID int64) (logShareRecord, error) {
	code = strings.TrimSpace(code)
	var record logShareRecord
	var counts []byte
	err := s.db.QueryRow(ctx, `select id,owner_user_id,public_code,source_type,left(title,200),left(original_name,512),status,
		redaction_version,redaction_counts,created_at,expires_at from log_shares where public_code=$1`, code).
		Scan(&record.ID, &record.OwnerID, &record.PublicCode, &record.SourceType, &record.Title, &record.OriginalName,
			&record.Status, &record.RedactionVersion, &counts, &record.CreatedAt, &record.ExpiresAt)
	if err != nil {
		return logShareRecord{}, err
	}
	if ownerOnly && (record.OwnerID == nil || *record.OwnerID != viewerID) {
		return logShareRecord{}, pgx.ErrNoRows
	}
	if record.Status != "ready" || !record.ExpiresAt.After(time.Now()) {
		return logShareRecord{}, errLogShareGone
	}
	if len(counts) > 16<<10 || json.Unmarshal(counts, &record.RedactionCounts) != nil {
		return logShareRecord{}, errors.New("invalid log share redaction counts")
	}
	if record.RedactionCounts == nil {
		record.RedactionCounts = map[string]int{}
	}
	return record, nil
}

func (s *Server) loadLogShareMetadata(ctx context.Context, code string, ownerOnly bool, viewerID int64) (publicLogShareResponse, logShareRecord, error) {
	record, err := s.loadLogShareRecord(ctx, code, ownerOnly, viewerID)
	if err != nil {
		return publicLogShareResponse{}, logShareRecord{}, err
	}
	rows, err := s.db.Query(ctx, `select entry_index,left(safe_display_name,512),left(content_type,128),byte_size,line_count,left(checksum,128)
		from log_share_entries where log_share_id=$1 and status='ready' order by entry_index limit $2`, record.ID, maxLogArchiveFiles+1)
	if err != nil {
		return publicLogShareResponse{}, logShareRecord{}, err
	}
	defer rows.Close()
	entries := make([]logShareEntryMetadata, 0)
	for rows.Next() {
		var entry logShareEntryMetadata
		if err = rows.Scan(&entry.Index, &entry.Name, &entry.ContentType, &entry.ByteSize, &entry.LineCount, &entry.Checksum); err != nil {
			return publicLogShareResponse{}, logShareRecord{}, err
		}
		entries = append(entries, entry)
	}
	if err = rows.Err(); err != nil {
		return publicLogShareResponse{}, logShareRecord{}, err
	}
	if len(entries) == 0 || len(entries) > maxLogArchiveFiles {
		return publicLogShareResponse{}, logShareRecord{}, errors.New("invalid log share entry count")
	}
	response := publicLogShareResponse{
		PublicCode: record.PublicCode, SourceType: record.SourceType, Title: record.Title, OriginalName: record.OriginalName,
		Status: record.Status, RedactionVersion: record.RedactionVersion, RedactionCounts: record.RedactionCounts,
		CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt, Downloadable: record.SourceType == "file", Entries: entries,
	}
	return response, record, nil
}

func (s *Server) publicLogShareEntryContent(w http.ResponseWriter, r *http.Request) {
	release, ok := s.acquireLogShareBodyRead(w, r, "log-share-chunk", 60)
	if !ok {
		return
	}
	defer release()
	entryIndex, err := strconv.Atoi(strings.TrimSpace(r.PathValue("entryIndex")))
	if err != nil || entryIndex < 0 || entryIndex >= maxLogArchiveFiles {
		writeAPIError(w, http.StatusBadRequest, "LOG_SHARE_ENTRY_INVALID", "invalid log share entry", 0, nil)
		return
	}
	values := r.URL.Query()
	for key, entries := range values {
		if key != "cursor" || len(entries) != 1 {
			writeAPIError(w, http.StatusBadRequest, "LOG_SHARE_CHUNK_QUERY_INVALID", "invalid log share chunk query", 0, nil)
			return
		}
	}
	code := strings.TrimSpace(r.PathValue("code"))
	scope := logShareChunkCursorScope(code, entryIndex)
	offset, err := decodeLogShareChunkCursor(values.Get("cursor"), scope)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "LOG_SHARE_CHUNK_CURSOR_INVALID", "invalid log share chunk cursor", 0, nil)
		return
	}
	var raw string
	err = s.db.QueryRow(r.Context(), `select substring(entry.sanitized_text from $3::integer for $4::integer)
		from log_shares share join log_share_entries entry on entry.log_share_id=share.id
		where share.public_code=$1 and share.status='ready' and share.deleted_at is null and share.expires_at>now()
		 and entry.entry_index=$2 and entry.status='ready' and entry.sanitized_text is not null`,
		code, entryIndex, offset+1, maxLogShareChunkRunes+1).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "日志不存在、已失效或条目不可读")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取脱敏日志分块失败")
		return
	}
	page := makeLogShareChunkPage(raw, offset, scope)
	payload, err := json.Marshal(apiResponse{Data: page})
	if err != nil || len(payload)+1 > maxLogShareChunkResponseBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "LOG_SHARE_CHUNK_RESPONSE_BUDGET", "log share chunk exceeds response budget", 0, nil)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	writeJSONBytes(w, http.StatusOK, append(payload, '\n'))
}

func (s *Server) allowLogShareRead(w http.ResponseWriter, r *http.Request, scope string, anonymousLimit int) bool {
	if s.antiAbuse == nil || !s.antiAbuse.Enabled() {
		return true
	}
	claims := currentClaims(r)
	identity := s.requestClientLocation(r).IP
	limit := anonymousLimit
	if claims.Subject > 0 {
		identity += fmt.Sprintf(":user:%d", claims.Subject)
		limit *= 2
	}
	allowed, retry := s.antiAbuse.ExpensiveReadLimit(r.Context(), scope, identity, limit, time.Minute)
	if allowed {
		return true
	}
	seconds := max(1, int(retry.Round(time.Second).Seconds()))
	writeAPIError(w, http.StatusTooManyRequests, "LOG_SHARE_READ_RATE_LIMIT", "log share read rate exceeded", seconds, nil)
	return false
}

func (s *Server) acquireLogShareBodyRead(w http.ResponseWriter, r *http.Request, scope string, anonymousLimit int) (func(), bool) {
	if !s.allowLogShareRead(w, r, scope, anonymousLimit) {
		return nil, false
	}
	if s.logShareBodyReads == nil {
		return func() {}, true
	}
	select {
	case s.logShareBodyReads <- struct{}{}:
		return func() { <-s.logShareBodyReads }, true
	default:
		writeAPIError(w, http.StatusServiceUnavailable, "LOG_SHARE_READ_BUSY", "too many concurrent log share reads", 1, nil)
		return nil, false
	}
}
