package httpapi

import (
	"testing"
	"time"
)

func TestParseUserStatisticsRange(t *testing.T) {
	now := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	value, err := parseUserStatisticsRange("7d", now)
	if err != nil || value.StartDate == nil || value.StartDate.Format("2006-01-02") != "2026-08-09" {
		t.Fatalf("unexpected seven-day range: %#v, %v", value, err)
	}
	if _, err = parseUserStatisticsRange("7 days; drop table users", now); err == nil {
		t.Fatal("expected an invalid range to be rejected")
	}
}

func TestActiveStreaks(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	dates := []time.Time{
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
	}
	current, longest := activeStreaks(dates, now)
	if current != 2 || longest != 3 {
		t.Fatalf("unexpected streaks: current=%d longest=%d", current, longest)
	}
}
