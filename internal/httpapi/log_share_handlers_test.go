package httpapi

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRedactLogTextMasksSensitiveValuesWithSnowflake(t *testing.T) {
	input := "Authorization: Bearer secret-token\nemail=user@example.com\nip=192.168.1.2\nC:\\Users\\alice\\AppData\n/home/bob/.config\n"
	output, counts := redactLogText(input)
	for _, secret := range []string{"secret-token", "user@example.com", "192.168.1.2", "alice", "bob"} {
		if strings.Contains(output, secret) {
			t.Fatalf("redacted output leaked %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, "❄") || counts["authorization"] != 1 || counts["email"] != 1 || counts["ipv4"] != 1 {
		t.Fatalf("unexpected redaction result: output=%q counts=%v", output, counts)
	}
}

func TestSanitizeLogZipRejectsTraversalAndRedactsEntries(t *testing.T) {
	unsafe := makeTestLogZip(t, map[string]string{"../latest.log": "secret"})
	if _, _, err := sanitizeLogZip(unsafe); err == nil {
		t.Fatal("expected zip path traversal to be rejected")
	}

	safe := makeTestLogZip(t, map[string]string{"logs/latest.log": "user@example.com\njava.lang.Error"})
	entries, counts, err := sanitizeLogZip(safe)
	if err != nil {
		t.Fatalf("sanitize safe zip: %v", err)
	}
	if len(entries) != 1 || strings.Contains(entries[0].Text, "user@example.com") || counts["email"] != 1 {
		t.Fatalf("unexpected sanitized zip: %#v %v", entries, counts)
	}
}

func TestSanitizeLogZipRejectsDuplicateNames(t *testing.T) {
	duplicate := makeTestLogZip(t, map[string]string{"A/latest.log": "first", "a/LATEST.LOG": "second"})
	if _, _, err := sanitizeLogZip(duplicate); err == nil {
		t.Fatal("expected case-insensitive duplicate ZIP names to be rejected")
	}
}

func TestLogShareExpiryAndCode(t *testing.T) {
	now := time.Date(2026, 8, 17, 1, 2, 3, 0, time.UTC)
	if _, err := logShareExpiry(now, 0); err != nil {
		t.Fatalf("default retention should be valid: %v", err)
	}
	if _, err := logShareExpiry(now, -1); err == nil {
		t.Fatal("negative retention must be rejected")
	}
	if _, err := logShareExpiry(now, 1097); err == nil {
		t.Fatal("retention over three years must be rejected")
	}
	left, err := randomLogShareCode()
	if err != nil || len(left) < 20 {
		t.Fatalf("unexpected code: %q %v", left, err)
	}
	right, _ := randomLogShareCode()
	if left == right {
		t.Fatal("random public codes must not repeat")
	}
}

func makeTestLogZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, value := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
