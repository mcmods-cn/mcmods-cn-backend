package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAntiAbuseActionClassification(t *testing.T) {
	tests := []struct{ method, path, body, want string }{
		{http.MethodGet, "/api/v1/mods", "", ""},
		{http.MethodPost, "/api/v1/comment-targets/mod/test/comments", `{"body":"hello"}`, "comment.create"},
		{http.MethodPost, "/api/v1/comment-targets/mod/test/comments", `{"body":"hello","parentId":"parent"}`, "comment.reply"},
		{http.MethodPost, "/api/v1/community/posts", `{}`, "community.submit"},
		{http.MethodPost, "/api/v1/mods", `{}`, "review.submit"},
		{http.MethodPost, "/api/v1/content-metrics/test/view", `{}`, ""},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		if got := antiAbuseAction(request); got != test.want {
			t.Errorf("%s %s classified as %q, want %q", test.method, test.path, got, test.want)
		}
	}
}

func TestInspectAntiAbuseBodyRestoresContent(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/community/posts", strings.NewReader(`{"title":"Title","bodyMarkdown":"Body"}`))
	request.Header.Set("Content-Type", "application/json")
	body, content := inspectAntiAbuseBody(request)
	if len(body) == 0 || !strings.Contains(content, "Title") || !strings.Contains(content, "Body") {
		t.Fatalf("unexpected body inspection: %q", content)
	}
}
