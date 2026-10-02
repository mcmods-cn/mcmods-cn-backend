package httpapi

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGovernanceCursorIsStrictAndScopeBound(t *testing.T) {
	t.Parallel()
	own, err := parseOwnReportPageRequest(url.Values{"limit": {"17"}}, 41)
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeGovernancePageCursor(governancePageCursor{
		Version: governancePageCursorVersion, Scope: own.Scope,
		CreatedAt: time.Date(2026, 8, 23, 1, 2, 3, 0, time.UTC), ID: 91,
	})
	if _, err = parseOwnReportPageRequest(url.Values{"limit": {"17"}, "cursor": {cursor}}, 41); err != nil {
		t.Fatalf("valid own-report cursor rejected: %v", err)
	}
	for label, values := range map[string]url.Values{
		"offset":      {"offset": {"1"}},
		"page":        {"page": {"2"}},
		"unknown":     {"extra": {"1"}},
		"duplicate":   {"limit": {"17", "18"}},
		"zero limit":  {"limit": {"0"}},
		"large limit": {"limit": {"101"}},
	} {
		if _, parseErr := parseOwnReportPageRequest(values, 41); parseErr == nil {
			t.Fatalf("%s request was accepted", label)
		}
	}
	for label, parse := range map[string]func() error{
		"cross reporter": func() error {
			_, e := parseOwnReportPageRequest(url.Values{"limit": {"17"}, "cursor": {cursor}}, 42)
			return e
		},
		"cross limit": func() error {
			_, e := parseOwnReportPageRequest(url.Values{"limit": {"18"}, "cursor": {cursor}}, 41)
			return e
		},
		"cross admin reports": func() error {
			_, e := parseAdminReportPageRequest(url.Values{"status": {"pending"}, "limit": {"17"}, "cursor": {cursor}})
			return e
		},
		"cross public blackroom": func() error {
			_, e := parsePublicBlackroomPageRequest(url.Values{"limit": {"17"}, "cursor": {cursor}})
			return e
		},
	} {
		if parse() == nil {
			t.Fatalf("%s cursor was accepted", label)
		}
	}
}

func TestAdminReportPaginationValidatesStatusAndBindsCursor(t *testing.T) {
	t.Parallel()
	request, err := parseAdminReportPageRequest(url.Values{"status": {"in_review"}, "limit": {"31"}})
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeGovernancePageCursor(governancePageCursor{
		Version: governancePageCursorVersion, Scope: request.Scope,
		CreatedAt: time.Date(2026, 8, 23, 1, 2, 3, 0, time.UTC), ID: 9,
	})
	if _, err = parseAdminReportPageRequest(url.Values{"status": {"in_review"}, "limit": {"31"}, "cursor": {cursor}}); err != nil {
		t.Fatalf("valid admin-report cursor rejected: %v", err)
	}
	for label, values := range map[string]url.Values{
		"invalid status":       {"status": {"deleted"}},
		"cross status":         {"status": {"pending"}, "limit": {"31"}, "cursor": {cursor}},
		"missing status scope": {"limit": {"31"}, "cursor": {cursor}},
	} {
		if _, parseErr := parseAdminReportPageRequest(values); parseErr == nil {
			t.Fatalf("%s request was accepted", label)
		}
	}
}

func TestGovernancePageQueriesUseBoundedTupleKeysets(t *testing.T) {
	t.Parallel()
	for name, contract := range map[string]struct {
		query  string
		keyset string
		order  string
	}{
		"own reports":      {ownReportPageSQL, "(report.created_at,report.id)<", "order by report.created_at desc,report.id desc"},
		"admin reports":    {adminReportPageSQL, "(report.created_at,report.id)>", "order by report.created_at,report.id"},
		"public blackroom": {blackroomPageSQL, "(ban.created_at,ban.id)<", "order by ban.created_at desc,ban.id desc"},
		"admin blackroom":  {adminBlackroomPageSQL, "(ban.created_at,ban.id)<", "order by ban.created_at desc,ban.id desc"},
	} {
		query := strings.ToLower(contract.query)
		for _, required := range []string{contract.keyset, contract.order, "limit $"} {
			if !strings.Contains(query, required) {
				t.Fatalf("%s SQL is missing %q", name, required)
			}
		}
		for _, forbidden := range []string{" offset ", "count(*) over"} {
			if strings.Contains(query, forbidden) {
				t.Fatalf("%s SQL retains %q", name, forbidden)
			}
		}
	}
}

func TestGovernanceListHandlersRemoveOffsetAndCheckIterationErrors(t *testing.T) {
	t.Parallel()
	handlers, err := os.ReadFile("governance_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	pagination, err := os.ReadFile("governance_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(handlers) + string(pagination))
	for _, forbidden := range []string{"boundedoffset", " offset $", `"offset"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("governance list path retains %q", forbidden)
		}
	}
	for _, required := range []string{"hasmore", "nextcursor", "rows.err()"} {
		if !strings.Contains(text, required) {
			t.Fatalf("governance list path is missing %q", required)
		}
	}
}
