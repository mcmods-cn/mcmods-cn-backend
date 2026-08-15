package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestContributionDateRangeUsesRollingWindowForCurrentYear(t *testing.T) {
	t.Parallel()
	from, to := contributionDateRange(2026, time.Date(2026, time.August, 15, 19, 30, 0, 0, time.FixedZone("test", 8*60*60)))
	if got, want := from.Format("2006-01-02"), "2025-08-16"; got != want {
		t.Fatalf("from=%s want %s", got, want)
	}
	if got, want := to.Format("2006-01-02"), "2026-08-15"; got != want {
		t.Fatalf("to=%s want %s", got, want)
	}
}

func TestContributionDateRangeUsesCalendarYearForHistory(t *testing.T) {
	t.Parallel()
	from, to := contributionDateRange(2024, time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC))
	if got, want := from.Format("2006-01-02"), "2024-01-01"; got != want {
		t.Fatalf("from=%s want %s", got, want)
	}
	if got, want := to.Format("2006-01-02"), "2024-12-31"; got != want {
		t.Fatalf("to=%s want %s", got, want)
	}
}

func TestRequestedContributionYearValidatesRange(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		query string
		year  int
		valid bool
	}{
		{"", 2026, true},
		{"?year=2024", 2024, true},
		{"?year=2027", 2027, false},
		{"?year=invalid", 0, false},
		{"?year=1969", 1969, false},
	} {
		request := httptest.NewRequest("GET", "/api/v1/users/example/contributions"+test.query, nil)
		year, valid := requestedContributionYear(request, 2026)
		if year != test.year || valid != test.valid {
			t.Fatalf("query %q returned (%d,%v), want (%d,%v)", test.query, year, valid, test.year, test.valid)
		}
	}
}
