package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

type oauthFailingTransport struct{}

func (oauthFailingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("deterministic OAuth transport failure")
}

func TestOAuthCallbackDoesNotLogProviderCredentialsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database to verify OAuth error logging")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table system_settings(key text primary key,value jsonb not null)`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-test-only-settings-key-32"}}
	const secret, code = "synthetic-client-secret", "synthetic-authorization-code"
	raw, err := server.sealSystemSetting(oauthConfigPayload{Providers: map[string]oauthProviderConfig{
		"wechat": {Enabled: true, ClientID: "synthetic-client", ClientSecret: secret, RedirectURI: "https://example.invalid/callback"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings values('auth.oauth',$1::jsonb)`, raw); err != nil {
		t.Fatal(err)
	}
	previousClient := oauthHTTPClient
	oauthHTTPClient = &http.Client{Transport: oauthFailingTransport{}}
	defer func() { oauthHTTPClient = previousClient }()
	var logged bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(previousOutput)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/wechat/callback?state=test-state&code="+url.QueryEscape(code), nil)
	request.SetPathValue("provider", "wechat")
	request.AddCookie(&http.Cookie{Name: oauthStateCookie("wechat"), Value: "test-state"})
	response := httptest.NewRecorder()
	server.oauthCallback(response, request.WithContext(ctx))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s; want 502", response.Code, response.Body.String())
	}
	if !strings.Contains(logged.String(), "provider=wechat") {
		t.Fatalf("failure was not logged: %s", logged.String())
	}
	for _, sensitive := range []string{secret, code, "secret=", "code=", "access_token="} {
		if strings.Contains(logged.String()+response.Body.String(), sensitive) {
			t.Errorf("OAuth failure disclosed synthetic credential %q", sensitive)
		}
	}
}
