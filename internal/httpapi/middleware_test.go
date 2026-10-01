package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
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

func TestAuthenticationLookupFailurePreservesSession(t *testing.T) {
	// A closed real pgx pool reproduces an infrastructure failure without
	// connecting to an external database or replacing the authentication query.
	pool, err := pgxpool.New(context.Background(), "postgres://audit@127.0.0.1:1/audit?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	server := &Server{cfg: config.Config{JWTSecret: "test-secret"}, db: pool, cache: cache}
	claims, err := security.NewClaims("abc234567", "audit", "audit@example.test", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, err := security.SignToken(server.cfg.JWTSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			called := false
			next := func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }
			handler := server.requireAuth(next)
			if optional {
				handler = server.optionalAuth(next)
			}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: token})
			response := httptest.NewRecorder()
			handler(response, request)
			if response.Code != http.StatusServiceUnavailable || called {
				t.Fatalf("status=%d called=%v, want infrastructure failure without running handler", response.Code, called)
			}
			if response.Header().Get(authStateHeader) != "" {
				t.Fatal("infrastructure failure must not invalidate authentication")
			}
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == authSessionCookieName {
					t.Fatal("infrastructure failure must preserve the authentication cookie")
				}
			}
		})
	}
}
