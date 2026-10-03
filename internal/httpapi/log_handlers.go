package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/runtimelog"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

type logRetentionConfig struct {
	Enabled      bool           `json:"enabled"`
	DefaultDays  int            `json:"defaultDays"`
	CategoryDays map[string]int `json:"categoryDays"`
}

type logRetentionConfigQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

var appLogRetentionCategories = []string{"system", "user_interaction", "admin_operation", "api_access", "ai_call", "download"}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status == 0 && (status >= 200 || status == http.StatusSwitchingProtocols) {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	size, err := r.ResponseWriter.Write(body)
	r.bytes += size
	return size, err
}

func (r *responseRecorder) Flush() {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	_ = http.NewResponseController(r.ResponseWriter).Flush()
}

func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(r.ResponseWriter).Hijack()
}

func (r *responseRecorder) Push(target string, options *http.PushOptions) error {
	writer := r.ResponseWriter
	for writer != nil {
		if pusher, ok := writer.(http.Pusher); ok {
			return pusher.Push(target, options)
		}
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		next := unwrapper.Unwrap()
		if next == writer {
			break
		}
		writer = next
	}
	return http.ErrNotSupported
}

func (r *responseRecorder) ReadFrom(source io.Reader) (int64, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if readerFrom, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		size, err := readerFrom.ReadFrom(source)
		r.bytes += int(size)
		return size, err
	}
	return io.Copy(struct{ io.Writer }{r}, source)
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (s *Server) logAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		annotation := &requestActivity{}
		r = r.WithContext(context.WithValue(r.Context(), requestActivityContextKey, annotation))
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		// Runtime-log polling is intentionally absent from API access logs. The
		// endpoint reads an in-memory ring buffer; recording every poll would turn
		// observability into a continuous PostgreSQL write source.
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/runtime-logs" {
			return
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		if s.accessLogs != nil {
			s.accessLogs.Capture(r.Method, r.URL.Path, status, s.accessLogRecord(r, status, time.Since(started), recorder.bytes))
		}
		if status >= http.StatusOK && status < http.StatusBadRequest {
			s.recordRequestActivity(r, annotation)
		}
	})
}

func (s *Server) accessLogRecord(r *http.Request, status int, latency time.Duration, responseBytes int) accessLogRecord {
	payload := struct {
		QueryPresent bool `json:"queryPresent"`
		Bytes        int  `json:"bytes"`
	}{QueryPresent: r.URL.RawQuery != "", Bytes: responseBytes}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{}`)
	}
	pathValue := boundedAccessLogText(r.URL.Path, 2048)
	return accessLogRecord{
		Category: "api_access", Level: levelForStatus(status), Action: boundedAccessLogText(r.Method, 16),
		Target: pathValue, IP: boundedAccessLogText(s.requestClientLocation(r).IP, 64),
		UserAgent: boundedAccessLogText(r.UserAgent(), 512), Method: boundedAccessLogText(r.Method, 16),
		Path: pathValue, Status: status, LatencyMS: latency.Milliseconds(), Payload: raw, CreatedAt: time.Now().UTC(),
	}
}

func boundedAccessLogText(value string, maximumBytes int) string {
	if maximumBytes <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maximumBytes {
		return value
	}
	end := maximumBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

type runtimeLogResponse struct {
	Items       []runtimelog.Entry `json:"items"`
	LastID      uint64             `json:"lastId"`
	OldestID    uint64             `json:"oldestId"`
	ResetNeeded bool               `json:"resetNeeded"`
}

func (s *Server) adminRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	level := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	if level != "" && level != "info" && level != "warn" && level != "error" {
		writeError(w, http.StatusBadRequest, "invalid runtime log level")
		return
	}
	afterID, _ := strconv.ParseUint(strings.TrimSpace(r.URL.Query().Get("afterId")), 10, 64)
	limit := boundedLimit(r.URL.Query().Get("limit"), 300, 1000)
	from, hasFrom := parseLogTime(r.URL.Query().Get("from"), false)
	to, hasTo := parseLogTime(r.URL.Query().Get("to"), true)

	all := runtimelog.Entries()
	response := runtimeLogResponse{Items: make([]runtimelog.Entry, 0, min(limit, len(all)))}
	if len(all) > 0 {
		response.OldestID = all[0].ID
		response.LastID = all[len(all)-1].ID
		response.ResetNeeded = afterID > 0 && afterID < response.OldestID-1
	}
	for _, item := range all {
		if afterID > 0 && item.ID <= afterID {
			continue
		}
		if level != "" && item.Level != level {
			continue
		}
		if hasFrom && item.CreatedAt.Before(from) || hasTo && item.CreatedAt.After(to) {
			continue
		}
		item.Line, _ = redactLogText(item.Line)
		if query != "" && !strings.Contains(strings.ToLower(item.Line), query) {
			continue
		}
		response.Items = append(response.Items, item)
	}
	if len(response.Items) > limit {
		response.Items = response.Items[len(response.Items)-limit:]
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	request, err := parseLogPageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.loadLogPage(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取日志失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": page.Items, "limit": request.Limit,
		"hasMore": page.HasMore, "nextCursor": page.NextCursor,
	})
}

func (s *Server) getLogConfig(w http.ResponseWriter, r *http.Request) {
	payload, err := loadLogRetentionConfig(r.Context(), s.db)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "LOG_RETENTION_CONFIG_READ_FAILED", "读取日志清理配置失败", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) updateLogConfig(w http.ResponseWriter, r *http.Request) {
	var payload logRetentionConfig
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	payload = normalizeLogConfig(payload)
	raw, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "日志清理配置格式不正确")
		return
	}
	_, err = s.db.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ('logs.retention', $1::jsonb, $2, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		string(raw),
		currentClaims(r).Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存日志清理配置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": payload})
}

func (s *Server) runLogCleanup(w http.ResponseWriter, r *http.Request) {
	payload, err := loadLogRetentionConfig(r.Context(), s.db)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "LOG_RETENTION_CONFIG_READ_FAILED", "读取日志清理配置失败", 0, nil)
		return
	}
	deleted, failedCategories, cleanupErr := s.cleanupLogs(r.Context(), payload)
	if cleanupErr != nil {
		log.Printf("manual log cleanup failed: %v", cleanupErr)
		writeAPIError(w, http.StatusInternalServerError, "LOG_CLEANUP_FAILED", "部分日志清理失败", 0, map[string]any{
			"config": payload, "deleted": deleted, "failedCategories": failedCategories,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": payload, "deleted": deleted})
}

func (s *Server) writeAppLog(ctx context.Context, category string, level string, action string, target string, actorID int64, r *http.Request, status int, latency time.Duration, payload any) {
	if s.db == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	var actor any
	if actorID > 0 {
		actor = actorID
	}
	method, pathValue, ip, userAgent := "", "", "", ""
	if r != nil {
		method = r.Method
		pathValue = r.URL.Path
		ip = s.requestClientLocation(r).IP
		userAgent = r.UserAgent()
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, _ = s.db.Exec(
		timeoutCtx,
		`insert into app_logs (category, level, actor_id, action, target, ip, user_agent, method, path, status, latency_ms, payload)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)`,
		category,
		level,
		actor,
		action,
		target,
		ip,
		userAgent,
		method,
		pathValue,
		status,
		latency.Milliseconds(),
		string(raw),
	)
}

func (s *Server) querySimpleRows(r *http.Request, sql string, args ...any) ([]map[string]any, error) {
	return s.querySimpleRowsWithContext(r.Context(), sql, args...)
}

func (s *Server) querySimpleRowsWithContext(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return collectSimpleRows(rows)
}

func collectSimpleRows(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()
	fields := rows.FieldDescriptions()
	items := make([]map[string]any, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		item := make(map[string]any, len(values))
		for index, value := range values {
			key := string(fields[index].Name)
			if raw, ok := value.([]byte); ok {
				var decoded any
				if err := json.Unmarshal(raw, &decoded); err == nil {
					value = decoded
				} else {
					value = string(raw)
				}
			}
			item[key] = value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func parseLogTime(value string, endOfDay bool) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		if endOfDay {
			parsed = parsed.Add(24*time.Hour - time.Nanosecond)
		}
		return parsed, true
	}
	return time.Time{}, false
}

func loadLogRetentionConfig(ctx context.Context, queryer logRetentionConfigQueryer) (logRetentionConfig, error) {
	payload := defaultLogConfig()
	var raw []byte
	err := queryer.QueryRow(ctx, `select value from system_settings where key = 'logs.retention'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return payload, nil
	}
	if err != nil {
		return logRetentionConfig{}, fmt.Errorf("read logs.retention: %w", err)
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return logRetentionConfig{}, fmt.Errorf("decode logs.retention: %w", err)
	}
	return normalizeLogConfig(payload), nil
}

func defaultLogConfig() logRetentionConfig {
	return logRetentionConfig{
		Enabled:     true,
		DefaultDays: 180,
		CategoryDays: map[string]int{
			"api_access":        90,
			"login_security":    365,
			"permission_change": 1095,
			"admin_operation":   365,
			"file_upload":       365,
			"file_scan":         365,
			"ai_call":           180,
			"system":            180,
			"user_interaction":  180,
			"download":          90,
		},
	}
}

func normalizeLogConfig(payload logRetentionConfig) logRetentionConfig {
	defaults := defaultLogConfig()
	if payload.DefaultDays <= 0 {
		payload.DefaultDays = defaults.DefaultDays
	}
	if payload.DefaultDays > 3650 {
		payload.DefaultDays = 3650
	}
	normalized := defaults.CategoryDays
	keys := make([]string, 0, len(payload.CategoryDays))
	for category := range payload.CategoryDays {
		keys = append(keys, category)
	}
	slices.Sort(keys)
	seen := make(map[string]bool, len(keys))
	for _, rawCategory := range keys {
		category := strings.TrimSpace(rawCategory)
		if category == "" || seen[category] {
			continue
		}
		// Explicit canonical keys take precedence over whitespace aliases. If
		// only aliases exist, sorted input gives a stable choice on every load.
		days, canonical := payload.CategoryDays[category]
		if !canonical {
			days = payload.CategoryDays[rawCategory]
		}
		seen[category] = true
		if days <= 0 {
			delete(normalized, category)
			continue
		}
		normalized[category] = min(days, 3650)
	}
	payload.CategoryDays = normalized
	return payload
}

func (s *Server) cleanupLogs(ctx context.Context, cfg logRetentionConfig) (map[string]int64, []string, error) {
	deleted := map[string]int64{}
	if !cfg.Enabled {
		return deleted, nil, nil
	}
	failedCategories := make([]string, 0)
	failures := make([]error, 0)
	for _, statement := range logCleanupStatements(cfg, logCleanupBatchSize) {
		tag, err := s.db.Exec(ctx, statement.SQL, statement.Args...)
		if err != nil {
			failedCategories = append(failedCategories, statement.Label)
			failures = append(failures, fmt.Errorf("%s: %w", statement.Label, err))
			continue
		}
		deleted[statement.Label] = tag.RowsAffected()
	}
	return deleted, failedCategories, errors.Join(failures...)
}

func boundedLimit(raw string, fallback int, max int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func levelForStatus(status int) string {
	switch {
	case status >= 500:
		return "error"
	case status >= 400:
		return "warn"
	default:
		return "info"
	}
}

func requestIP(r *http.Request) string {
	return normalizeIPAddress(remoteIP(r.RemoteAddr))
}
