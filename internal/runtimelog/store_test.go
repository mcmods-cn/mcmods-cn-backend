package runtimelog

import (
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
