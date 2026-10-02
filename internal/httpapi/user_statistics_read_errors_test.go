package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type userStatisticsDateQueryStub struct {
	rows pgx.Rows
	err  error
}

func (query userStatisticsDateQueryStub) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return query.rows, query.err
}

type userStatisticsDateRowsStub struct {
	dates       []time.Time
	position    int
	terminalErr error
	closed      bool
}

func (rows *userStatisticsDateRowsStub) Close()                                       { rows.closed = true }
func (rows *userStatisticsDateRowsStub) Err() error                                   { return rows.terminalErr }
func (rows *userStatisticsDateRowsStub) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *userStatisticsDateRowsStub) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *userStatisticsDateRowsStub) Values() ([]any, error)                       { return nil, errors.New("unused") }
func (rows *userStatisticsDateRowsStub) RawValues() [][]byte                          { return nil }
func (rows *userStatisticsDateRowsStub) Conn() *pgx.Conn                              { return nil }
func (rows *userStatisticsDateRowsStub) Next() bool {
	if rows.position >= len(rows.dates) {
		rows.closed = true
		return false
	}
	rows.position++
	return true
}
func (rows *userStatisticsDateRowsStub) Scan(destinations ...any) error {
	*destinations[0].(*time.Time) = rows.dates[rows.position-1]
	return nil
}

type userStatisticsTotalsQueryStub struct{ row pgx.Row }

func (query userStatisticsTotalsQueryStub) QueryRow(context.Context, string, ...any) pgx.Row {
	return query.row
}

type userStatisticsTotalsRowStub struct {
	lastEditAt, lastCommentAt *time.Time
	err                       error
}

func (row userStatisticsTotalsRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*destinations[0].(**time.Time) = row.lastEditAt
	*destinations[1].(**time.Time) = row.lastCommentAt
	return nil
}

func TestUserStatisticsReadsDoNotSilentlyReturnPartialFacts(t *testing.T) {
	raw, err := os.ReadFile("user_statistics_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	streakBody := goFunctionBody(t, source, "loadUserActiveStreaks")
	streakQuery := strings.Index(streakBody, "select stat_date from user_statistics_daily")
	streakResult := strings.Index(streakBody, "activeStreaks(dates")
	streakRowsErr := strings.Index(streakBody, "rows.Err()")
	if streakQuery < 0 || streakResult < 0 || streakRowsErr < streakQuery || streakRowsErr > streakResult {
		t.Fatal("active streak calculation must reject a terminal row iterator error before using partial dates")
	}

	activityBody := goFunctionBody(t, source, "queryUserActivityStatistics")
	latestBody := goFunctionBody(t, source, "loadUserLatestActivityTimes")
	if strings.Contains(activityBody, "_ = s.db.QueryRow") {
		t.Fatal("latest activity timestamps still discard every totals query error")
	}
	for _, required := range []string{"errors.Is(err, pgx.ErrNoRows)", "return result, err"} {
		if !strings.Contains(activityBody+latestBody, required) {
			t.Fatalf("latest activity timestamp read is missing %q", required)
		}
	}
}

func TestUserStatisticsReadFailuresAreObservable(t *testing.T) {
	wantErr := errors.New("statistics stream interrupted")
	day := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	rows := &userStatisticsDateRowsStub{dates: []time.Time{day}, terminalErr: wantErr}
	if current, longest, err := loadUserActiveStreaks(context.Background(), userStatisticsDateQueryStub{rows: rows}, 7, day); !errors.Is(err, wantErr) || current != 0 || longest != 0 || !rows.closed {
		t.Fatalf("interrupted streak read=(%d,%d,%v,closed=%t), want zero/zero/%v/true", current, longest, err, rows.closed, wantErr)
	}

	if _, _, err := loadUserLatestActivityTimes(context.Background(), userStatisticsTotalsQueryStub{
		row: userStatisticsTotalsRowStub{err: wantErr},
	}, 7); !errors.Is(err, wantErr) {
		t.Fatalf("totals database failure=%v, want %v", err, wantErr)
	}
	lastEdit := day.Add(-time.Hour)
	lastComment := day.Add(-2 * time.Hour)
	gotEdit, gotComment, err := loadUserLatestActivityTimes(context.Background(), userStatisticsTotalsQueryStub{
		row: userStatisticsTotalsRowStub{lastEditAt: &lastEdit, lastCommentAt: &lastComment},
	}, 7)
	if err != nil || gotEdit == nil || !gotEdit.Equal(lastEdit) || gotComment == nil || !gotComment.Equal(lastComment) {
		t.Fatalf("totals success=(%v,%v,%v)", gotEdit, gotComment, err)
	}
	gotEdit, gotComment, err = loadUserLatestActivityTimes(context.Background(), userStatisticsTotalsQueryStub{
		row: userStatisticsTotalsRowStub{err: pgx.ErrNoRows},
	}, 7)
	if err != nil || gotEdit != nil || gotComment != nil {
		t.Fatalf("missing totals=(%v,%v,%v), want explicit empty success", gotEdit, gotComment, err)
	}
}
