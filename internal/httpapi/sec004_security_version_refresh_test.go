package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSEC004ProductionHandlersDoNotIgnoreSecurityVersionRefreshFailures(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	ignored := regexp.MustCompile(`_\s*=\s*s\.refresh(?:Auth|Permission|RBAC|ProjectACL)Version\(`)
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		source, readErr := os.ReadFile(filepath.Clean(entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if ignored.Match(source) {
			matches = append(matches, entry.Name())
		}
	}
	if len(matches) != 0 {
		t.Fatalf("security version refresh failures remain ignored in %v", matches)
	}
	for file, required := range map[string][]string{
		"auth_cache.go":                   {"DeleteShared", "SECURITY_VERSION_REFRESH_FAILED", "security version refresh failed after commit", `"committed": true`},
		"infrastructure_metrics.go":       {`"securityVersions"`, `"refreshFailures"`},
		"maintenance_worker.go":           {"DeleteShared", "publish expired ban permission version", "!worker.cache.SetShared"},
		"permission_defaults_handlers.go": {"refreshAuthVersionForIdentity", "refreshPermissionVersion", "requireSecurityVersionRefresh"},
	} {
		source, readErr := os.ReadFile(filepath.Clean(file))
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, token := range required {
			if !strings.Contains(string(source), token) {
				t.Fatalf("%s is missing SEC-004 contract %q", file, token)
			}
		}
	}
}
