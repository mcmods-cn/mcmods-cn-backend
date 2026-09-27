package httpapi

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDeadLetterPageCursorIsStrictAndFilterScoped(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/admin/infrastructure/dead-letters?status=all&aggregateType=mod&aggregateId=perf038&limit=25", nil)
	page, err := parseDeadLetterPageRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if page.Status != "all" || page.AggregateType != "mod" || page.AggregateID != "perf038" || page.Limit != 25 {
		t.Fatalf("unexpected dead-letter page request: %#v", page)
	}
	cursor := encodeDeadLetterPageCursor(page, time.Date(2025, 2, 3, 4, 5, 6, 123456000, time.UTC), 42)
	continued := httptest.NewRequest("GET", request.URL.String()+"&cursor="+cursor, nil)
	decoded, err := parseDeadLetterPageRequest(continued)
	if err != nil || decoded.AfterID != 42 || !decoded.AfterFailedAt.Equal(time.Date(2025, 2, 3, 4, 5, 6, 123456000, time.UTC)) {
		t.Fatalf("cursor round trip = %#v, %v", decoded, err)
	}

	for _, path := range []string{
		"/api/v1/admin/infrastructure/dead-letters?page=2",
		"/api/v1/admin/infrastructure/dead-letters?offset=100",
		"/api/v1/admin/infrastructure/dead-letters?limit=101",
		"/api/v1/admin/infrastructure/dead-letters?limit=25&limit=50",
		"/api/v1/admin/infrastructure/dead-letters?status=pending",
		"/api/v1/admin/infrastructure/dead-letters?aggregateId=missing-type",
		"/api/v1/admin/infrastructure/dead-letters?unknown=value",
		"/api/v1/admin/infrastructure/dead-letters?status=replayed&aggregateType=mod&aggregateId=perf038&limit=25&cursor=" + cursor,
	} {
		if _, parseErr := parseDeadLetterPageRequest(httptest.NewRequest("GET", path, nil)); parseErr == nil {
			t.Errorf("accepted invalid dead-letter page request %s", path)
		}
	}
}

func TestDeadLetterPageSQLUsesStableFilteredKeyset(t *testing.T) {
	source, err := os.ReadFile("infrastructure_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(source))
	for _, required := range []string{
		"(failed_at,id)<", "order by failed_at desc,id desc", "page.limit+1",
		"aggregate_type", "aggregate_id", "replayed_at is null", "replayed_at is not null",
		`"hasmore"`, `"nextcursor"`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("dead-letter keyset handler is missing %q", required)
		}
	}
	for _, forbidden := range []string{"order by failed_at desc,id desc limit $1`, limit", `map[string]any{"items": items}`} {
		if strings.Contains(text, forbidden) {
			t.Errorf("dead-letter handler retained fixed-window response %q", forbidden)
		}
	}
}
