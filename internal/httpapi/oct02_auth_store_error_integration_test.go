package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02AuthStorageFailuresRemainDistinctFromInvalidCredentialsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	s := &Server{db: f.db, cache: cache}
	if _, err := f.db.Exec(f.ctx, `alter table email_verification_codes rename column code_hash to oct02_broken_code_hash`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body string
		handler    http.HandlerFunc
	}{
		{"register", `{"username":"SyntheticAuthStore","email":"synthetic-auth-store@example.invalid","password":"synthetic-long-password","code":"123456"}`, s.register},
		{"email login", `{"email":"synthetic-auth-store@example.invalid","code":"123456"}`, s.emailLogin},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/fixture/auth", strings.NewReader(test.body)).WithContext(f.ctx)
			w := httptest.NewRecorder()
			test.handler(w, r)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("storage failure status=%d, want503 body=%s", w.Code, w.Body.String())
			}
		})
	}
	if _, err := f.db.Exec(f.ctx, `alter table email_verification_codes rename column oct02_broken_code_hash to code_hash`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/fixture/auth", strings.NewReader(`{"email":"synthetic-auth-store@example.invalid","code":"123456"}`)).WithContext(f.ctx)
	w := httptest.NewRecorder()
	s.emailLogin(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing verification code status=%d, want401", w.Code)
	}
	closed, err := pgxpool.NewWithConfig(f.ctx, f.db.Config())
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	r = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	r = r.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor]}))
	w = httptest.NewRecorder()
	(&Server{db: closed}).me(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("me storage outage status=%d, want503", w.Code)
	}
	r = r.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: -1}))
	w = httptest.NewRecorder()
	s.me(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("actual missing user status=%d, want404", w.Code)
	}
}

func TestOCT02AdminUserStorageFailureIsNotAUsernameConflictIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `alter table users add constraint oct02_reject_admin_user check(username<>'SyntheticAuthStore') not valid`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", strings.NewReader(`{"username":"SyntheticAuthStore","email":"synthetic-auth-store@example.invalid","password":"synthetic-long-password"}`)).WithContext(f.ctx)
	w := httptest.NewRecorder()
	(&Server{db: f.db}).createAdminUser(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("nonunique storage failure status=%d want500 body=%s", w.Code, w.Body.String())
	}
	var exists bool
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from users where username='SyntheticAuthStore')`).Scan(&exists); err != nil || exists {
		t.Fatalf("failed admin create persisted user=%v err=%v", exists, err)
	}
}
