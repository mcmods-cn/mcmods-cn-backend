// Package diagnostic formats bounded error details for worker status and recovery.
package diagnostic

import (
	"strings"
	"unicode/utf8"
)

// Text returns at most maxBytes of PostgreSQL-compatible UTF-8 text. It preserves
// complete characters and replaces invalid bytes and NULs in diagnostic messages.
func Text(message string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(message) <= maxBytes && utf8.ValidString(message) && !strings.ContainsRune(message, 0) {
		return message
	}
	var result strings.Builder
	result.Grow(min(len(message), maxBytes))
	for _, r := range message {
		if r == 0 {
			r = utf8.RuneError
		}
		if result.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		result.WriteRune(r)
	}
	return result.String()
}
