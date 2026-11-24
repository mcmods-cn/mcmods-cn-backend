package runtimelog

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func TestStoreCapturesAndBoundsCompleteLogLines(t *testing.T) {
	store := NewStore(2)
	_, _ = store.Write([]byte("2026/08/18 09:23:37 backend listening\n"))
	_, _ = store.Write([]byte("2026/08/18 09:23:38 WARNING: temporary key\n"))
	_, _ = store.Write([]byte("2026/08/18 09:24:20 initialization completed\n"))

	entries := store.Entries()
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	if entries[0].ID != 2 || entries[0].Level != "warn" || entries[1].ID != 3 {
		t.Fatalf("unexpected bounded entries: %#v", entries)
	}
}

func TestStoreCombinesPartialWrites(t *testing.T) {
	store := NewStore(5)
	_, _ = store.Write([]byte("2026/08/18 09:23:37 back"))
	_, _ = store.Write([]byte("end ready\n"))
	entries := store.Entries()
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Line, "backend ready") {
		t.Fatalf("partial line was not combined: %#v", entries)
	}
}

func TestBUG054InstallCapturesStructuredAndLegacyLogsWithAuthoritativeLevels(t *testing.T) {
	oldStore := processStore
	oldDefault := slog.Default()
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	processStore = NewStore(10)
	t.Cleanup(func() {
		slog.SetDefault(oldDefault)
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
		processStore = oldStore
	})

	Install()
	slog.Info("structured runtime event", "error", "none", "attempt", 3, "sample", "level=error")
	slog.Warn("retry scheduled", slog.Group("task", "kind", "outbox", "id", 54))
	log.Print("legacy runtime event")
	log.Print("legacy worker failed")

	const fixtureDetail = "synthetic-private-connection-detail"
	slog.Error("database request failed", slog.Group("request", "error", fmt.Errorf("wrapped: %w",
		&pgconn.PgError{Code: "23514", Message: fixtureDetail})))
	slog.Warn("pool setup failed", "error", pgconn.NewParseConfigError("postgres://"+fixtureDetail, fixtureDetail, nil))
	slog.Warn("ordinary event", "error", errors.New("ordinary failure"))
	entries := processStore.Entries()
	if len(entries) != 7 {
		t.Fatalf("captured entries = %d, want structured, legacy and diagnostic logs: %#v", len(entries), entries)
	}
	if entries[0].Level != "info" || !strings.Contains(entries[0].Line, "error=none") || !strings.Contains(entries[0].Line, "attempt=3") {
		t.Errorf("structured info record lost its authoritative level or attributes: %#v", entries[0])
	}
	if entries[1].Level != "warn" || !strings.Contains(entries[1].Line, "outbox") || !strings.Contains(entries[1].Line, "54") {
		t.Errorf("structured warning group was not serialized: %#v", entries[1])
	}
	if entries[2].Level != "info" || !strings.Contains(entries[2].Line, "legacy runtime event") {
		t.Errorf("legacy standard log no longer shares the runtime pipeline: %#v", entries[2])
	}
	if strings.Contains(entries[4].Line+entries[5].Line, fixtureDetail) || !strings.Contains(entries[4].Line, "23514") ||
		!strings.Contains(entries[5].Line, "ParseConfigError") || !strings.Contains(entries[6].Line, "ordinary failure") {
		t.Error("PostgreSQL diagnostics should retain the stage and kind without driver-supplied details")
	}
	if entries[3].Level != "error" || !strings.Contains(entries[3].Line, "legacy worker failed") {
		t.Errorf("legacy severity inference was lost by the slog bridge: %#v", entries[3])
	}
}

func TestOversizePartialWriteKeepsOneDiagnosticLine(t *testing.T) {
	store := NewStore(4)
	prefix := strings.Repeat("a", maxLineBytes)
	for _, part := range []string{prefix[:500], prefix[500:] + "over", "flow", "\nnext line\npartial", " end\n"} {
		n, err := store.Write([]byte(part))
		if n != len(part) || err != nil {
			t.Fatalf("write = %d, %v", n, err)
		}
	}
	entries := store.Entries()
	if len(entries) != 3 || entries[0].Line != prefix+" …[truncated]" || entries[1].Line != "next line" || entries[2].Line != "partial end" {
		t.Fatalf("unexpected stored lines after truncation (count=%d)", len(entries))
	}
}

func BenchmarkStoreLongCompletedLine(b *testing.B) {
	store := NewStore(1)
	record := append([]byte(strings.Repeat("a", 128<<10)), '\n')
	b.ReportAllocs()
	for b.Loop() {
		_, _ = store.Write(record)
	}
}
