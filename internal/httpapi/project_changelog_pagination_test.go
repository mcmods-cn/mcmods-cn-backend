package httpapi

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProjectChangelogCursorIsStrictAndScopeBound(t *testing.T) {
	target := projectChangelogTarget{RouteID: 42, EntityType: "mod", PublicID: "abc123xyz"}
	request, err := parseProjectChangelogPageRequest(url.Values{"locale": {"zh-CN"}, "limit": {"25"}}, target, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	eventAt := time.Date(2026, time.August, 22, 8, 30, 0, 0, time.UTC)
	raw := encodeProjectChangelogPageCursor(projectChangelogPageCursor{
		Version: projectChangelogCursorVersion, Scope: request.Scope, EventAt: eventAt, ID: 99,
	})
	cursor, err := decodeProjectChangelogPageCursor(raw, request.Scope)
	if err != nil || cursor.ID != 99 || !cursor.EventAt.Equal(eventAt) {
		t.Fatalf("decode cursor: cursor=%+v err=%v", cursor, err)
	}
	for name, invalid := range map[string]string{
		"malformed": "not-base64!",
		"unknown field": encodeProjectChangelogPageCursor(projectChangelogPageCursor{
			Version: projectChangelogCursorVersion, Scope: request.Scope, EventAt: eventAt, ID: 0,
		}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, decodeErr := decodeProjectChangelogPageCursor(invalid, request.Scope); decodeErr == nil {
				t.Fatal("invalid cursor was accepted")
			}
		})
	}
	foreign := projectChangelogPageScope(target, "en-US", request.Limit)
	if _, err = decodeProjectChangelogPageCursor(raw, foreign); err == nil {
		t.Fatal("cursor was accepted for another locale")
	}
}

func TestProjectChangelogPageRequestRejectsDeepOffsetsAndInvalidLimits(t *testing.T) {
	target := projectChangelogTarget{RouteID: 42, EntityType: "mod", PublicID: "abc123xyz"}
	for name, values := range map[string]url.Values{
		"page":   {"page": {"2"}},
		"offset": {"offset": {"50"}},
		"zero":   {"limit": {"0"}},
		"large":  {"limit": {"51"}},
		"text":   {"limit": {"many"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseProjectChangelogPageRequest(values, target, "zh-CN"); err == nil {
				t.Fatal("invalid page request was accepted")
			}
		})
	}
}

func TestProjectChangelogPageSQLUsesKeysetAndLimitPlusOne(t *testing.T) {
	request := projectChangelogPageRequest{Limit: 50, Cursor: &projectChangelogPageCursor{
		EventAt: time.Date(2026, time.August, 22, 8, 30, 0, 0, time.UTC), ID: 100,
	}}
	query, args := projectChangelogPageSQL(42, "zh-CN", request)
	if !strings.Contains(query, "(entry.event_at,entry.id)<($6,$7)") || strings.Contains(strings.ToLower(query), " offset ") {
		t.Fatalf("expected keyset query without offset: %s", query)
	}
	if got := args[len(args)-1]; got != 51 {
		t.Fatalf("expected limit+1=51, got %v", got)
	}
	if strings.Contains(query, "jsonb_object_agg(value.locale,value.body_markdown)") {
		t.Fatal("summary page aggregates complete localized bodies")
	}
}
