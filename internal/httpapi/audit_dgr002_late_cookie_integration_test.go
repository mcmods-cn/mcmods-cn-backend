package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

// This is a real cookie-jar transport assertion, not a JavaScript auth mock:
// HTTP Set-Cookie is applied before any client generation guard can run.
func TestAuditDGR002LateOptionalInvalidCookieCannotDeleteNewLoginHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	const password = "DGR002-cookie-test-only!"
	hash, err := security.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var account, otherPublic string
	if err = f.db.QueryRow(f.ctx, `update users set password_hash=$2 where id=$1 returning username,public_id`, f.userIDs[f.otherEditor], hash).Scan(&account, &otherPublic); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/auth/logout", nil, 200)
	captured, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	// A failing assertion must never leave this owned proxy waiting in Cleanup.
	t.Cleanup(unblock)
	oldPath := "/api/v1/mods?limit=1&auditDelayedInvalidCookie=1"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, err := url.Parse(f.origin.URL + r.URL.RequestURI())
		if err != nil {
			http.Error(w, "owned target", 500)
			return
		}
		request := r.Clone(r.Context())
		request.URL = target
		request.RequestURI = ""
		request.Host = target.Host
		response, err := http.DefaultTransport.RoundTrip(request)
		if err != nil {
			http.Error(w, "owned upstream", 502)
			return
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		if err != nil {
			http.Error(w, "owned body", 502)
			return
		}
		if r.URL.RequestURI() == oldPath {
			if response.StatusCode != 200 || response.Header.Get(authStateHeader) != authStateInvalid {
				http.Error(w, "old optional invalid response not reached", 500)
				return
			}
			close(captured)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		for name, values := range response.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(raw)
	}))
	t.Cleanup(func() { unblock(); proxy.CloseClientConnections(); proxy.Close() })
	origin, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(origin, []*http.Cookie{{Name: authSessionCookieName, Value: f.editor, Path: "/api/", HttpOnly: true}})
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	oldDone := make(chan error, 1)
	go func() {
		response, err := client.Get(proxy.URL + oldPath)
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode != 200 {
				err = io.ErrUnexpectedEOF
			}
		}
		oldDone <- err
	}()
	select {
	case <-captured:
	case <-f.ctx.Done():
		t.Fatal("old optional response did not reach deterministic gate")
	}
	post := func(path string, body any) *http.Response {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, proxy.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", f.server.cfg.FrontendOrigin)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("actual local login/logout request failed")
		}
		return response
	}
	login := post("/api/v1/auth/login", map[string]string{"account": account, "password": password})
	defer login.Body.Close()
	if login.StatusCode != 200 {
		t.Fatalf("new ordinary login status=%d", login.StatusCode)
	}
	var logged struct {
		Data struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err = json.NewDecoder(login.Body).Decode(&logged); err != nil || logged.Data.User.ID != otherPublic {
		t.Fatal("actual new login did not establish intended other account")
	}
	checkMe := func(want int) {
		t.Helper()
		response, err := client.Get(proxy.URL + "/api/v1/auth/me")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("actual current session status=%d want=%d (cookies omitted)", response.StatusCode, want)
		}
		if want == 200 {
			var body struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err = json.NewDecoder(response.Body).Decode(&body); err != nil || body.Data.ID != otherPublic {
				t.Fatal("current session identity changed")
			}
		}
	}
	checkMe(200)
	unblock()
	if err = <-oldDone; err != nil {
		t.Fatalf("old response release: %v", err)
	}
	checkMe(200)
	// Current explicit logout retains its effective cookie/session semantics.
	logout := post("/api/v1/auth/logout", nil)
	logout.Body.Close()
	if logout.StatusCode != 200 {
		t.Fatalf("explicit logout=%d", logout.StatusCode)
	}
	checkMe(401)
	t.Log("DGR002_COOKIE_POSTCONDITIONS actual revokedA optional response delayed across realB password login; B survives late headers; explicit current logout remains401")
}
