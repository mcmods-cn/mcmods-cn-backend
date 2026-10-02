package httpapi

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFavoriteExportPageCursorIsStrictAndScoped(t *testing.T) {
	request, err := parseFavoriteExportPageRequest(url.Values{"status": {"ready"}, "limit": {"25"}}, 42)
	if err != nil {
		t.Fatal(err)
	}
	anchorTime := time.Date(2026, 8, 22, 12, 34, 56, 789, time.UTC)
	cursor, err := encodeFavoriteExportPageCursor(request, anchorTime, 987)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseFavoriteExportPageRequest(url.Values{"status": {"ready"}, "limit": {"25"}, "cursor": {cursor}}, 42)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.AfterID != 987 || !parsed.AfterCreatedAt.Equal(anchorTime) {
		t.Fatalf("parsed cursor = %d/%s", parsed.AfterID, parsed.AfterCreatedAt)
	}
	for name, values := range map[string]url.Values{
		"owner":     {"status": {"ready"}, "limit": {"25"}, "cursor": {cursor}},
		"status":    {"status": {"failed"}, "limit": {"25"}, "cursor": {cursor}},
		"limit":     {"status": {"ready"}, "limit": {"26"}, "cursor": {cursor}},
		"duplicate": {"status": {"ready", "failed"}},
		"unknown":   {"wat": {"1"}},
		"offset":    {"offset": {"100"}},
		"page":      {"page": {"2"}},
	} {
		ownerID := int64(42)
		if name == "owner" {
			ownerID = 43
		}
		if _, parseErr := parseFavoriteExportPageRequest(values, ownerID); parseErr == nil {
			t.Errorf("%s request unexpectedly succeeded", name)
		}
	}
}

func TestFavoriteExportPageSQLUsesStableFilteredKeyset(t *testing.T) {
	for _, status := range []string{"all", "pending", "processing", "ready", "failed", "expired", "cancelled"} {
		request := favoriteExportPageRequest{OwnerUserID: 42, Status: status, Limit: 30,
			AfterCreatedAt: time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC), AfterID: 900}
		query, arguments := favoriteExportPageSQL(request)
		for _, required := range []string{"task.owner_user_id=$1", "(task.created_at,task.id)<", "order by task.created_at desc,task.id desc", "limit"} {
			if !strings.Contains(strings.ToLower(query), required) {
				t.Errorf("status %s SQL is missing %q:\n%s", status, required, query)
			}
		}
		if strings.Contains(strings.ToLower(query), "offset") || strings.Contains(strings.ToLower(query), "count(") {
			t.Errorf("status %s SQL contains offset/count:\n%s", status, query)
		}
		if status != "all" && !strings.Contains(query, "task.status=$2") {
			t.Errorf("status %s SQL is not status-leading: %s", status, query)
		}
		if len(arguments) < 4 || arguments[len(arguments)-1] != request.Limit+1 {
			t.Errorf("status %s args = %#v", status, arguments)
		}
	}
}
