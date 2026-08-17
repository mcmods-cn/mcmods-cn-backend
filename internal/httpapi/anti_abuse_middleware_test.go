package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/antiabuse"
	"mcmods-cn-backend/internal/security"
)

func TestAntiAbuseActionClassification(t *testing.T) {
	tests := []struct{ method, path, body, want string }{
		{http.MethodGet, "/api/v1/mods", "", ""},
		{http.MethodPost, "/api/v1/comment-targets/mod/test/comments", `{"body":"hello"}`, "comment.create"},
		{http.MethodPost, "/api/v1/comment-targets/mod/test/comments", `{"body":"hello","parentId":"parent"}`, "comment.reply"},
		{http.MethodPost, "/api/v1/community/posts", `{}`, "community.submit"},
		{http.MethodPost, "/api/v1/mods/example/content-versions", `{}`, "review.submit"},
		{http.MethodPut, "/api/v1/mods/example/content-versions/version-1", `{}`, "review.submit"},
		{http.MethodPost, "/api/v1/mods/example/content-sections", `{}`, "review.submit"},
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

func TestAntiAbuseRateScopeSeparatesContentVersionMutations(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/api/v1/mods/example/content-versions", "mod_content_version_create"},
		{http.MethodPut, "/api/v1/mods/example/content-versions/version-1", "mod_content_version_update"},
		{http.MethodDelete, "/api/v1/mods/example/content-versions/version-1", "mod_content_version_delete"},
		{http.MethodPost, "/api/v1/mods/example/content-sections", ""},
		{http.MethodPost, "/api/v1/mods", ""},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, nil)
		if got := antiAbuseRateScope(request); got != test.want {
			t.Errorf("%s %s scope = %q, want %q", test.method, test.path, got, test.want)
		}
	}
}

func TestAntiAbuseRateLimitPercentUsesVariablePermissions(t *testing.T) {
	claims := security.Claims{PermissionRules: []security.PermissionRule{
		{Code: "security.anti-abuse.rate_multiplier.150", Allow: true, Priority: 10},
		{Code: "security.anti-abuse.rate_multiplier.review_submit.300", Allow: true, Priority: 10},
	}}
	if got := antiAbuseRateLimitPercent(claims, "comment.create"); got != 150 {
		t.Fatalf("global rate allowance = %d, want 150", got)
	}
	if got := antiAbuseRateLimitPercent(claims, "review.submit"); got != 300 {
		t.Fatalf("review.submit rate allowance = %d, want 300", got)
	}
	if got := antiAbuseRateLimitPercent(security.Claims{}, "review.submit"); got != 100 {
		t.Fatalf("missing rate permission = %d, want safe default 100", got)
	}
	admin := security.Claims{PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 100}}}
	if got := antiAbuseRateLimitPercent(admin, "review.submit"); got != antiabuse.MaxRateLimitPercent {
		t.Fatalf("administrator allowance = %d, want bounded maximum %d", got, antiabuse.MaxRateLimitPercent)
	}
}

func TestBannedAccountMarkerRequiresExplicitPermission(t *testing.T) {
	administrator := security.Claims{PermissionRules: []security.PermissionRule{
		{Code: "admin.*", Allow: true, Priority: 100},
	}}
	if claimsExplicitlyAllow(administrator, "account.banned") {
		t.Fatal("administrator wildcard must not imply the account.banned state marker")
	}

	banned := security.Claims{PermissionRules: []security.PermissionRule{
		{Code: "account.banned", Allow: true, Priority: 100},
		{Code: "*", Allow: false, Priority: 100},
	}}
	if !claimsExplicitlyAllow(banned, "account.banned") {
		t.Fatal("explicit account.banned marker must remain effective")
	}

	revoked := security.Claims{PermissionRules: []security.PermissionRule{
		{Code: "account.banned", Allow: true, Priority: 50},
		{Code: "account.banned", Allow: false, Priority: 100},
	}}
	if claimsExplicitlyAllow(revoked, "account.banned") {
		t.Fatal("higher-priority explicit denial must override the marker")
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
