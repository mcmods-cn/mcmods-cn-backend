package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeOAuthNext(t *testing.T) {
	t.Parallel()
	valid := []string{"", "/", "/skins", "/skins/upload?kind=skin#editor", "/zh-CN/users/me"}
	for _, value := range valid {
		value := value
		t.Run("valid_"+value, func(t *testing.T) {
			got, ok := normalizeOAuthNext(value)
			if !ok || got != value {
				t.Fatalf("normalizeOAuthNext(%q) = %q, %v", value, got, ok)
			}
		})
	}
	invalid := []string{"https://evil.example", "//evil.example", `\\evil.example`, `/\\evil.example`, "/%5cevil.example", "skins"}
	for _, value := range invalid {
		value := value
		t.Run("invalid_"+value, func(t *testing.T) {
			if got, ok := normalizeOAuthNext(value); ok || got != "" {
				t.Fatalf("normalizeOAuthNext(%q) = %q, %v", value, got, ok)
			}
		})
	}
}

func TestOAuthNextFromRequest(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/callback", nil)
	next := "/skins/upload?kind=cape"
	request.AddCookie(&http.Cookie{
		Name:  oauthNextCookie("github"),
		Value: base64.RawURLEncoding.EncodeToString([]byte(next)),
	})
	if got := oauthNextFromRequest(request, "github"); got != next {
		t.Fatalf("oauthNextFromRequest() = %q, want %q", got, next)
	}
}
