package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestCookieMutationRequiresExactOrigin(t *testing.T) {
	server := &Server{cfg: config.Config{FrontendOrigin: "https://mcmods.example"}}
	called := false
	handler := server.cookieRequestOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
	for _, test := range []struct {
		name, origin string
		want         int
	}{{"missing", "", http.StatusForbidden}, {"cross-site", "https://evil.example", http.StatusForbidden}, {"same-origin", "https://mcmods.example", http.StatusNoContent}} {
		t.Run(test.name, func(t *testing.T) {
			called = false
			request := httptest.NewRequest(http.MethodPost, "/api/v1/comments", nil)
			request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: "cookie"})
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want || called != (test.want == http.StatusNoContent) {
				t.Fatalf("status=%d called=%v", response.Code, called)
			}
		})
	}
}

func TestBearerMutationDoesNotRequireBrowserOrigin(t *testing.T) {
	server := &Server{cfg: config.Config{FrontendOrigin: "https://mcmods.example"}}
	handler := server.cookieRequestOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/comments", nil)
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: "cookie"})
	request.Header.Set("Authorization", "Bearer api-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestOptionalAuthTreatsInvalidCookieAsGuest(t *testing.T) {
	server := &Server{cfg: config.Config{JWTSecret: "test-secret"}}
	called := false
	handler := server.optionalAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if claims := currentClaims(r); claims.Subject != 0 {
			t.Fatalf("expected guest claims, got subject %d", claims.Subject)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: "invalid-session"})
	response := httptest.NewRecorder()

	handler(response, request)

	if !called {
		t.Fatal("expected public handler to run")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
	cleared := false
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == authSessionCookieName && cookie.MaxAge < 0 {
			cleared = true
			break
		}
	}
	if !cleared {
		t.Fatal("expected invalid authentication cookie to be cleared")
	}
}

func TestOptionalAuthTreatsInvalidAuthorizationAsGuestWithoutClearingCookie(t *testing.T) {
	server := &Server{cfg: config.Config{JWTSecret: "test-secret"}}
	handler := server.optionalAuth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	request.Header.Set("Authorization", "Bearer invalid-session")
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == authSessionCookieName {
			t.Fatal("explicit authorization must not mutate browser cookies")
		}
	}
}
