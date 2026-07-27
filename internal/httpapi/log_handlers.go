package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
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

type logQueryFilter struct {
	Category string
	Query    string
	Level    string
	Status   string
	From     string
	To       string
	Limit    int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
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
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		loggedQuery := r.URL.RawQuery
		if strings.HasPrefix(r.URL.Path, "/api/yggdrasil/") {
			loggedQuery = ""
		}
		s.writeAppLog(context.Background(), "api_access", levelForStatus(status), r.Method, r.URL.Path, 0, r, status, time.Since(started), map[string]any{
			"query": loggedQuery,
			"bytes": recorder.bytes,
		})
		if status >= http.StatusOK && status < http.StatusBadRequest {
			s.recordRequestActivity(r, annotation)
		}
	})
}

func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	filter := logFilterFromRequest(r)
	switch filter.Category {
	case "permission_change":
		writeJSON(w, http.StatusOK, s.permissionChangeLogs(r, filter))
	case "login_security":
		writeJSON(w, http.StatusOK, s.loginSecurityLogs(r, filter))
	case "file_upload":
		writeJSON(w, http.StatusOK, s.fileUploadLogs(r, filter))
	default:
		writeJSON(w, http.StatusOK, s.appLogs(r, filter))
	}
}

func (s *Server) getLogConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.logConfigFromSettings(r.Context()))
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
	deleted := s.cleanupLogs(r.Context(), payload)
	writeJSON(w, http.StatusOK, map[string]any{"config": payload, "deleted": deleted})
}

func (s *Server) appLogs(r *http.Request, filter logQueryFilter) []map[string]any {
	args := []any{filter.Category}
	where := []string{"l.category = $1"}
	addLogCommonFiltersForColumn(&where, &args, filter, "l.created_at", []string{
		"l.action", "l.target", "l.ip", "l.user_agent", "l.method", "l.path", "l.payload::text",
		"actor.username", "actor.display_name", "actor.email", "actor.public_id",
	})
	if filter.Level != "" {
		args = append(args, filter.Level)
		where = append(where, fmt.Sprintf("l.level = $%d", len(args)))
	}
	if filter.Status != "" {
		if status, err := strconv.Atoi(filter.Status); err == nil {
			args = append(args, status)
			where = append(where, fmt.Sprintf("l.status = $%d", len(args)))
		}
	}
	args = append(args, filter.Limit)
	return s.querySimpleRows(
		r,
		`select l.id, l.category, l.level, actor.public_id as actor_id,
		        actor.username as actor_username,
		        actor.display_name as actor_display_name,
		        l.action, l.target, l.ip, l.user_agent, l.method, l.path, l.status, l.latency_ms, l.payload, l.created_at
		 from app_logs l
		 left join users actor on actor.id = l.actor_id
		 where `+strings.Join(where, " and ")+`
		 order by l.created_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
}

func (s *Server) permissionChangeLogs(r *http.Request, filter logQueryFilter) []map[string]any {
	args := []any{}
	where := []string{"1 = 1"}
	addLogCommonFiltersForColumn(&where, &args, filter, "l.created_at", []string{
		"l.action", "l.payload::text", "operator.public_id", "target.public_id",
		"operator.username", "operator.display_name", "operator.email",
		"target.username", "target.display_name", "target.email",
	})
	args = append(args, filter.Limit)
	return s.querySimpleRows(
		r,
		`select l.id, operator.public_id as operator_id,
		        operator.username as operator_username,
		        operator.display_name as operator_display_name,
		        target.public_id as target_user_id,
		        target.username as target_username,
		        target.display_name as target_display_name,
		        l.action, l.payload, l.created_at
		 from permission_audit_logs l
		 left join users operator on operator.id = l.operator_id
		 left join users target on target.id = l.target_user_id
		 where `+strings.Join(where, " and ")+`
		 order by l.created_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
}

func (s *Server) loginSecurityLogs(r *http.Request, filter logQueryFilter) []map[string]any {
	args := []any{}
	where := []string{"1 = 1"}
	addLogCommonFiltersForColumn(&where, &args, filter, "l.created_at", []string{
		"l.account", "l.ip", "l.user_agent", "l.reason", "u.public_id",
		"u.username", "u.display_name", "u.email",
	})
	switch filter.Status {
	case "success":
		where = append(where, "l.success = true")
	case "failed", "fail":
		where = append(where, "l.success = false")
	}
	args = append(args, filter.Limit)
	return s.querySimpleRows(
		r,
		`select l.id, u.public_id as user_id,
		        u.username,
		        u.display_name,
		        l.account, l.ip, l.user_agent, l.success, l.reason, l.created_at
		 from user_login_logs l
		 left join users u on u.id = l.user_id
		 where `+strings.Join(where, " and ")+`
		 order by l.created_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
}

func (s *Server) fileUploadLogs(r *http.Request, filter logQueryFilter) []map[string]any {
	args := []any{}
	where := []string{"1 = 1"}
	addLogCommonFiltersForColumn(&where, &args, filter, "l.created_at", []string{
		"l.object_key", "l.original_name", "l.ip", "l.user_agent", "l.result", "l.message", "uploader.public_id",
		"uploader.username", "uploader.display_name", "uploader.email",
	})
	if filter.Status != "" {
		args = append(args, filter.Status)
		where = append(where, fmt.Sprintf("l.result = $%d", len(args)))
	}
	args = append(args, filter.Limit)
	return s.querySimpleRows(
		r,
		`select l.id, file.public_id as file_id, uploader.public_id as uploader_id,
		        uploader.username as uploader_username,
		        uploader.display_name as uploader_display_name,
		        l.object_key, l.original_name, l.size_bytes, l.ip, l.user_agent, l.result, l.message, l.created_at
		 from oss_upload_logs l
		 left join users uploader on uploader.id = l.uploader_id
		 left join oss_files file on file.id = l.file_id
		 where `+strings.Join(where, " and ")+`
		 order by l.created_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
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

func (s *Server) querySimpleRows(r *http.Request, sql string, args ...any) []map[string]any {
	rows, err := s.db.Query(r.Context(), sql, args...)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	items := make([]map[string]any, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			continue
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
	return items
}

func logFilterFromRequest(r *http.Request) logQueryFilter {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category == "" {
		category = "system"
	}
	return logQueryFilter{
		Category: category,
		Query:    strings.TrimSpace(r.URL.Query().Get("q")),
		Level:    strings.TrimSpace(r.URL.Query().Get("level")),
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
		From:     strings.TrimSpace(r.URL.Query().Get("from")),
		To:       strings.TrimSpace(r.URL.Query().Get("to")),
		Limit:    boundedLimit(r.URL.Query().Get("limit"), 100, 500),
	}
}

func addLogCommonFiltersForColumn(where *[]string, args *[]any, filter logQueryFilter, createdAtColumn string, searchColumns []string) {
	if filter.Query != "" && len(searchColumns) > 0 {
		*args = append(*args, "%"+strings.ToLower(filter.Query)+"%")
		placeholder := fmt.Sprintf("$%d", len(*args))
		parts := make([]string, 0, len(searchColumns))
		for _, column := range searchColumns {
			parts = append(parts, "lower(coalesce("+column+", '')) like "+placeholder)
		}
		*where = append(*where, "("+strings.Join(parts, " or ")+")")
	}
	if from, ok := parseLogTime(filter.From, false); ok {
		*args = append(*args, from)
		*where = append(*where, fmt.Sprintf("%s >= $%d", createdAtColumn, len(*args)))
	}
	if to, ok := parseLogTime(filter.To, true); ok {
		*args = append(*args, to)
		*where = append(*where, fmt.Sprintf("%s <= $%d", createdAtColumn, len(*args)))
	}
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

func (s *Server) logConfigFromSettings(ctx context.Context) logRetentionConfig {
	payload := defaultLogConfig()
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key = 'logs.retention'`).Scan(&raw)
	if err != nil {
		_ = ignoreNoRows(err)
		return payload
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return defaultLogConfig()
	}
	return normalizeLogConfig(payload)
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
			"ai_call":           180,
			"system":            180,
			"user_interaction":  180,
		},
	}
}

func normalizeLogConfig(payload logRetentionConfig) logRetentionConfig {
	if payload.DefaultDays <= 0 {
		payload.DefaultDays = 180
	}
	if payload.DefaultDays > 3650 {
		payload.DefaultDays = 3650
	}
	if payload.CategoryDays == nil {
		payload.CategoryDays = map[string]int{}
	}
	for category, days := range payload.CategoryDays {
		category = strings.TrimSpace(category)
		if category == "" {
			delete(payload.CategoryDays, category)
			continue
		}
		if days <= 0 {
			delete(payload.CategoryDays, category)
			continue
		}
		if days > 3650 {
			payload.CategoryDays[category] = 3650
		}
	}
	return payload
}

func (s *Server) cleanupLogs(ctx context.Context, cfg logRetentionConfig) map[string]int64 {
	deleted := map[string]int64{}
	if !cfg.Enabled {
		return deleted
	}
	run := func(label string, query string, args ...any) {
		tag, err := s.db.Exec(ctx, query, args...)
		if err == nil {
			deleted[label] = tag.RowsAffected()
		}
	}
	daysFor := func(category string) int {
		if days, ok := cfg.CategoryDays[category]; ok && days > 0 {
			return days
		}
		return cfg.DefaultDays
	}
	for _, category := range []string{"system", "user_interaction", "admin_operation", "api_access", "ai_call"} {
		run(category, `delete from app_logs where category = $1 and created_at < now() - make_interval(days => $2::int)`, category, daysFor(category))
	}
	run("permission_change", `delete from permission_audit_logs where created_at < now() - make_interval(days => $1::int)`, daysFor("permission_change"))
	run("login_security", `delete from user_login_logs where created_at < now() - make_interval(days => $1::int)`, daysFor("login_security"))
	run("file_upload", `delete from oss_upload_logs where created_at < now() - make_interval(days => $1::int)`, daysFor("file_upload"))
	run("file_scan", `delete from oss_scan_logs where created_at < now() - make_interval(days => $1::int)`, daysFor("file_scan"))
	return deleted
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
