package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type githubIdentityFixtureTransport struct {
	token, user, emails     string
	emailStatus, emailReads int
}

func (f *githubIdentityFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	status, body := http.StatusOK, ""
	switch r.URL.Host + r.URL.Path {
	case "github.com/login/oauth/access_token":
		body = f.token
	case "api.github.com/user":
		body = f.user
	case "api.github.com/user/emails":
		body, status = f.emails, f.emailStatus
		f.emailReads++
	default:
		return nil, errors.New("unexpected provider fixture endpoint")
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
}

func TestOAuthGitHubVerifiedIdentityAndOptionalEmail(t *testing.T) {
	for _, tc := range []struct {
		name, token, user, emails string
		status                    int
		wantError, wantLookup     bool
		wantEmail                 string
	}{
		{"missing account id", `{"access_token":"fixture"}`, `{"login":"ordinary"}`, "[]", 200, true, false, ""},
		{"negative account id", `{"access_token":"fixture"}`, `{"id":-1}`, "[]", 200, true, false, ""},
		{"withheld scope", `{"access_token":"fixture","scope":"read:user"}`, `{"id":7,"email":"public@example.invalid"}`, "[]", 200, false, false, ""},
		{"empty verified list", `{"access_token":"fixture","scope":"user"}`, `{"id":7}`, "[]", 200, false, true, ""},
		{"unverified entry", `{"access_token":"fixture"}`, `{"id":7}`, `[{"email":"public@example.invalid","primary":true}]`, 200, false, true, ""},
		{"prefer verified primary", `{"access_token":"fixture","scope":"read:user, user:email"}`, `{"id":7}`, `[{"email":"other@example.invalid","verified":true},{"email":"MAIN@example.invalid","verified":true,"primary":true}]`, 200, false, true, "main@example.invalid"},
		{"temporarily unavailable", `{"access_token":"fixture"}`, `{"id":7}`, "", 503, true, true, ""},
		{"malformed response", `{"access_token":"fixture"}`, `{"id":7}`, "null", 200, true, true, ""},
		{"malformed verified address", `{"access_token":"fixture"}`, `{"id":7}`, `[{"verified":true}]`, 200, true, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &githubIdentityFixtureTransport{token: tc.token, user: tc.user, emails: tc.emails, emailStatus: tc.status}
			previous := oauthHTTPClient
			oauthHTTPClient = &http.Client{Transport: transport}
			t.Cleanup(func() { oauthHTTPClient = previous })
			profile, err := fetchGitHubProfile(t.Context(), oauthProviderConfig{}, "synthetic-code")
			if transport.emailReads != 0 || (profile.loadVerifiedEmail != nil) != tc.wantLookup {
				t.Fatal("email was read before lookup of the stable provider binding, or the optional scope was lost")
			}
			email := ""
			if err == nil && profile.loadVerifiedEmail != nil {
				email, err = profile.loadVerifiedEmail(t.Context())
			}
			if (err != nil) != tc.wantError || email != tc.wantEmail {
				t.Fatalf("email state valid=%t returned=%q, want valid=%t email=%q", err == nil, email, !tc.wantError, tc.wantEmail)
			}
		})
	}
}

func TestOAuthGitHubEmailFailurePreservesLinkAndFirstLoginRetryIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	errProvider := errors.New("synthetic provider unavailable")
	profile := oauthProviderProfile{Provider: "github", ProviderUserID: "7654321", Username: "fixture-oauth"}
	var reads int
	email := ""
	profile.loadVerifiedEmail = func(_ context.Context) (string, error) {
		reads++
		if email == "" {
			return "", errProvider
		}
		return email, nil
	}
	var usersBefore int64
	if err := f.db.QueryRow(f.ctx, "select count(*) from users").Scan(&usersBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := f.server.findOrCreateOAuthUser(f.ctx, profile, clientLocation{}); !errors.Is(err, errOAuthIdentityUnavailable) || !errors.Is(err, errProvider) {
		t.Fatalf("missing provider state=%v", err)
	}
	var usersAfter, bindings int64
	if err := f.db.QueryRow(f.ctx, "select (select count(*) from users),(select count(*) from oauth_accounts where provider=$1 and provider_user_id=$2)", profile.Provider, profile.ProviderUserID).Scan(&usersAfter, &bindings); err != nil || usersBefore != usersAfter || bindings != 0 {
		t.Fatalf("failed first login changed account facts before=%d after=%d bindings=%d err=%v", usersBefore, usersAfter, bindings, err)
	}
	knownID := f.userIDs[f.editor]
	if err := f.db.QueryRow(f.ctx, "select email from users where id=$1", knownID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	account, err := f.server.findOrCreateOAuthUser(f.ctx, profile, clientLocation{})
	if err != nil || account.ID != knownID {
		t.Fatalf("verified email retry matched id=%d expected=%d err=%v", account.ID, knownID, err)
	}
	email = ""
	account, err = f.server.findOrCreateOAuthUser(f.ctx, profile, clientLocation{})
	if err != nil || account.ID != knownID || reads != 2 {
		t.Fatalf("existing binding id=%d reads=%d err=%v", account.ID, reads, err)
	}
}
