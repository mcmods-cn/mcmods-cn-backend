package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type userOSSQuotaQueryStub struct {
	rows  []userOSSQuotaRowStub
	calls int
}

func (query *userOSSQuotaQueryStub) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	index := query.calls
	query.calls++
	if index >= len(query.rows) {
		return userOSSQuotaRowStub{err: errors.New("unexpected quota query")}
	}
	return query.rows[index]
}

type userOSSQuotaRowStub struct {
	sourceUsed int64
	storedUsed int64
	err        error
}

func (row userOSSQuotaRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*destinations[0].(*int64) = row.sourceUsed
	*destinations[1].(*int64) = row.storedUsed
	return nil
}

func TestLoadUserOSSFileQuotaUsageFailsClosed(t *testing.T) {
	wantErr := errors.New("quota database unavailable")

	dailyFailure := &userOSSQuotaQueryStub{rows: []userOSSQuotaRowStub{{err: wantErr}}}
	if _, err := loadUserOSSFileQuotaUsage(context.Background(), dailyFailure, 7); !errors.Is(err, wantErr) || dailyFailure.calls != 1 {
		t.Fatalf("daily failure = (err=%v, calls=%d), want (%v, 1)", err, dailyFailure.calls, wantErr)
	}

	totalFailure := &userOSSQuotaQueryStub{rows: []userOSSQuotaRowStub{{sourceUsed: 10, storedUsed: 6}, {err: wantErr}}}
	if _, err := loadUserOSSFileQuotaUsage(context.Background(), totalFailure, 7); !errors.Is(err, wantErr) || totalFailure.calls != 2 {
		t.Fatalf("total failure = (err=%v, calls=%d), want (%v, 2)", err, totalFailure.calls, wantErr)
	}

	success := &userOSSQuotaQueryStub{rows: []userOSSQuotaRowStub{
		{sourceUsed: 10, storedUsed: 6},
		{sourceUsed: 20, storedUsed: 12},
	}}
	usage, err := loadUserOSSFileQuotaUsage(context.Background(), success, 7)
	if err != nil || usage.DailySourceUsed != 10 || usage.DailyStoredUsed != 6 || usage.TotalSourceUsed != 20 || usage.TotalStoredUsed != 12 {
		t.Fatalf("success = (%+v, %v), want daily 10/6 and total 20/12", usage, err)
	}
}

func TestUserOSSFilesChecksRowsErrBeforeSuccess(t *testing.T) {
	source, err := os.ReadFile("user_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func (s *Server) userOSSFiles")
	end := strings.Index(text, "func (s *Server) userOSSFileQuota")
	if start < 0 || end <= start {
		t.Fatal("user OSS file handler boundaries not found")
	}
	handler := text[start:end]
	rowsErr := strings.Index(handler, "rows.Err()")
	success := strings.LastIndex(handler, "writeJSON(w, http.StatusOK")
	if rowsErr < 0 || success < 0 || rowsErr > success {
		t.Fatal("userOSSFiles must check rows.Err() before writing the successful file list")
	}
}
