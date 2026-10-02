package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
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

func TestRequireAuthPreservesSessionWhenAuthenticationStoreUnavailable(t *testing.T) {
	// Creating a zero-minimum pool is lazy; closing it before the request makes
	// the failure deterministic without contacting any database.
	pool, err := pgxpool.New(context.Background(), "postgres://127.0.0.1:1/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	const secret = "synthetic-authentication-secret"
	claims, err := security.NewClaims("authuser1", "example", "example@example.invalid", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, err := security.SignToken(secret, claims)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Config{JWTSecret: secret}}
	called := false
	handler := server.requireAuth(func(http.ResponseWriter, *http.Request) { called = true })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: token})
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("status=%d called=%v body=%s; want 503 with no protected handler call", response.Code, called, response.Body.String())
	}
	if response.Header().Get(authStateHeader) != "" || len(response.Result().Cookies()) != 0 {
		t.Fatal("temporary authentication storage failure must not invalidate or clear the session")
	}
	invalid := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	invalid.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: "invalid-token"})
	invalidResponse := httptest.NewRecorder()
	handler(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusUnauthorized || called {
		t.Fatalf("invalid session status=%d called=%v; want 401", invalidResponse.Code, called)
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
	if got := response.Header().Get(authStateHeader); got != authStateInvalid {
		t.Fatalf("authentication state header = %q, want %q", got, authStateInvalid)
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
	if got := response.Header().Get(authStateHeader); got != authStateInvalid {
		t.Fatalf("authentication state header = %q, want %q", got, authStateInvalid)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == authSessionCookieName {
			t.Fatal("explicit authorization must not mutate browser cookies")
		}
	}
}

func TestOptionalAuthWithoutSessionDoesNotSignalInvalidAuthentication(t *testing.T) {
	server := &Server{cfg: config.Config{JWTSecret: "test-secret"}}
	handler := server.optionalAuth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
	if got := response.Header().Get(authStateHeader); got != "" {
		t.Fatalf("guest request was marked as invalid authentication: %q", got)
	}
}
