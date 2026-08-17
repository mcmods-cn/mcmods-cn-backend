package httpapi

import (
	"testing"
	"time"
)

func TestDesiredProjectMaintenanceStatusUsesCalendarThresholds(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last time.Time
		want string
	}{
		{name: "recent", last: now.AddDate(0, -5, 0), want: ""},
		{name: "exactly six months", last: now.AddDate(0, -6, 0), want: ""},
		{name: "over six months", last: now.AddDate(0, -6, 0).Add(-time.Second), want: automatedMaintenanceLowFrequency},
		{name: "exactly one year", last: now.AddDate(-1, 0, 0), want: automatedMaintenanceLowFrequency},
		{name: "over one year", last: now.AddDate(-1, 0, 0).Add(-time.Second), want: automatedMaintenanceDiscontinued},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := desiredProjectMaintenanceStatus(test.last, now); got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLatestProviderActivityIgnoresOrderingAndZeroTimes(t *testing.T) {
	old := time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC)
	latest := time.Date(2026, time.July, 3, 0, 0, 0, 0, time.UTC)
	files := []providerProjectFile{{PublishedAt: old}, {}, {PublishedAt: latest}}
	if got := latestProviderFileActivity(files); !got.Equal(latest) {
		t.Fatalf("latest file activity = %s, want %s", got, latest)
	}
	releases := []projectAutomationRelease{{PublishedAt: latest}, {PublishedAt: old}}
	if got := latestProviderReleaseActivity(releases); !got.Equal(latest) {
		t.Fatalf("latest release activity = %s, want %s", got, latest)
	}
}
