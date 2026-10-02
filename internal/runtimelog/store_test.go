package runtimelog

import (
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
	slog.Info("structured runtime event", "error", "none", "attempt", 3)
	slog.Warn("retry scheduled", slog.Group("task", "kind", "outbox", "id", 54))
	log.Print("legacy runtime event")
	log.Print("legacy worker failed")

	entries := processStore.Entries()
	if len(entries) != 4 {
		t.Fatalf("captured entries = %d, want structured info/warn plus legacy logs: %#v", len(entries), entries)
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
	if entries[3].Level != "error" || !strings.Contains(entries[3].Line, "legacy worker failed") {
		t.Errorf("legacy severity inference was lost by the slog bridge: %#v", entries[3])
	}
}
