package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

func TestTEST024LogoDelegationIsOpaqueScopedExpiringAndSessionBoundIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.JWTSecret = "test024-only-jwt-signing-secret-32-bytes"
	cfg.FrontendOrigin = "https://frontend.example.test"
	cfg.Redis.Enabled, cfg.AntiAbuse.Enabled = false, false
	cfg.NATS = config.NATSConfig{}
	q := queue.New(ctx, cfg.NATS)
	defer q.Close()
	server := NewServer(ctx, cfg, pool, q, nil, nil, nil)
	defer server.cache.Close()
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			t.Error(err)
		}
	}()
	origin := httptest.NewServer(server)
	defer origin.Close()
	userID, _, token := createTEST044User(t, ctx, pool, cfg, "test024-logo", "UTC")
	grantTEST044Permissions(t, ctx, pool, userID, "admin.config.write")
	_, _, denied := createTEST044User(t, ctx, pool, cfg, "test024-denied", "UTC")
	request := func(auth, cookie, requestOrigin, method, path string, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, origin.URL+path, bytes.NewBufferString(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: cookie})
		}
		if requestOrigin != "" {
			req.Header.Set("Origin", requestOrigin)
		}
		response, err := origin.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d err=%v body=%s", method, path, response.StatusCode, want, err, raw)
		}
		if path == "/api/v1/admin/config/general/logo-upload-authorization" && want == http.StatusOK && !strings.Contains(response.Header.Get("Cache-Control"), "no-store") {
			t.Fatal("delegation response must not be cached")
		}
		return raw
	}
	path := "/api/v1/admin/config/general/logo-upload-authorization"
	request("", "", "", http.MethodPost, path, http.StatusUnauthorized)
	request("Bearer "+denied, "", "", http.MethodPost, path, http.StatusForbidden)
	request("", token, "https://foreign.example.test", http.MethodPost, path, http.StatusForbidden)
	raw := request("", token, cfg.FrontendOrigin, http.MethodPost, path, http.StatusOK)
	var result struct {
		Data struct {
			Authorization string `json:"authorization"`
			ExpiresAt     int64  `json:"expiresAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	credential := result.Data.Authorization
	if !strings.HasPrefix(credential, siteLogoCredentialScheme) || bytes.Contains(raw, []byte(token)) || result.Data.ExpiresAt <= time.Now().Unix() || result.Data.ExpiresAt > time.Now().Unix()+60 {
		t.Fatalf("invalid/exposed delegation: expiry=%d", result.Data.ExpiresAt)
	}
	encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(credential, siteLogoCredentialScheme))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := security.ParseToken(cfg.JWTSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(parent.SessionID)) || bytes.Contains(encoded, []byte(parent.PublicSubject)) || bytes.Contains(encoded, []byte(parent.Email)) {
		t.Fatal("opaque credential exposes session or user facts")
	}
	request(credential, "", "", http.MethodGet, "/api/v1/admin/config/general/logo-upload-access", http.StatusNoContent)
	request(credential, "", "", http.MethodGet, "/api/v1/auth/me", http.StatusUnauthorized)
	request("Bearer "+strings.TrimPrefix(credential, siteLogoCredentialScheme), "", "", http.MethodPut, "/api/v1/admin/config/general", http.StatusUnauthorized)
	request(credential, "", "", http.MethodPost, path, http.StatusUnauthorized) // cannot redelegate
	request(siteLogoCredentialScheme+"not-a-ciphertext", "", "", http.MethodGet, "/api/v1/admin/config/general/logo-upload-access", http.StatusUnauthorized)
	altered := append([]byte(nil), encoded...)
	altered[len(altered)/2] ^= 1
	request(siteLogoCredentialScheme+base64.RawURLEncoding.EncodeToString(altered), "", "", http.MethodGet, "/api/v1/admin/config/general/logo-upload-access", http.StatusUnauthorized)
	seal := func(purpose string, claims security.Claims) string {
		t.Helper()
		body, err := json.Marshal(siteLogoCredential{Purpose: purpose, Claims: claims})
		if err != nil {
			t.Fatal(err)
		}
		body, err = security.EncryptSetting(server.siteLogoCredentialKey(), body)
		if err != nil {
			t.Fatal(err)
		}
		return siteLogoCredentialScheme + base64.RawURLEncoding.EncodeToString(body)
	}
	parent.IssuedAt, parent.ExpiresAt = time.Now().Unix()-65, time.Now().Unix()-5
	request(seal(siteLogoCredentialPurpose, parent), "", "", http.MethodGet, "/api/v1/admin/config/general/logo-upload-access", http.StatusUnauthorized)
	parent.IssuedAt, parent.ExpiresAt = time.Now().Unix(), time.Now().Unix()+61
	request(seal(siteLogoCredentialPurpose, parent), "", "", http.MethodGet, "/api/v1/admin/config/general/logo-upload-access", http.StatusUnauthorized)
	parent.ExpiresAt = parent.IssuedAt + 60
	request(seal("other-purpose", parent), "", "", http.MethodGet, "/api/v1/admin/config/general/logo-upload-access", http.StatusUnauthorized)
	if _, err = pool.Exec(ctx, `update user_permissions set allow=false where user_id=$1 and permission_id=(select id from permissions where code='admin.config.write')`, userID); err != nil {
		t.Fatal(err)
	}
	request(credential, "", "", http.MethodGet, "/api/v1/admin/config/general/logo-upload-access", http.StatusForbidden)
	if _, err = pool.Exec(ctx, `update user_permissions set allow=true where user_id=$1 and permission_id=(select id from permissions where code='admin.config.write')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update auth_sessions set revoked_at=now() where session_hash=$1`, security.SessionFingerprint(parent.SessionID)); err != nil {
		t.Fatal(err)
	}
	request(credential, "", "", http.MethodGet, "/api/v1/admin/config/general/logo-upload-access", http.StatusUnauthorized)
}
