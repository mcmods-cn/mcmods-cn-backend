package httpapi

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLogPageCursorIsStrictAndBoundToEveryFilter(t *testing.T) {
	values := url.Values{
		"category": {"api_access"}, "q": {"timeout api"}, "level": {"error"}, "status": {"500"},
		"from": {"2026-01-01"}, "to": {"2026-01-31"}, "limit": {"50"},
	}
	request, err := parseLogPageRequest(values)
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeLogPageCursor(logPageCursor{
		Version: logPageCursorVersion, Scope: request.Scope,
		CreatedAt: time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), ID: 77,
	})
	withCursor := cloneURLValues(values)
	withCursor.Set("cursor", cursor)
	parsed, err := parseLogPageRequest(withCursor)
	if err != nil || parsed.Cursor == nil || parsed.Cursor.ID != 77 {
		t.Fatalf("parsed cursor=%+v err=%v", parsed.Cursor, err)
	}
	for key, value := range map[string]string{"category": "system", "q": "other", "level": "warn", "status": "404", "from": "2026-01-02", "to": "2026-02-01", "limit": "51"} {
		changed := cloneURLValues(withCursor)
		changed.Set(key, value)
		if _, err = parseLogPageRequest(changed); err == nil {
			t.Errorf("cursor crossed changed %s", key)
		}
	}
	for _, invalid := range []url.Values{
		{"category": {"unknown"}}, {"category": {"system"}, "page": {"2"}},
		{"category": {"system"}, "offset": {"1"}}, {"category": {"system"}, "limit": {"501"}},
		{"category": {"system"}, "q": {strings.Repeat("x", 201)}},
		{"category": {"system"}, "from": {"not-a-date"}},
		{"category": {"system"}, "cursor": {"not-base64"}},
		{"category": {"system", "api_access"}},
	} {
		if _, err = parseLogPageRequest(invalid); err == nil {
			t.Errorf("accepted invalid values %v", invalid)
		}
	}
}

func cloneURLValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, items := range values {
		clone[key] = append([]string(nil), items...)
	}
	return clone
}

func TestLogPageSQLUsesSearchProjectionAndStableTupleForEverySource(t *testing.T) {
	for _, category := range []string{"api_access", "permission_change", "login_security", "file_upload"} {
		request, err := parseLogPageRequest(url.Values{
			"category": {category}, "q": {"failure timeout"}, "limit": {"25"},
		})
		if err != nil {
			t.Fatal(err)
		}
		request.Cursor = &logPageCursor{CreatedAt: time.Now().UTC(), ID: 100}
		query, args := logPageSQL(request)
		lower := strings.ToLower(query)
		for _, required := range []string{"search_document @@ websearch_to_tsquery", "(l.created_at,l.id)<", "order by l.created_at desc,l.id desc", "limit"} {
			if !strings.Contains(lower, required) {
				t.Errorf("%s query missing %q: %s", category, required, query)
			}
		}
		for _, forbidden := range []string{"payload::text", " like ", "offset", "count("} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s query contains %q: %s", category, forbidden, query)
			}
		}
		if len(args) == 0 || args[len(args)-1] != 26 {
			t.Errorf("%s limit args=%v", category, args)
		}
	}
}

func TestLogCleanupStatementsDeleteOnlyOneStableIDBatch(t *testing.T) {
	statements := logCleanupStatements(defaultLogConfig(), 1000)
	if len(statements) != len(appLogRetentionCategories)+4 {
		t.Fatalf("cleanup statements=%d", len(statements))
	}
	for _, statement := range statements {
		lower := strings.ToLower(statement.SQL)
		for _, required := range []string{"delete from", "select id", "order by created_at,id", "limit"} {
			if !strings.Contains(lower, required) {
				t.Errorf("%s cleanup missing %q: %s", statement.Label, required, statement.SQL)
			}
		}
		if strings.Contains(lower, "delete from "+statement.Table+" where created_at") {
			t.Errorf("%s retained an unbounded direct delete: %s", statement.Label, statement.SQL)
		}
		if got := statement.Args[len(statement.Args)-1]; got != 1000 {
			t.Errorf("%s batch arg=%v", statement.Label, got)
		}
	}
}
