package httpapi

import (
	"testing"
	"time"
)

func TestMinecraftVersionType(t *testing.T) {
	tests := map[string]string{
		"release":   "release",
		"snapshot":  "snapshot",
		"old_alpha": "legacy",
		"old_beta":  "legacy",
	}
	for input, expected := range tests {
		if actual := minecraftVersionType(input); actual != expected {
			t.Fatalf("minecraftVersionType(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestNextMinecraftVersionSync(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	before := time.Date(2026, time.July, 15, 3, 30, 0, 0, location)
	if actual := nextMinecraftVersionSync(before); actual.Hour() != 4 || actual.Day() != 15 {
		t.Fatalf("next sync before 04:00 = %s", actual)
	}
	after := time.Date(2026, time.July, 15, 4, 30, 0, 0, location)
	if actual := nextMinecraftVersionSync(after); actual.Hour() != 4 || actual.Day() != 16 {
		t.Fatalf("next sync after 04:00 = %s", actual)
	}
}

func TestStoredMinecraftVersionOrderIsAuthoritative(t *testing.T) {
	base := minecraftVersionConfig{Versions: []minecraftVersionOption{{Code: "1.20.1", Type: "release"}}}
	stored := minecraftVersionConfig{Versions: []minecraftVersionOption{
		{Code: "26.2", Type: "release"},
		{Code: "26.3-snapshot-3", Type: "snapshot"},
	}}
	merged := mergeMinecraftVersionConfig(base, stored)
	if len(merged.Versions) != 2 || merged.Versions[0].Code != "26.2" {
		t.Fatalf("stored version order was not preserved: %#v", merged.Versions)
	}
}
