package httpapi

import "testing"

func TestNormalizeTimezone(t *testing.T) {
	for _, timezone := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Europe/Paris"} {
		if normalized, err := normalizeTimezone(timezone); err != nil || normalized != timezone {
			t.Fatalf("normalizeTimezone(%q) = %q, %v", timezone, normalized, err)
		}
	}
	for _, timezone := range []string{"", "Local", "Asia/Not_A_Real_City", "../UTC"} {
		if _, err := normalizeTimezone(timezone); err == nil {
			t.Fatalf("normalizeTimezone(%q) must fail", timezone)
		}
	}
}
