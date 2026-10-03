package httpapi

import "testing"

func TestOCT02ArchivesRejectPortableTraversal(t *testing.T) {
	for _, name := range []string{`..\..\latest.log`, `logs\..\latest.log`, `C:\latest.log`, `C:latest.log`, `\\server\share\latest.log`, "..", ".", "logs/../latest.log", "logs/latest.log:secret", "/latest.log"} {
		t.Run(name, func(t *testing.T) {
			raw := makeTestLogZip(t, map[string]string{name: "synthetic log"})
			if _, _, err := sanitizeLogZip(raw); err == nil {
				t.Fatal("log archive accepted unsafe portable path")
			}
			if err := validateReportEvidenceZIP(raw); err == nil {
				t.Fatal("evidence archive accepted unsafe portable path")
			}
		})
	}
	for _, name := range []string{"logs/latest.log", `logs\latest.log`} {
		raw := makeTestLogZip(t, map[string]string{name: "java.lang.Error"})
		entries, _, err := sanitizeLogZip(raw)
		if err != nil || len(entries) != 1 || entries[0].Name != "logs/latest.log" {
			t.Fatalf("safe archive %q: %#v %v", name, entries, err)
		}
		if err = validateReportEvidenceZIP(raw); err != nil {
			t.Fatalf("safe evidence %q: %v", name, err)
		}
	}
	duplicate := makeTestLogZip(t, map[string]string{`logs\latest.log`: "one", "logs/latest.log": "two"})
	if _, _, err := sanitizeLogZip(duplicate); err == nil {
		t.Fatal("separator-normalized duplicate log names accepted")
	}
	if err := validateReportEvidenceZIP(duplicate); err == nil {
		t.Fatal("separator-normalized duplicate evidence names accepted")
	}
}
