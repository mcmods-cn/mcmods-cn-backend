package httpapi

import (
	"strings"
	"testing"
)

func TestCommentIdempotencyUsesTransactionLock(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(commentIdempotencyLockSQL)
	for _, required := range []string{"pg_advisory_xact_lock", "$1::text"} {
		if !strings.Contains(definition, required) {
			t.Fatalf("comment idempotency lock is missing %q", required)
		}
	}
	if got := commentIdempotencyLockKey(42, "request-key"); got != "comment-idempotency:42:request-key" {
		t.Fatalf("unexpected comment idempotency lock key %q", got)
	}
}
