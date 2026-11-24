package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

func TestWebSessionIsBoundToProvedAccountRevision(t *testing.T) {
	f := newAuditFixGRFixture(t)
	var email string
	if err := f.db.QueryRow(f.ctx, "select email from users where id=$1", f.userIDs[f.editor]).Scan(&email); err != nil {
		t.Fatal(err)
	}
	account, _, err := f.server.findUserForLogin(f.ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	const currentPassword = "fixture-rotated-session-password"
	hash, err := security.HashPassword(currentPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update users set password_hash=$1 where id=$2", hash, account.ID); err != nil {
		t.Fatal(err)
	}
	var countBefore int
	if err = f.db.QueryRow(f.ctx, "select count(*) from auth_sessions where user_id=$1", account.ID).Scan(&countBefore); err != nil {
		t.Fatal(err)
	}
	if session, err := f.server.issueToken(f.ctx, f.db, account); !errors.Is(err, pgx.ErrNoRows) || session != (issuedAuthSession{}) {
		t.Fatal("changed credential revision must require a new authentication")
	}
	var countAfter int
	if err = f.db.QueryRow(f.ctx, "select count(*) from auth_sessions where user_id=$1", account.ID).Scan(&countAfter); err != nil || countAfter != countBefore {
		t.Fatalf("changed revision wrote a session: before %d after %d err %v", countBefore, countAfter, err)
	}
	f.require(t, "", http.MethodPost, "/api/v1/auth/login", map[string]string{"account": email, "password": currentPassword}, http.StatusOK)
	var live int
	if err = f.db.QueryRow(f.ctx, "select count(*) from auth_sessions s join users u on u.id=s.user_id and u.auth_version=s.auth_version where u.id=$1", account.ID).Scan(&live); err != nil || live != 1 {
		t.Fatalf("normal login with rotated credential has %d current sessions, error %v", live, err)
	}
}

func TestEmailCodeSessionWritesRetryTogether(t *testing.T) {
	f := newAuditFixGRFixture(t)
	const email = "register-retry@example.invalid"
	const code = "217486"
	hash, err := security.HashCode(code)
	if err != nil {
		t.Fatal(err)
	}
	seed := func(purpose string) int64 {
		t.Helper()
		var id int64
		err := f.db.QueryRow(f.ctx, "insert into email_verification_codes(email,purpose,code_hash,expires_at) values($1,$2,$3,clock_timestamp()+interval '10 minutes') returning id", email, purpose, hash).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	for _, tc := range []struct {
		purpose, path string
		body          map[string]string
		status        int
	}{
		{"register", "/api/v1/auth/register", map[string]string{"username": "sessionretry", "email": email, "password": "fixture-session-retry", "code": code}, http.StatusCreated},
		{"login", "/api/v1/auth/email-login", map[string]string{"email": email, "code": code}, http.StatusOK},
	} {
		t.Run(tc.purpose, func(t *testing.T) {
			id := seed(tc.purpose)
			if _, err := f.db.Exec(f.ctx, "alter table auth_sessions add constraint fixture_session_store_unavailable check(false) not valid"); err != nil {
				t.Fatal(err)
			}
			f.require(t, "", http.MethodPost, tc.path, tc.body, http.StatusServiceUnavailable)
			var consumed, accountExists bool
			if err := f.db.QueryRow(f.ctx, "select consumed_at is not null,exists(select 1 from users where email=$2) from email_verification_codes where id=$1", id, email).Scan(&consumed, &accountExists); err != nil || consumed || accountExists != (tc.purpose == "login") {
				t.Fatalf("after storage failure consumed=%v account=%v err=%v", consumed, accountExists, err)
			}
			if _, err := f.db.Exec(f.ctx, "alter table auth_sessions drop constraint fixture_session_store_unavailable"); err != nil {
				t.Fatal(err)
			}
			f.require(t, "", http.MethodPost, tc.path, tc.body, tc.status)
			if err := f.db.QueryRow(f.ctx, "select consumed_at is not null from email_verification_codes where id=$1", id).Scan(&consumed); err != nil || !consumed {
				t.Fatalf("successful login did not consume code, err=%v", err)
			}
			f.require(t, "", http.MethodPost, tc.path, tc.body, http.StatusUnauthorized)
		})
	}
	t.Run("expired before consumption", func(t *testing.T) {
		id := seed("login")
		verified, err := verifiedEmailCodeID(f.ctx, f.db, email, "login", code)
		if err != nil || verified != id {
			t.Fatalf("valid code lookup returned %d, want %d, err %v", verified, id, err)
		}
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		if _, err = tx.Exec(f.ctx, "update email_verification_codes set expires_at=clock_timestamp() where id=$1", id); err != nil {
			t.Fatal(err)
		}
		if err = claimVerifiedEmailCode(f.ctx, tx, verified); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expired code must not be consumed: %v", err)
		}
	})
}
