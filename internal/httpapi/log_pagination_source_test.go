package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestAdminLogHandlerUsesBoundedEnvelopeAndVisibleReadErrors(t *testing.T) {
	source, err := os.ReadFile("log_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{"parseLogPageRequest", "loadLogPage", `"hasMore"`, `"nextCursor"`} {
		if !strings.Contains(text, required) {
			t.Errorf("log handler is missing %q", required)
		}
	}
	if strings.Contains(text, "writeJSON(w, http.StatusOK, s.appLogs") || strings.Contains(text, "payload::text") {
		t.Fatal("log handler retains the unpaged wide-search path")
	}
}
