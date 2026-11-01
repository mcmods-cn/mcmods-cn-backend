package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func auditOSS001Content(t *testing.T, method, path string, body string) string {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	raw, content := inspectAntiAbuseBody(request)
	if string(raw) != body {
		t.Fatal("body must remain unchanged for the real DTO/byte binding")
	}
	return content
}
func TestAuditOSS001CreatorAvatarFingerprintRegisteredScopeAndBudgets(t *testing.T) {
	a := `{"kind":"author","name":"Unchanged actual biography","avatarFileId":"Avatar001"}`
	b := `{"avatarFileId":"avatar002","name":"Unchanged actual biography","kind":"author"}`
	for _, route := range []struct{ method, path string }{{http.MethodPost, "/api/v1/creators"}, {http.MethodPut, "/api/v1/creators/creator01"}} {
		before := auditOSS001Content(t, route.method, route.path, a)
		after := auditOSS001Content(t, route.method, route.path, b)
		if before == after || !strings.Contains(before, "creator-avatar:avatar001") || !strings.Contains(after, "creator-avatar:avatar002") {
			t.Fatal("registered actual image identity must distinguish unchanged creator text")
		}
		reordered := `{"name":"Unchanged actual biography","avatarFileId":"avatar001","kind":"author"}`
		if before != auditOSS001Content(t, route.method, route.path, reordered) {
			t.Fatal("canonical key order and public ID case must be stable")
		}
	}
	for _, route := range []struct{ method, path string }{{http.MethodPost, "/api/v1/mods"}, {http.MethodPost, "/api/v1/community/posts"}, {http.MethodPost, "/api/v1/creators/creator01/claims"}, {http.MethodPut, "/api/v1/creators/creator01/members"}, {http.MethodPatch, "/api/v1/creators/creator01"}, {http.MethodPut, "/api/v1/creators/not-a-valid-target"}} {
		if auditOSS001Content(t, route.method, route.path, a) != auditOSS001Content(t, route.method, route.path, b) {
			t.Fatal("unregistered endpoint must not treat avatarFileId as a nonce", route)
		}
	}
	nestedA := `{"kind":"author","name":"Same text","extra":{"avatarFileId":"avatar001"}}`
	nestedB := strings.ReplaceAll(nestedA, "avatar001", "avatar002")
	if auditOSS001Content(t, http.MethodPost, "/api/v1/creators", nestedA) != auditOSS001Content(t, http.MethodPost, "/api/v1/creators", nestedB) {
		t.Fatal("nested unknown identity must remain unregistered")
	}
	invalidA := `{"kind":"other","name":"Same text","avatarFileId":"avatar001"}`
	invalidB := strings.ReplaceAll(invalidA, "avatar001", "avatar002")
	if auditOSS001Content(t, http.MethodPost, "/api/v1/creators", invalidA) != auditOSS001Content(t, http.MethodPost, "/api/v1/creators", invalidB) {
		t.Fatal("invalid creator kind must not register a reference")
	}
	fields := []map[string]string{}
	for i := 0; i < 20; i++ {
		fields = append(fields, map[string]string{"name": strings.Repeat("x", 20001)})
	}
	raw, err := json.Marshal(map[string]any{"kind": "author", "avatarFileId": "avatar001", "details": fields})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(auditOSS001Content(t, http.MethodPost, "/api/v1/creators", string(raw)), "\n")
	if len(parts) != 16 {
		t.Fatal("registered identity must share the original sixteen-field budget", len(parts))
	}
	for _, part := range parts {
		if strings.HasPrefix(part, "creator-avatar:") {
			if len([]rune(part)) > 20000 {
				t.Fatal("registered reference exceeds original payload budget")
			}
			continue
		}
		// Existing truncateRunes preserves20k payload runes plus its existing
		// display ellipsis. That marker is not an extra business-content rune.
		if part != strings.Repeat("x", 20000)+"…" {
			t.Fatal("original exact twenty-thousand payload runes and ellipsis contract changed")
		}
	}
}
