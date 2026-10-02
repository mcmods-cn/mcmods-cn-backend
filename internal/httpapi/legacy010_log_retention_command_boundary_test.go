package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestLEGACY010LogPolicySaveAndManualCleanupAreSeparateCommands(t *testing.T) {
	handlers, err := os.ReadFile("log_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(handlers)
	saveStart := strings.Index(source, "func (s *Server) updateLogConfig(")
	if saveStart < 0 {
		t.Fatal("could not find the log retention policy save handler")
	}
	nextHandler := strings.Index(source[saveStart:], "\nfunc (s *Server) ")
	if nextHandler <= 0 {
		t.Fatal("could not isolate the log retention policy save handler")
	}
	saveHandler := source[saveStart : saveStart+nextHandler]
	if strings.Contains(saveHandler, "cleanupLogs") {
		t.Fatal("saving the log retention policy still performs synchronous cleanup")
	}
	if !strings.Contains(source, "func (s *Server) runLogCleanup(") {
		t.Fatal("log retention has no explicit manual cleanup command")
	}

	routes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(routes), `s.mux.HandleFunc("POST /api/v1/admin/logs/cleanup", s.requirePermission("log.write", s.runLogCleanup))`) {
		t.Fatal("manual log cleanup is not bound to an exact log.write route")
	}
}
