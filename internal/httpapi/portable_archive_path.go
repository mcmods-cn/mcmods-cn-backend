package httpapi

import "strings"

// ZIP names must be safe on both Unix and Windows: sharing a sanitized archive
// does not control which operating system will eventually extract it.
func portableArchiveEntryName(raw string, maximumDepth int) (string, bool) {
	name := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, ":\x00") {
		return "", false
	}
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	if len(parts)-1 > maximumDepth {
		return "", false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return strings.Join(parts, "/"), true
}
