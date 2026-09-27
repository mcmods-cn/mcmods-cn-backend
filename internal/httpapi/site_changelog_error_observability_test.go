package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSiteChangelogReadsDoNotReturnUncheckedOrPartialData(t *testing.T) {
	handlers, err := os.ReadFile("site_affairs_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	pagination, err := os.ReadFile("site_changelog_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	handlerSource := string(handlers)
	pageSource := string(pagination)
	if strings.Contains(handlerSource, `"translations": json.RawMessage(translations)`) {
		t.Fatal("admin changelog detail still returns unchecked aggregate JSON")
	}
	for _, required := range []string{
		"collectPublicSiteChangelogRows(rows, request.Limit+1)",
		"collectAdminSiteChangelogRows(rows, request.Limit+1)",
		"loadAdminSiteChangelogDetail(r.Context(), s.db, id)",
		"logSiteChangelogReadFailure",
	} {
		if !strings.Contains(handlerSource+pageSource, required) {
			t.Fatalf("site changelog reads are missing %q", required)
		}
	}
}

func TestSiteChangelogCollectorsRejectScanCursorAndJSONFailures(t *testing.T) {
	databaseFailure := errors.New("ARCH-029 database failure")
	for name, collect := range map[string]func(*arch027Rows) error{
		"public": func(rows *arch027Rows) error {
			_, err := collectPublicSiteChangelogRows(rows, 1)
			return err
		},
		"admin": func(rows *arch027Rows) error {
			_, err := collectAdminSiteChangelogRows(rows, 1)
			return err
		},
	} {
		t.Run(name+" scan", func(t *testing.T) {
			rows := &arch027Rows{remaining: 1, scan: func(...any) error { return databaseFailure }}
			if err := collect(rows); !errors.Is(err, databaseFailure) || !rows.closed {
				t.Fatalf("scan failure = %v, closed=%t", err, rows.closed)
			}
		})
		t.Run(name+" terminal", func(t *testing.T) {
			rows := &arch027Rows{err: databaseFailure}
			if err := collect(rows); !errors.Is(err, databaseFailure) || !rows.closed {
				t.Fatalf("terminal failure = %v, closed=%t", err, rows.closed)
			}
		})
	}

	rows := &arch027Rows{remaining: 1, scan: func(destinations ...any) error {
		*(destinations[0].(*int64)) = 1
		*(destinations[1].(*string)) = "arch029"
		*(destinations[2].(*time.Time)) = time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
		*(destinations[3].(*string)) = "published"
		*(destinations[4].(*[]byte)) = []byte("null")
		*(destinations[5].(*time.Time)) = time.Now().UTC()
		return nil
	}}
	if items, err := collectAdminSiteChangelogRows(rows, 1); err == nil || items != nil || !rows.closed {
		t.Fatalf("corrupt admin aggregate = %#v, %v, closed=%t", items, err, rows.closed)
	}
}

type arch029DetailQueryer struct {
	detail adminSiteChangelogDetailValue
	raw    []byte
	err    error
}

func (queryer arch029DetailQueryer) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return arch029DetailRow(queryer)
}

type arch029DetailRow arch029DetailQueryer

func (row arch029DetailRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	date, err := time.Parse("2006-01-02", row.detail.ChangeDate)
	if err != nil {
		return err
	}
	*(destinations[0].(*time.Time)) = date
	*(destinations[1].(*string)) = row.detail.Status
	*(destinations[2].(*time.Time)) = row.detail.UpdatedAt
	*(destinations[3].(*[]byte)) = append([]byte(nil), row.raw...)
	return nil
}

func TestAdminSiteChangelogDetailValidatesAggregateJSON(t *testing.T) {
	if _, err := loadAdminSiteChangelogDetail(context.Background(), arch029DetailQueryer{err: pgx.ErrNoRows}, "missing"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing detail error = %v", err)
	}
	for _, raw := range []string{"null", "[]", `{"zh-CN":"invalid"}`} {
		if detail, err := loadAdminSiteChangelogDetail(context.Background(), arch029DetailQueryer{
			detail: adminSiteChangelogDetailValue{ChangeDate: "2026-08-23", Status: "published"}, raw: []byte(raw),
		}, "arch029"); err == nil {
			t.Fatalf("corrupt detail %q was accepted as %#v", raw, detail)
		}
	}
	detail, err := loadAdminSiteChangelogDetail(context.Background(), arch029DetailQueryer{
		detail: adminSiteChangelogDetailValue{ChangeDate: "2026-08-23", Status: "published"},
		raw:    []byte(`{"zh-CN":{"title":"标题","bodyMarkdown":"正文","status":"published"}}`),
	}, "arch029")
	if err != nil || detail.Translations["zh-CN"].Title != "标题" {
		t.Fatalf("valid detail = %#v, %v", detail, err)
	}
}
