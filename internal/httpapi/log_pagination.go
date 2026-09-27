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
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	logPageCursorVersion = 1
	logPageDefaultLimit  = 100
	logPageMaximumLimit  = 500
	logCleanupBatchSize  = 1000
)

type logPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

type logPageRequest struct {
	Category string
	Query    string
	Level    string
	Status   string
	From     *time.Time
	To       *time.Time
	Limit    int
	Scope    string
	Cursor   *logPageCursor
}

type logPage struct {
	Items      []map[string]any
	HasMore    bool
	NextCursor string
}

type logCleanupStatement struct {
	Label string
	Table string
	SQL   string
	Args  []any
}

func parseLogPageRequest(values url.Values) (logPageRequest, error) {
	allowed := map[string]bool{
		"category": true, "q": true, "level": true, "status": true,
		"from": true, "to": true, "limit": true, "cursor": true,
	}
	for key, items := range values {
		if !allowed[key] {
			return logPageRequest{}, fmt.Errorf("unsupported log query parameter %q", key)
		}
		if len(items) != 1 {
			return logPageRequest{}, fmt.Errorf("log query parameter %q must appear exactly once", key)
		}
	}

	request := logPageRequest{
		Category: strings.TrimSpace(values.Get("category")),
		Query:    strings.TrimSpace(values.Get("q")),
		Level:    strings.ToLower(strings.TrimSpace(values.Get("level"))),
		Status:   strings.ToLower(strings.TrimSpace(values.Get("status"))),
		Limit:    logPageDefaultLimit,
	}
	if request.Category == "" {
		request.Category = "system"
	}
	if !validLogCategory(request.Category) {
		return logPageRequest{}, errors.New("invalid log category")
	}
	if utf8.RuneCountInString(request.Query) > 200 || len(request.Query) > 800 {
		return logPageRequest{}, errors.New("log search query is too long")
	}
	if request.Level != "" && request.Level != "info" && request.Level != "warn" && request.Level != "error" {
		return logPageRequest{}, errors.New("invalid log level")
	}
	if len(request.Status) > 64 {
		return logPageRequest{}, errors.New("invalid log status")
	}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 || limit > logPageMaximumLimit {
			return logPageRequest{}, errors.New("invalid log page limit")
		}
		request.Limit = limit
	}
	if rawFrom := strings.TrimSpace(values.Get("from")); rawFrom != "" {
		from, ok := parseLogTime(rawFrom, false)
		if !ok {
			return logPageRequest{}, errors.New("invalid log start time")
		}
		from = from.UTC()
		request.From = &from
	}
	if rawTo := strings.TrimSpace(values.Get("to")); rawTo != "" {
		to, ok := parseLogTime(rawTo, true)
		if !ok {
			return logPageRequest{}, errors.New("invalid log end time")
		}
		to = to.UTC()
		request.To = &to
	}
	if request.From != nil && request.To != nil && request.From.After(*request.To) {
		return logPageRequest{}, errors.New("log time range is reversed")
	}
	if err := validateLogSourceFilters(request); err != nil {
		return logPageRequest{}, err
	}

	request.Scope = logPageScope(request)
	if rawCursor := strings.TrimSpace(values.Get("cursor")); rawCursor != "" {
		cursor, err := decodeLogPageCursor(rawCursor, request.Scope)
		if err != nil {
			return logPageRequest{}, err
		}
		request.Cursor = &cursor
	}
	return request, nil
}

func validLogCategory(category string) bool {
	for _, candidate := range appLogRetentionCategories {
		if category == candidate {
			return true
		}
	}
	return category == "permission_change" || category == "login_security" || category == "file_upload"
}

func validateLogSourceFilters(request logPageRequest) error {
	isAppLog := request.Category != "permission_change" && request.Category != "login_security" && request.Category != "file_upload"
	if request.Level != "" && !isAppLog {
		return errors.New("level is only supported for application logs")
	}
	if request.Status == "" {
		return nil
	}
	switch request.Category {
	case "permission_change":
		return errors.New("status is not supported for permission logs")
	case "login_security":
		if request.Status != "success" && request.Status != "failed" && request.Status != "fail" {
			return errors.New("invalid login log status")
		}
	case "file_upload":
		// Upload result codes are provider-defined and are always bound as values.
	default:
		status, err := strconv.Atoi(request.Status)
		if err != nil || status < 0 || status > 999 {
			return errors.New("invalid HTTP log status")
		}
	}
	return nil
}

func logPageScope(request logPageRequest) string {
	type scopeDocument struct {
		Category string `json:"category"`
		Query    string `json:"q"`
		Level    string `json:"level"`
		Status   string `json:"status"`
		From     string `json:"from"`
		To       string `json:"to"`
		Limit    int    `json:"limit"`
	}
	document := scopeDocument{
		Category: request.Category, Query: request.Query, Level: request.Level,
		Status: request.Status, Limit: request.Limit,
	}
	if request.From != nil {
		document.From = request.From.UTC().Format(time.RFC3339Nano)
	}
	if request.To != nil {
		document.To = request.To.UTC().Format(time.RFC3339Nano)
	}
	raw, _ := json.Marshal(document)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func encodeLogPageCursor(cursor logPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeLogPageCursor(raw string, scope string) (logPageCursor, error) {
	if len(raw) > 2048 {
		return logPageCursor{}, errors.New("log page cursor is too long")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > 1536 {
		return logPageCursor{}, errors.New("invalid log page cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor logPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return logPageCursor{}, errors.New("invalid log page cursor")
	}
	if err = ensureLogCursorEOF(decoder); err != nil {
		return logPageCursor{}, err
	}
	if cursor.Version != logPageCursorVersion || cursor.Scope != scope || cursor.ID <= 0 || cursor.CreatedAt.IsZero() {
		return logPageCursor{}, errors.New("log page cursor does not match this query")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return cursor, nil
}

func ensureLogCursorEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid log page cursor")
	}
	return nil
}

func logPageSQL(request logPageRequest) (string, []any) {
	args := make([]any, 0, 8)
	where := make([]string, 0, 8)
	selectClause := ""
	fromClause := ""

	switch request.Category {
	case "permission_change":
		selectClause = `select l.id, operator.public_id as operator_id,
		        operator.username as operator_username,
		        target.public_id as target_user_id,
		        target.username as target_username,
		        l.action, l.payload, l.created_at`
		fromClause = `from permission_audit_logs l
		 left join users operator on operator.id = l.operator_id
		 left join users target on target.id = l.target_user_id`
	case "login_security":
		selectClause = `select l.id, u.public_id as user_id, u.username,
		        l.account, l.ip, l.user_agent, l.success, l.reason, l.created_at`
		fromClause = `from user_login_logs l left join users u on u.id = l.user_id`
		if request.Status == "success" {
			where = append(where, "l.success = true")
		} else if request.Status == "failed" || request.Status == "fail" {
			where = append(where, "l.success = false")
		}
	case "file_upload":
		selectClause = `select l.id, file.public_id as file_id, uploader.public_id as uploader_id,
		        uploader.username as uploader_username,
		        l.object_key, l.original_name, l.size_bytes, l.ip, l.user_agent, l.result, l.message, l.created_at`
		fromClause = `from oss_upload_logs l
		 left join users uploader on uploader.id = l.uploader_id
		 left join oss_files file on file.id = l.file_id`
		if request.Status != "" {
			args = append(args, request.Status)
			where = append(where, fmt.Sprintf("l.result = $%d", len(args)))
		}
	default:
		selectClause = `select l.id, l.category, l.level, actor.public_id as actor_id,
		        actor.username as actor_username,
		        l.action, l.target, l.ip, l.user_agent, l.method, l.path, l.status, l.latency_ms, l.payload, l.created_at`
		fromClause = `from app_logs l left join users actor on actor.id = l.actor_id`
		args = append(args, request.Category)
		where = append(where, "l.category = $1")
		if request.Level != "" {
			args = append(args, request.Level)
			where = append(where, fmt.Sprintf("l.level = $%d", len(args)))
		}
		if request.Status != "" {
			status, _ := strconv.Atoi(request.Status)
			args = append(args, status)
			where = append(where, fmt.Sprintf("l.status = $%d", len(args)))
		}
	}

	if request.Query != "" {
		args = append(args, request.Query)
		where = append(where, fmt.Sprintf("l.search_document @@ websearch_to_tsquery('simple',$%d)", len(args)))
	}
	if request.From != nil {
		args = append(args, *request.From)
		where = append(where, fmt.Sprintf("l.created_at >= $%d", len(args)))
	}
	if request.To != nil {
		args = append(args, *request.To)
		where = append(where, fmt.Sprintf("l.created_at <= $%d", len(args)))
	}
	if request.Cursor != nil {
		args = append(args, request.Cursor.CreatedAt, request.Cursor.ID)
		where = append(where, fmt.Sprintf("(l.created_at,l.id)<($%d,$%d)", len(args)-1, len(args)))
	}
	if len(where) == 0 {
		where = append(where, "true")
	}
	args = append(args, request.Limit+1)
	query := selectClause + "\n " + fromClause + "\n where " + strings.Join(where, " and ") +
		"\n order by l.created_at desc,l.id desc\n limit $" + strconv.Itoa(len(args))
	return query, args
}

func (s *Server) loadLogPage(ctx context.Context, request logPageRequest) (logPage, error) {
	query, args := logPageSQL(request)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return logPage{}, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	items := make([]map[string]any, 0, request.Limit+1)
	for rows.Next() {
		values, valuesErr := rows.Values()
		if valuesErr != nil {
			return logPage{}, valuesErr
		}
		item := make(map[string]any, len(values))
		for index, value := range values {
			if raw, ok := value.([]byte); ok {
				var decoded any
				if json.Unmarshal(raw, &decoded) == nil {
					value = decoded
				} else {
					value = string(raw)
				}
			}
			item[string(fields[index].Name)] = value
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return logPage{}, err
	}

	page := logPage{Items: items}
	if len(page.Items) > request.Limit {
		page.HasMore = true
		page.Items = page.Items[:request.Limit]
	}
	if page.HasMore && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		createdAt, createdOK := last["created_at"].(time.Time)
		id, idOK := logRowID(last["id"])
		if !createdOK || !idOK {
			return logPage{}, errors.New("log page row is missing its stable key")
		}
		page.NextCursor = encodeLogPageCursor(logPageCursor{
			Version: logPageCursorVersion, Scope: request.Scope,
			CreatedAt: createdAt.UTC(), ID: id,
		})
	}
	return page, nil
}

func logRowID(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, typed > 0
	case int32:
		return int64(typed), typed > 0
	case int:
		return int64(typed), typed > 0
	default:
		return 0, false
	}
}

func logCleanupStatements(cfg logRetentionConfig, requestedBatchSize int) []logCleanupStatement {
	batchSize := requestedBatchSize
	if batchSize <= 0 || batchSize > logCleanupBatchSize {
		batchSize = logCleanupBatchSize
	}
	daysFor := func(category string) int {
		if days, ok := cfg.CategoryDays[category]; ok && days > 0 {
			return days
		}
		return cfg.DefaultDays
	}
	statements := make([]logCleanupStatement, 0, len(appLogRetentionCategories)+4)
	for _, category := range appLogRetentionCategories {
		statements = append(statements, logCleanupStatement{
			Label: category, Table: "app_logs",
			SQL: `delete from app_logs where id in (
				select id from app_logs
				where category = $1 and created_at < now() - make_interval(days => $2::int)
				order by created_at,id limit $3 for update skip locked
			)`,
			Args: []any{category, daysFor(category), batchSize},
		})
	}
	for _, source := range []struct {
		label string
		table string
	}{
		{label: "permission_change", table: "permission_audit_logs"},
		{label: "login_security", table: "user_login_logs"},
		{label: "file_upload", table: "oss_upload_logs"},
		{label: "file_scan", table: "oss_scan_logs"},
	} {
		statements = append(statements, logCleanupStatement{
			Label: source.label, Table: source.table,
			SQL: `delete from ` + source.table + ` where id in (
				select id from ` + source.table + `
				where created_at < now() - make_interval(days => $1::int)
				order by created_at,id limit $2 for update skip locked
			)`,
			Args: []any{daysFor(source.label), batchSize},
		})
	}
	return statements
}
