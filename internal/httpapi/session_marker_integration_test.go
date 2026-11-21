package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestConditionalLogoutPreservesReplacedBrowserSessionsHTTPPostgres(t *testing.T) {
	f := newAuditFixGRFixture(t)
	const logoutPath = "/auth/logout/if-current"
	const password = "session-fixture-password"
	const code = "842716"

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	apiURL, err := url.Parse(f.origin.URL + "/api/v1/")
	if err != nil {
		t.Fatal(err)
	}
	type reply struct {
		Data struct {
			SessionMarker string              `json:"sessionMarker"`
			ID            string              `json:"id"`
			User          struct{ ID string } `json:"user"`
		} `json:"data"`
		Code string
	}
	request := func(method, path string, body any, want int) (reply, http.Header) {
		t.Helper()
		var raw []byte
		if body != nil {
			var err error
			raw, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequestWithContext(f.ctx, method, f.origin.URL+"/api/v1"+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", f.server.cfg.FrontendOrigin)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out reply
		if err = json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != want {
			t.Fatalf("%s %s returned %d, want %d (code %q, decode %v)", method, path, resp.StatusCode, want, out.Code, err)
		}
		return out, resp.Header
	}
	seedCode := func(email, purpose string) {
		t.Helper()
		hash, err := security.HashCode(code)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.db.Exec(f.ctx, "insert into email_verification_codes(email,purpose,code_hash,expires_at) values($1,$2,$3,now()+interval '5 minutes')", email, purpose, hash)
		if err != nil {
			t.Fatal(err)
		}
	}
	type session struct {
		marker, userID string
		hash           []byte
	}
	checkLogin := func(login reply) session {
		t.Helper()
		me, headers := request(http.MethodGet, "/auth/me", nil, 200)
		if login.Data.SessionMarker == "" || login.Data.SessionMarker != me.Data.SessionMarker || me.Data.ID == "" || me.Data.ID != login.Data.User.ID || headers.Get("Cache-Control") != "private, no-store" {
			t.Fatal("login and /me did not identify the same private session")
		}
		for _, cookie := range jar.Cookies(apiURL) {
			if cookie.Name != authSessionCookieName {
				continue
			}
			claims, err := security.ParseToken(f.server.cfg.JWTSecret, cookie.Value)
			if err != nil || login.Data.SessionMarker == cookie.Value || login.Data.SessionMarker == claims.SessionID {
				t.Fatal("session marker must be distinct from both the signed cookie and raw session ID")
			}
			request(http.MethodPost, "/site/presence", map[string]string{}, 200)
			return session{login.Data.SessionMarker, me.Data.ID, security.SessionFingerprint(claims.SessionID)}
		}
		t.Fatal("actual login failed to set its session cookie")
		return session{}
	}
	checkDatabase := func(s session, revoked, present bool) {
		t.Helper()
		var gotRevoked, gotPresent bool
		err := f.db.QueryRow(f.ctx, "select revoked_at is not null, exists(select 1 from user_presence_sessions where session_hash=$1) from auth_sessions where session_hash=$1", s.hash).Scan(&gotRevoked, &gotPresent)
		if err != nil || revoked != gotRevoked || present != gotPresent {
			t.Fatalf("database session revoked=%t present=%t expected %t/%t, err=%v", gotRevoked, gotPresent, revoked, present, err)
		}
	}
	preserveCookie := func(headers http.Header) {
		t.Helper()
		if len(headers.Values("Set-Cookie")) != 0 {
			t.Fatal("bound logout attempted to change the shared cookie")
		}
	}
	postMarker := func(marker string, status int) reply {
		t.Helper()
		out, headers := request(http.MethodPost, logoutPath, map[string]string{"sessionMarker": marker}, status)
		preserveCookie(headers)
		return out
	}

	const firstEmail = "targeted-session@example.invalid"
	seedCode(firstEmail, "register")
	result, _ := request(http.MethodPost, "/auth/register", map[string]string{"username": "markerfixture", "email": firstEmail, "password": password, "code": code}, 201)
	first := checkLogin(result)
	f.require(t, first.marker, http.MethodGet, "/api/v1/auth/me", nil, 401) // A marker alone never authenticates.
	if _, err = f.db.Exec(f.ctx, "alter table auth_sessions add constraint fixture_logout_unavailable check(revoked_at is null) not valid"); err != nil {
		t.Fatal(err)
	}
	postMarker(first.marker, 503)
	checkDatabase(first, false, true)
	if _, err = f.db.Exec(f.ctx, "alter table auth_sessions drop constraint fixture_logout_unavailable"); err != nil {
		t.Fatal(err)
	}

	result, _ = request(http.MethodPost, "/auth/login", map[string]string{"account": firstEmail, "password": password}, 200)
	second := checkLogin(result)
	if first.userID != second.userID || first.marker == second.marker {
		t.Fatal("same account login must create a distinct browser session")
	}
	if postMarker(first.marker, 409).Code != "AUTH_SESSION_CHANGED" {
		t.Fatal("same-account mismatch missing stable code")
	}
	checkDatabase(first, false, true)
	checkDatabase(second, false, true)

	var otherEmail string
	if err = f.db.QueryRow(f.ctx, "select email from users where id=$1", f.userIDs[f.otherEditor]).Scan(&otherEmail); err != nil {
		t.Fatal(err)
	}
	seedCode(otherEmail, "login")
	result, _ = request(http.MethodPost, "/auth/email-login", map[string]string{"email": otherEmail, "code": code}, 200)
	other := checkLogin(result)
	if second.userID == other.userID || second.marker == other.marker {
		t.Fatal("other account login must create a distinct browser session")
	}
	if postMarker(second.marker, 409).Code != "AUTH_SESSION_CHANGED" {
		t.Fatal("cross-account mismatch missing stable code")
	}
	for _, body := range []any{nil, map[string]string{}, map[string]any{"sessionMarker": nil}, map[string]string{"sessionMarker": ""}} {
		_, headers := request(http.MethodPost, logoutPath, body, 400)
		preserveCookie(headers)
	}
	checkDatabase(second, false, true)
	checkDatabase(other, false, true)
	postMarker(other.marker, 200)
	checkDatabase(other, true, false)
	request(http.MethodGet, "/auth/me", nil, 401)
	postMarker(other.marker, 401)

	result, _ = request(http.MethodPost, "/auth/login", map[string]string{"account": firstEmail, "password": password}, 200)
	legacy := checkLogin(result)
	_, headers := request(http.MethodPost, "/auth/logout", nil, 200)
	if len(headers.Values("Set-Cookie")) == 0 || len(jar.Cookies(apiURL)) != 0 {
		t.Fatal("legacy explicit logout did not expire the cookie")
	}
	checkDatabase(legacy, true, false)
	request(http.MethodGet, "/auth/me", nil, 401)
}

func TestConditionalLogoutReportsCommittedCacheFailure(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := t.Context()
	claims, err := security.NewClaims("u00000042", "fixture", "fixture@example.invalid", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims.Subject = 42
	if _, err = pool.Exec(ctx, `create table auth_sessions(session_hash bytea primary key,user_id bigint,revoked_at timestamptz);
		create table user_presence_sessions(session_hash bytea,user_id bigint)`); err != nil {
		t.Fatal(err)
	}
	hash := security.SessionFingerprint(claims.SessionID)
	if _, err = pool.Exec(ctx, "insert into auth_sessions values($1,42,null)", hash); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "insert into user_presence_sessions values($1,42)", hash); err != nil {
		t.Fatal(err)
	}
	redis := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{Enabled: true, Addr: redis.Addr(), Prefix: "fixture", Namespace: "targeted-logout",
		PoolSize: 1, DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond})
	defer cache.Close()
	redis.Close()
	body, err := json.Marshal(map[string]string{"sessionMarker": browserSessionMarker(claims.SessionID)})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(context.WithValue(ctx, claimsContextKey, claims), http.MethodPost, "/api/v1/auth/logout/if-current", bytes.NewReader(body))
	out := httptest.NewRecorder()
	(&Server{db: pool, cache: cache}).logoutIfCurrent(out, req)
	var response struct {
		Code    string
		Details struct {
			Committed bool
			Operation string
		}
	}
	if err = json.Unmarshal(out.Body.Bytes(), &response); err != nil || out.Code != 503 || response.Code != "SECURITY_VERSION_REFRESH_FAILED" ||
		!response.Details.Committed || response.Details.Operation != "logout" || len(out.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("committed cache failure did not retain its status, details and cookie boundary")
	}
	var revoked, present bool
	if err = pool.QueryRow(ctx, "select revoked_at is not null, exists(select 1 from user_presence_sessions where session_hash=$1) from auth_sessions where session_hash=$1", hash).Scan(&revoked, &present); err != nil || !revoked || present {
		t.Fatalf("committed facts revoked=%t present=%t error=%v", revoked, present, err)
	}
}
