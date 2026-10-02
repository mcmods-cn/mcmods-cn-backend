package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	logRedactionVersion    = 2
	maxLogSourceBytes      = int64(20 << 20)
	maxLogUncompressed     = int64(100 << 20)
	maxLogArchiveFiles     = 200
	maxLogArchiveDepth     = 12
	maxLogCompressionRatio = 200
	maxPasteLogRunes       = 1_000_000
)

type logShareEntryWrite struct {
	Name, ContentType, Text, Checksum string
	Size, Lines                       int64
}

type createPasteLogShareRequest struct {
	Title         string `json:"title"`
	Content       string `json:"content"`
	RetentionDays int    `json:"retentionDays"`
}

type createFileLogSharesRequest struct {
	FileIDs       []string `json:"fileIds"`
	RetentionDays int      `json:"retentionDays"`
}

var logRedactionRules = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"authorization", regexp.MustCompile(`(?im)(authorization\s*:\s*(?:bearer|basic)\s+)[^\s]+`)},
	{"secret_field", regexp.MustCompile(`(?im)(["']?\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|session[_-]?token|client[_-]?secret|password|passwd|redis[_-]?password|nats[_-]?(?:token|creds))\b["']?\s*[:=]\s*)("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;}\]]+)`)},
	{"connection_userinfo", regexp.MustCompile(`(?i)((?:postgres(?:ql)?|mysql|redis|rediss|nats)://)[^/@\s]+@`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
	{"github_token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{30,})\b`)},
	{"discord_token", regexp.MustCompile(`\b[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{20,}\b`)},
	{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,63}\b`)},
	{"phone", regexp.MustCompile(`\b(?:\+?86[- ]?)?1[3-9]\d{9}\b`)},
	{"ipv4", regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}\b`)},
	{"ipv6", regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){2,7}[0-9a-f]{0,4}\b`)},
	{"mac", regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b`)},
	{"windows_home", regexp.MustCompile(`(?i)([A-Z]:\\Users\\)[^\\\r\n]+`)},
	{"unix_home", regexp.MustCompile(`(?m)(/(?:home|Users)/)[^/\s]+`)},
	{"url_secret", regexp.MustCompile(`(?i)([?&](?:token|key|secret|access_token|refresh_token)=)[^&#\s]+`)},
	{"cookie", regexp.MustCompile(`(?im)(cookie\s*:\s*)[^\r\n]+`)},
}

func redactLogText(input string) (string, map[string]int) {
	input = strings.ToValidUTF8(input, "�")
	input = stripANSIControlSequences(input)
	counts := make(map[string]int)
	for _, rule := range logRedactionRules {
		input = rule.pattern.ReplaceAllStringFunc(input, func(value string) string {
			replacement := "❄"
			if match := rule.pattern.FindStringSubmatch(value); len(match) > 1 && match[1] != "" {
				if rule.name == "connection_userinfo" {
					replacement = match[1] + "❄@"
				} else if rule.name == "secret_field" && len(match) > 2 && (strings.HasPrefix(match[2], "\"") || strings.HasPrefix(match[2], "'")) {
					replacement = match[1] + match[2][:1] + "❄" + match[2][:1]
				} else {
					replacement = match[1] + "❄"
				}
			}
			if replacement != value {
				counts[rule.name]++
			}
			return replacement
		})
	}
	return input, counts
}

var ansiControlSequence = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

func stripANSIControlSequences(value string) string {
	value = ansiControlSequence.ReplaceAllString(value, "")
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' || character >= 0x20 {
			return character
		}
		return -1
	}, value)
}

func (s *Server) createPasteLogShare(w http.ResponseWriter, r *http.Request) {
	var request createPasteLogShareRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "日志请求格式不正确")
		return
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" || utf8.RuneCountInString(request.Content) > maxPasteLogRunes {
		writeError(w, http.StatusBadRequest, "日志内容为空或超过大小限制")
		return
	}
	expiresAt, err := logShareExpiry(time.Now(), request.RetentionDays)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sanitized, counts := redactLogText(request.Content)
	entry := makeLogShareEntry("pasted.log", "text/plain; charset=utf-8", sanitized)
	claims := currentClaims(r)
	result, err := s.persistReadyLogShare(r.Context(), claims.Subject, "paste", 0, request.Title, "", expiresAt, counts, []logShareEntryWrite{entry})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存脱敏日志失败")
		return
	}
	s.writeAppLog(context.Background(), "user_interaction", "info", "log_share.create", result["publicCode"].(string), claims.Subject, r, http.StatusCreated, 0, map[string]any{"sourceType": "paste"})
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) createFileLogShares(w http.ResponseWriter, r *http.Request) {
	var request createFileLogSharesRequest
	if decodeJSON(r, &request) != nil || len(request.FileIDs) == 0 || len(request.FileIDs) > 10 {
		writeError(w, http.StatusBadRequest, "请选择 1 至 10 个日志文件")
		return
	}
	expiresAt, err := logShareExpiry(time.Now(), request.RetentionDays)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	claims := currentClaims(r)
	items := make([]map[string]any, 0, len(request.FileIDs))
	for _, fileID := range request.FileIDs {
		item := map[string]any{"fileId": fileID, "status": "failed"}
		result, processErr := s.createFileLogShare(r.Context(), claims.Subject, fileID, expiresAt)
		if processErr != nil {
			item["error"] = processErr.Error()
		} else {
			item = result
			item["fileId"] = fileID
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusMultiStatus, map[string]any{"items": items})
}

func (s *Server) createFileLogShare(ctx context.Context, userID int64, publicFileID string, expiresAt time.Time) (map[string]any, error) {
	var fileID, size int64
	var objectKey, name, contentType, digest string
	err := s.db.QueryRow(ctx, `select id,object_key,coalesce(nullif(source_original_name,''),original_name),content_type,source_size_bytes,sha256
		from oss_files where public_id=$1 and uploader_id=$2 and status='active'
		  and (scan_status in ('clean','trusted_generated') or (source in ('log_share','comment') and scan_status='pending'))`, strings.ToLower(strings.TrimSpace(publicFileID)), userID).
		Scan(&fileID, &objectKey, &name, &contentType, &size, &digest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("文件不存在、未完成安全检查或不属于当前用户")
		}
		return nil, errors.New("读取日志文件记录失败，请稍后重试")
	}
	if size <= 0 || size > maxLogSourceBytes {
		return nil, fmt.Errorf("文件超过 %d MiB 限制", maxLogSourceBytes>>20)
	}
	var existingCode string
	lookupErr := s.db.QueryRow(ctx, `select public_code from log_shares where source_file_id=$1 and redaction_version=$2
		and status='ready' and deleted_at is null and expires_at>now()`, fileID, logRedactionVersion).Scan(&existingCode)
	if lookupErr == nil {
		return map[string]any{"publicCode": existingCode, "url": "/log/s/" + existingCode, "status": "ready", "reused": true}, nil
	}
	if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return nil, errors.New("读取已有日志分享失败，请稍后重试")
	}
	raw, err := s.readLogOSSObject(ctx, objectKey, size, digest)
	if err != nil {
		return nil, err
	}
	entries, counts, err := sanitizeLogFile(name, contentType, raw)
	if err != nil {
		return nil, err
	}
	result, err := s.persistReadyLogShare(ctx, userID, "file", fileID, name, name, expiresAt, counts, entries)
	if err == nil {
		return result, nil
	}
	if !isUniqueViolation(err) {
		return nil, errors.New("保存脱敏日志失败，请稍后重试")
	}
	// The partial unique index is the concurrency authority. If another
	// request sanitized the same source file first, return that share instead
	// of surfacing a spurious batch failure.
	if s.db.QueryRow(ctx, `select public_code from log_shares where source_file_id=$1 and redaction_version=$2
		and status='ready' and deleted_at is null and expires_at>now()`, fileID, logRedactionVersion).Scan(&existingCode) == nil {
		return map[string]any{"publicCode": existingCode, "url": "/log/s/" + existingCode, "status": "ready", "reused": true}, nil
	}
	return nil, errors.New("保存脱敏日志失败，请稍后重试")
}

func (s *Server) readLogOSSObject(ctx context.Context, objectKey string, expectedSize int64, expectedDigest string) ([]byte, error) {
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return nil, errors.New("对象存储暂时不可用")
	}
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		return nil, errors.New("读取日志文件失败")
	}
	defer result.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(result.Body, maxLogSourceBytes+1))
	if err != nil || int64(len(raw)) > maxLogSourceBytes || int64(len(raw)) != expectedSize {
		return nil, errors.New("日志文件大小校验失败")
	}
	if expectedDigest != "" {
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != expectedDigest {
			return nil, errors.New("日志文件摘要校验失败")
		}
	}
	return raw, nil
}

func sanitizeLogFile(name, contentType string, raw []byte) ([]logShareEntryWrite, map[string]int, error) {
	extension := strings.ToLower(filepath.Ext(filepath.Base(name)))
	switch extension {
	case ".log", ".txt":
		text, err := decodeLogText(raw)
		if err != nil {
			return nil, nil, err
		}
		sanitized, counts := redactLogText(text)
		return []logShareEntryWrite{makeLogShareEntry(filepath.Base(name), "text/plain; charset=utf-8", sanitized)}, counts, nil
	case ".zip":
		if len(raw) < 4 || !bytes.Equal(raw[:4], []byte{'P', 'K', 3, 4}) || !(strings.Contains(strings.ToLower(contentType), "zip") || strings.Contains(strings.ToLower(contentType), "octet-stream")) {
			return nil, nil, errors.New("文件不是有效 ZIP")
		}
		return sanitizeLogZip(raw)
	default:
		return nil, nil, errors.New("仅支持 .zip、.log 和 .txt")
	}
}

func sanitizeLogZip(raw []byte) ([]logShareEntryWrite, map[string]int, error) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(archive.File) > maxLogArchiveFiles {
		return nil, nil, errors.New("ZIP 无效或文件数量过多")
	}
	entries := make([]logShareEntryWrite, 0, len(archive.File))
	counts := map[string]int{}
	seenNames := make(map[string]struct{}, len(archive.File))
	var total int64
	for _, file := range archive.File {
		clean, safePath := portableArchiveEntryName(file.Name, maxLogArchiveDepth)
		unsafeMode := file.Mode() & (os.ModeSymlink | os.ModeDevice | os.ModeCharDevice | os.ModeNamedPipe | os.ModeSocket)
		if !safePath || unsafeMode != 0 {
			return nil, nil, errors.New("ZIP 包含不安全路径或符号链接")
		}
		if file.Flags&0x1 != 0 {
			return nil, nil, errors.New("不支持加密 ZIP")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		nameKey := strings.ToLower(clean)
		if _, duplicate := seenNames[nameKey]; duplicate {
			return nil, nil, errors.New("ZIP 包含重复文件名")
		}
		seenNames[nameKey] = struct{}{}
		if strings.EqualFold(filepath.Ext(clean), ".zip") {
			return nil, nil, errors.New("不支持嵌套 ZIP")
		}
		extension := strings.ToLower(filepath.Ext(clean))
		if !stringSet(".log", ".txt", ".json", ".cfg", ".properties", ".toml", ".yml", ".yaml", ".xml", ".md")[extension] {
			continue
		}
		if file.UncompressedSize64 > uint64(maxLogSourceBytes) || file.CompressedSize64 > uint64(maxLogSourceBytes) {
			return nil, nil, errors.New("ZIP 文件大小超过安全限制")
		}
		uncompressed := int64(file.UncompressedSize64)
		compressed := max(int64(file.CompressedSize64), 1)
		if uncompressed > maxLogSourceBytes || uncompressed/compressed > maxLogCompressionRatio {
			return nil, nil, errors.New("ZIP 文件大小或压缩比超过安全限制")
		}
		total += uncompressed
		if total > maxLogUncompressed {
			return nil, nil, errors.New("ZIP 解压总大小超过安全限制")
		}
		reader, openErr := file.Open()
		if openErr != nil {
			return nil, nil, errors.New("ZIP 条目无法读取")
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxLogSourceBytes+1))
		reader.Close()
		if readErr != nil || int64(len(data)) != uncompressed {
			return nil, nil, errors.New("ZIP 条目读取不完整")
		}
		text, decodeErr := decodeLogText(data)
		if decodeErr != nil {
			continue
		}
		sanitized, entryCounts := redactLogText(text)
		for key, count := range entryCounts {
			counts[key] += count
		}
		entry := makeLogShareEntry(clean, mime.TypeByExtension(extension), sanitized)
		entry.Name = clean
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, nil, errors.New("ZIP 中没有可安全分享的文本日志")
	}
	return entries, counts, nil
}

func decodeLogText(raw []byte) (string, error) {
	if bytes.IndexByte(raw, 0) >= 0 {
		return "", errors.New("日志包含二进制内容")
	}
	if utf8.Valid(raw) {
		return string(raw), nil
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), raw)
	if err != nil || !utf8.Valid(decoded) {
		return "", errors.New("日志编码不受支持")
	}
	return string(decoded), nil
}

func makeLogShareEntry(name, contentType, text string) logShareEntryWrite {
	digest := sha256.Sum256([]byte(text))
	return logShareEntryWrite{Name: filepath.Base(name), ContentType: contentType, Text: text,
		Checksum: hex.EncodeToString(digest[:]), Size: int64(len(text)), Lines: int64(strings.Count(text, "\n") + 1)}
}

func (s *Server) persistReadyLogShare(ctx context.Context, ownerID int64, sourceType string, sourceFileID int64, title, originalName string, expiresAt time.Time, counts map[string]int, entries []logShareEntryWrite) (map[string]any, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	code, err := randomLogShareCode()
	if err != nil {
		return nil, err
	}
	var shareID int64
	var owner, sourceFile any
	if ownerID > 0 {
		owner = ownerID
	}
	if sourceFileID > 0 {
		sourceFile = sourceFileID
	}
	countsJSON, _ := jsonMarshal(counts)
	err = tx.QueryRow(ctx, `insert into log_shares(public_code,owner_user_id,source_type,source_file_id,title,original_name,status,redaction_version,redaction_applied_version,redaction_counts,expires_at)
		values($1,$2,$3,$4,$5,$6,'ready',$7,$7,$8::jsonb,$9) returning id`, code, owner, sourceType, sourceFile,
		truncateRunes(strings.TrimSpace(title), 200), filepath.Base(originalName), logRedactionVersion, countsJSON, expiresAt).Scan(&shareID)
	if err != nil {
		return nil, err
	}
	for index, entry := range entries {
		if _, err = tx.Exec(ctx, `insert into log_share_entries(log_share_id,entry_index,original_name,safe_display_name,content_type,sanitized_text,byte_size,line_count,checksum,status)
			values($1,$2,$3,$3,$4,$5,$6,$7,$8,'ready')`, shareID, index, entry.Name, entry.ContentType, entry.Text, entry.Size, entry.Lines, entry.Checksum); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"publicCode": code, "url": "/log/s/" + code, "status": "ready", "expiresAt": expiresAt,
		"redactionVersion": logRedactionVersion, "redactionCounts": counts, "entryCount": len(entries)}, nil
}

func (s *Server) publicLogShare(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogShareRead(w, r, "log-share-metadata", 30) {
		return
	}
	if len(r.URL.Query()) != 0 {
		writeAPIError(w, http.StatusBadRequest, "LOG_SHARE_METADATA_QUERY_INVALID", "log share metadata does not accept query parameters", 0, nil)
		return
	}
	share, _, err := s.loadLogShareMetadata(r.Context(), r.PathValue("code"), false, currentClaims(r).Subject)
	if err != nil {
		writeLogShareReadError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	payload, err := json.Marshal(apiResponse{Data: share})
	if err != nil || len(payload)+1 > maxLogShareMetadataResponseBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "LOG_SHARE_METADATA_RESPONSE_BUDGET", "log share metadata exceeds response budget", 0, nil)
		return
	}
	writeJSONBytes(w, http.StatusOK, append(payload, '\n'))
}

var errLogShareGone = errors.New("log share gone")

func writeLogShareReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "日志不存在或已失效")
		return
	}
	if errors.Is(err, errLogShareGone) {
		writeError(w, http.StatusGone, "日志已失效")
		return
	}
	writeAPIError(w, http.StatusServiceUnavailable, "LOG_SHARE_READ_UNAVAILABLE", "无法读取安全脱敏日志，请稍后重试", 1, nil)
}

func (s *Server) downloadLogShare(w http.ResponseWriter, r *http.Request) {
	release, ok := s.acquireLogShareBodyRead(w, r, "log-share-download", 5)
	if !ok {
		return
	}
	defer release()
	share, record, err := s.loadLogShareMetadata(r.Context(), r.PathValue("code"), false, currentClaims(r).Subject)
	if err != nil {
		writeLogShareReadError(w, err)
		return
	}
	if record.SourceType != "file" {
		writeError(w, http.StatusNotFound, "该日志不能下载")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	name := record.OriginalName
	if strings.EqualFold(filepath.Ext(name), ".zip") || len(share.Entries) > 1 {
		rows, queryErr := s.db.Query(r.Context(), `select left(safe_display_name,512),sanitized_text
			from log_share_entries where log_share_id=$1 and status='ready' and sanitized_text is not null order by entry_index`, record.ID)
		if queryErr != nil {
			writeError(w, http.StatusInternalServerError, "读取脱敏日志下载内容失败")
			return
		}
		defer rows.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", contentDispositionAttachment(safeLogDownloadName(name, ".zip")))
		writer := zip.NewWriter(w)
		for rows.Next() {
			var entryName, text string
			if scanErr := rows.Scan(&entryName, &text); scanErr != nil {
				_ = writer.Close()
				return
			}
			part, createErr := writer.Create(entryName)
			if createErr != nil {
				_ = writer.Close()
				return
			}
			_, _ = io.WriteString(part, text)
		}
		_ = writer.Close()
		return
	}
	extension := strings.ToLower(filepath.Ext(name))
	if extension != ".log" && extension != ".txt" {
		extension = ".log"
	}
	var text string
	if err = s.db.QueryRow(r.Context(), `select sanitized_text from log_share_entries
		where log_share_id=$1 and entry_index=$2 and status='ready' and sanitized_text is not null`, record.ID, share.Entries[0].Index).Scan(&text); err != nil {
		writeError(w, http.StatusInternalServerError, "读取脱敏日志下载内容失败")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(safeLogDownloadName(name, extension)))
	_, _ = io.WriteString(w, text)
}

func (s *Server) myLogShares(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 30, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	sourceType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sourceType")))
	if sourceType != "" && !stringSet("file", "paste")[sourceType] {
		writeError(w, http.StatusBadRequest, "invalid log source type")
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !stringSet("processing", "ready", "failed", "expired")[status] {
		writeError(w, http.StatusBadRequest, "invalid log status")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	direction := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("direction")))
	if direction != "asc" {
		direction = "desc"
	}
	var total int
	if err := s.db.QueryRow(r.Context(), `select count(*)::int from log_shares
		where owner_user_id=$1 and deleted_at is null and ($2='' or source_type=$2) and ($3='' or status=$3)
		and ($4='' or title ilike '%'||$4||'%' or original_name ilike '%'||$4||'%')`, claims.Subject, sourceType, status, query).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "读取日志历史数量失败")
		return
	}
	order := "created_at desc,id desc"
	if direction == "asc" {
		order = "created_at asc,id asc"
	}
	items, err := s.querySimpleRows(r, `select public_code,source_type,title,original_name,status,redaction_version,created_at,expires_at
		from log_shares where owner_user_id=$1 and deleted_at is null and ($2='' or source_type=$2) and ($3='' or status=$3)
		and ($4='' or title ilike '%'||$4||'%' or original_name ilike '%'||$4||'%')
		order by `+order+` limit $5 offset $6`, claims.Subject, sourceType, status, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取日志历史失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) deleteLogShare(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	tag, err := s.db.Exec(r.Context(), `update log_shares set status='deleted',deleted_at=now()
		where public_code=$1 and owner_user_id=$2 and deleted_at is null`, r.PathValue("code"), claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除日志分享失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "日志分享不存在")
		return
	}
	s.writeAppLog(context.Background(), "user_interaction", "info", "log_share.delete", r.PathValue("code"), claims.Subject, r, http.StatusOK, 0, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func logShareExpiry(now time.Time, days int) (time.Time, error) {
	if days == 0 {
		days = 30
	}
	expires := now.UTC().AddDate(0, 0, days)
	if days < 1 || expires.After(now.UTC().AddDate(3, 0, 0)) {
		return time.Time{}, errors.New("保留时间必须在 1 天到 3 年之间")
	}
	return expires, nil
}

func randomLogShareCode() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func safeLogDownloadName(name, extension string) string {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	base = regexp.MustCompile(`[^\pL\pN._-]+`).ReplaceAllString(base, "_")
	if base == "" {
		base = "sanitized-log"
	}
	return base + "-sanitized" + extension
}

func contentDispositionAttachment(name string) string {
	value := mime.FormatMediaType("attachment", map[string]string{"filename": name})
	if value == "" {
		return "attachment"
	}
	return value
}

func jsonMarshal(value any) (string, error) {
	raw, err := json.Marshal(value)
	return string(raw), err
}
