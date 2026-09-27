package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

func TestOSSDeletionAdminRoutesArePermissionGuarded(t *testing.T) {
	t.Parallel()
	routes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	definition := string(routes)
	for _, required := range []string{
		`HandleFunc("GET /api/v1/admin/infrastructure/oss-deletions", s.requirePermission("admin.config.read", s.adminOSSDeletionJobs))`,
		`HandleFunc("POST /api/v1/admin/infrastructure/oss-deletions/{id}/replay", s.requirePermission("admin.config.write", s.adminReplayOSSDeletion))`,
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("missing permission-guarded OSS deletion route %q", required)
		}
	}
}

func TestOSSDeletionFailureClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		err       error
		class     string
		retryable bool
	}{
		{name: "configuration", err: errOSSDeletionCredentialsUnavailable, class: "configuration"},
		{name: "shared configuration", err: fmt.Errorf("disabled: %w", errOSSConfigurationUnavailable), class: "configuration"},
		{name: "invalid target", err: errOSSDeletionTargetIncomplete, class: "invalid_target"},
		{name: "deadline", err: context.DeadlineExceeded, class: "timeout", retryable: true},
		{name: "authentication", err: &aliyunoss.ServiceError{StatusCode: 403, Code: "InvalidAccessKeyId"}, class: "authentication"},
		{name: "authorization", err: &aliyunoss.ServiceError{StatusCode: 403, Code: "AccessDenied"}, class: "authorization"},
		{name: "bad target", err: &aliyunoss.ServiceError{StatusCode: 404, Code: "NoSuchBucket"}, class: "invalid_target"},
		{name: "rate limit", err: &aliyunoss.ServiceError{StatusCode: 429, Code: "TooManyRequests"}, class: "rate_limit", retryable: true},
		{name: "remote", err: &aliyunoss.ServiceError{StatusCode: 503, Code: "ServiceUnavailable"}, class: "remote_transient", retryable: true},
		{name: "network", err: &net.DNSError{Err: "temporary", IsTemporary: true}, class: "network", retryable: true},
		{name: "unknown", err: errors.New("unknown"), class: "unknown", retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyOSSDeletionFailure(test.err)
			if got.class != test.class || got.retryable != test.retryable {
				t.Fatalf("decision = %#v, want %s/retryable=%t", got, test.class, test.retryable)
			}
		})
	}
}

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
