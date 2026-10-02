package runtimelog

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultCapacity = 5000
	maxLineBytes    = 32 << 10
)

// Entry is one real line emitted through Go's process-wide standard logger.
// Line retains the console representation so the administration UI shows the
// same message operators see in the backend terminal.
type Entry struct {
	ID        uint64    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Level     string    `json:"level"`
	Line      string    `json:"line"`
}

// Store is a bounded, process-local ring buffer. Runtime logs are operational
// state rather than business facts, so losing the previous process's buffer on
// restart is intentional. Persistent system events continue to use app_logs.
type Store struct {
	mu       sync.RWMutex
	entries  []Entry
	capacity int
	start    int
	size     int
	nextID   uint64
	partial  []byte
}

var processStore = NewStore(defaultCapacity)

func NewStore(capacity int) *Store {
	if capacity < 1 {
		capacity = 1
	}
	return &Store{capacity: capacity, entries: make([]Entry, capacity)}
}

// Install makes slog the single process logging pipeline. SetDefault also
// bridges the standard log package through this handler, so structured and
// legacy records reach stderr and the bounded administration view together.
// It must run before the first application log line is written.
func Install() {
	output := io.MultiWriter(os.Stderr, processStore)
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	log.SetFlags(0)
	log.SetPrefix("")
	log.SetOutput(legacyLogBridge{logger: logger})
}

type legacyLogBridge struct {
	logger *slog.Logger
}

func (bridge legacyLogBridge) Write(payload []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(payload), "\r\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		bridge.logger.Log(context.Background(), legacySlogLevel(line), line)
	}
	return len(payload), nil
}

func Entries() []Entry {
	return processStore.Entries()
}

func (s *Store) Entries() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Entry, s.size)
	for index := range result {
		result[index] = s.entries[(s.start+index)%s.capacity]
	}
	return result
}

func (s *Store) Write(payload []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	originalLength := len(payload)
	s.partial = append(s.partial, payload...)
	for {
		newline := bytes.IndexByte(s.partial, '\n')
		if newline < 0 {
			break
		}
		s.appendLineLocked(string(s.partial[:newline]))
		s.partial = s.partial[newline+1:]
	}
	// The standard logger normally writes complete lines. Bound a malformed or
	// third-party partial write so it cannot grow process memory without limit.
	if len(s.partial) > maxLineBytes {
		s.appendLineLocked(string(s.partial[:maxLineBytes]))
		s.partial = s.partial[:0]
	}
	return originalLength, nil
}

func (s *Store) appendLineLocked(line string) {
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return
	}
	if len(line) > maxLineBytes {
		line = line[:maxLineBytes] + " …[truncated]"
	}
	createdAt := time.Now()
	if strings.HasPrefix(line, "time=") {
		rawTime := strings.TrimPrefix(strings.SplitN(line, " ", 2)[0], "time=")
		if parsed, err := time.Parse(time.RFC3339Nano, rawTime); err == nil {
			createdAt = parsed
		}
	} else if len(line) >= 19 {
		if parsed, err := time.ParseInLocation("2006/01/02 15:04:05", line[:19], time.Local); err == nil {
			createdAt = parsed
		}
	}
	s.nextID++
	entry := Entry{ID: s.nextID, CreatedAt: createdAt, Level: inferLevel(line), Line: line}
	if s.size < s.capacity {
		s.entries[(s.start+s.size)%s.capacity] = entry
		s.size++
		return
	}
	s.entries[s.start] = entry
	s.start = (s.start + 1) % s.capacity
}

func inferLevel(line string) string {
	value := strings.ToLower(line)
	for _, marker := range []string{"level=error", "level=warn", "level=info", "level=debug"} {
		if !strings.Contains(value, marker) {
			continue
		}
		switch marker {
		case "level=error":
			return "error"
		case "level=warn":
			return "warn"
		default:
			return "info"
		}
	}
	return inferLegacyLevel(value)
}

func inferLegacyLevel(value string) string {
	value = strings.ToLower(value)
	for _, marker := range []string{"panic", "fatal", "error", "failed", "failure"} {
		if strings.Contains(value, marker) {
			return "error"
		}
	}
	for _, marker := range []string{"warning", "warn", "unavailable", "degraded", "retry"} {
		if strings.Contains(value, marker) {
			return "warn"
		}
	}
	return "info"
}

func legacySlogLevel(line string) slog.Level {
	switch inferLegacyLevel(line) {
	case "error":
		return slog.LevelError
	case "warn":
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
