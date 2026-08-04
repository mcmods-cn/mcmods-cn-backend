package httpapi

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOSSDeletionRetryDelay(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		attempts int
		want     time.Duration
	}{
		{attempts: 0, want: 2 * time.Second},
		{attempts: 1, want: 2 * time.Second},
		{attempts: 5, want: 32 * time.Second},
		{attempts: 10, want: 1024 * time.Second},
		{attempts: 100, want: time.Hour},
	} {
		if got := ossDeletionRetryDelay(test.attempts); got != test.want {
			t.Fatalf("attempts %d: got %s, want %s", test.attempts, got, test.want)
		}
	}
}

func TestTruncateOSSDeletionError(t *testing.T) {
	t.Parallel()
	if got := truncateOSSDeletionError(nil); got != "" {
		t.Fatalf("nil error: got %q", got)
	}
	got := truncateOSSDeletionError(errors.New(strings.Repeat("x", 2500)))
	if len(got) != 2000 {
		t.Fatalf("got %d bytes, want 2000", len(got))
	}
}
