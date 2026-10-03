package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

const oct02OAuthSettingsKey = "synthetic-oauth-settings-encryption-key-32"

// The fault changes only this owned fixture connection's lookup path. PostgreSQL
// returns a real undefined-table error; the old handler's subsequent write still
// has a valid table, allowing the test to detect persistent secret loss.
type oct02OAuthReadFault struct {
	schema   string
	armed    atomic.Bool
	readFail atomic.Int64
}

type oct02OAuthFaultContextKey struct{}

func (fault *oct02OAuthReadFault) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "select value from system_settings") &&
		strings.Contains(data.SQL, "auth.oauth") && fault.armed.CompareAndSwap(true, false) {
		results, err := conn.PgConn().Exec(ctx, "set search_path=pg_catalog").ReadAll()
		if err == nil {
			for _, result := range results {
				if result.Err != nil {
					err = result.Err
				}
			}
		}
		if err == nil {
			return context.WithValue(ctx, oct02OAuthFaultContextKey{}, true)
		}
	}
	return ctx
}

func (fault *oct02OAuthReadFault) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if injected, _ := ctx.Value(oct02OAuthFaultContextKey{}).(bool); !injected {
		return
	}
	var databaseError *pgconn.PgError
	if errors.As(data.Err, &databaseError) && databaseError.Code == "42P01" {
		fault.readFail.Add(1)
	}
	// A failed read inside the new transaction leaves it aborted, so this SET
	// will fail until rollback restores its original transaction-local path.
	// Outside a transaction (the regression), this restores the write target.
	_, _ = conn.PgConn().Exec(ctx, "set search_path="+pgx.Identifier{fault.schema}.Sanitize()).ReadAll()
}

func oct02OAuthFixture(t *testing.T) (context.Context, *Server, *oct02OAuthReadFault) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || databaseURL == "" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 and an explicit owned MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	owner, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect explicit test database failed")
	}
	schema := "oct02_oauth_" + randomCatalogPublicID()
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err = owner.Exec(ctx, "create schema "+quotedSchema); err != nil {
		_ = owner.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, cleanupErr := owner.Exec(cleanupCtx, "drop schema "+quotedSchema+" cascade")
		if cleanupErr != nil {
			t.Errorf("drop owned OAuth fixture schema: %v", cleanupErr)
		}
		_ = owner.Close(cleanupCtx)
	})
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse explicit test database configuration failed")
	}
	fault := &oct02OAuthReadFault{schema: schema}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	poolConfig.ConnConfig.RuntimeParams["search_path"] = quotedSchema
	poolConfig.ConnConfig.Tracer = fault
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("initialize explicit test pool failed")
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `create table system_settings(
		key text primary key,value jsonb not null,updated_by bigint,updated_at timestamptz not null default now()
	)`); err != nil {
		t.Fatal(err)
	}
	return ctx, &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: oct02OAuthSettingsKey}}, fault
}

func oct02OAuthSeed(t *testing.T, ctx context.Context, server *Server, raw string) {
	t.Helper()
	if _, err := server.db.Exec(ctx, `insert into system_settings(key,value,updated_by) values('auth.oauth',$1::jsonb,9)
		on conflict(key) do update set value=excluded.value,updated_by=9`, raw); err != nil {
		t.Fatal(err)
	}
}

func oct02OAuthStored(t *testing.T, ctx context.Context, server *Server) string {
	t.Helper()
	var stored string
	if err := server.db.QueryRow(ctx, `select value::text from system_settings where key='auth.oauth'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	return stored
}

func oct02OAuthUpdate(ctx context.Context, server *Server, payload string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/oauth", strings.NewReader(payload))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 17}))
	response := httptest.NewRecorder()
	server.updateOAuthConfig(response, request)
	return response
}

func oct02OAuthRequireUnavailable(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "OAUTH_SETTINGS_UNAVAILABLE") {
		t.Fatalf("unavailable OAuth settings status=%d; want503 with stable error", response.Code)
	}
	for _, sensitive := range []string{"synthetic-existing-github-secret", "synthetic-existing-google-secret", oct02OAuthSettingsKey, "ciphertext", "nonce"} {
		if strings.Contains(response.Body.String(), sensitive) {
			t.Fatal("OAuth settings error disclosed confidential fixture data")
		}
	}
}

func TestOCT02OAuthSettingsReadFailurePreservesStoredSecretsIntegration(t *testing.T) {
	for _, faultName := range []string{"database-read", "wrong-encryption-key", "malformed-config", "null-config", "missing-providers"} {
		t.Run(faultName, func(t *testing.T) {
			ctx, server, fault := oct02OAuthFixture(t)
			initial := oauthConfigPayload{Providers: map[string]oauthProviderConfig{
				"github": {ClientID: "existing-github", ClientSecret: "synthetic-existing-github-secret"},
				"google": {ClientID: "existing-google", ClientSecret: "synthetic-existing-google-secret"},
			}}
			raw, err := server.sealSystemSetting(initial)
			if err != nil {
				t.Fatal(err)
			}
			if faultName == "malformed-config" || faultName == "null-config" || faultName == "missing-providers" {
				plaintext := []byte(`{"providers":"invalid"}`)
				if faultName == "null-config" {
					plaintext = []byte(`null`)
				} else if faultName == "missing-providers" {
					plaintext = []byte(`{}`)
				}
				sealed, sealErr := security.EncryptSetting(oct02OAuthSettingsKey, plaintext)
				if sealErr != nil {
					t.Fatal(sealErr)
				}
				raw = string(sealed)
			}
			oct02OAuthSeed(t, ctx, server, raw)
			before := oct02OAuthStored(t, ctx, server)
			if faultName == "database-read" {
				fault.armed.Store(true)
			} else if faultName == "wrong-encryption-key" {
				server.cfg.SettingsEncryptionKey = "different-synthetic-oauth-settings-key-32"
			}
			response := oct02OAuthUpdate(ctx, server, `{"providers":{"github":{"clientId":"updated-github"}}}`)
			after := oct02OAuthStored(t, ctx, server)
			if before != after {
				t.Errorf("failed OAuth config read replaced the durable encrypted settings")
			}
			var actor int64
			if err = server.db.QueryRow(ctx, `select updated_by from system_settings where key='auth.oauth'`).Scan(&actor); err != nil || actor != 9 {
				t.Errorf("failed config read changed stored actor; err=%v actor=%d", err, actor)
			}
			if faultName == "database-read" && fault.readFail.Load() != 1 {
				t.Fatalf("real PostgreSQL undefined-table read faults=%d; want1", fault.readFail.Load())
			}
			oct02OAuthRequireUnavailable(t, response)
		})
	}
}

func TestOCT02OAuthSettingsMissingAndOmittedSecretsIntegration(t *testing.T) {
	ctx, server, _ := oct02OAuthFixture(t)
	response := oct02OAuthUpdate(ctx, server, `{"providers":{"github":{"clientId":"first-github"}}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("missing-row initial save status=%d", response.Code)
	}
	initial := oauthConfigPayload{Providers: map[string]oauthProviderConfig{
		"github": {Enabled: true, ClientID: "existing-github", ClientSecret: "synthetic-existing-github-secret", RedirectURI: "https://example.invalid/github"},
		"google": {Enabled: true, ClientID: "existing-google", ClientSecret: "synthetic-existing-google-secret", RedirectURI: "https://example.invalid/google"},
	}}
	raw, err := server.sealSystemSetting(initial)
	if err != nil {
		t.Fatal(err)
	}
	oct02OAuthSeed(t, ctx, server, raw)
	response = oct02OAuthUpdate(ctx, server, `{"providers":{"github":{"enabled":true,"clientId":"updated-github","redirectUri":"https://example.invalid/github"}}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("omitted-secret update status=%d", response.Code)
	}
	var saved oauthConfigPayload
	if err = server.openSystemSetting([]byte(oct02OAuthStored(t, ctx, server)), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Providers["github"].ClientID != "updated-github" || saved.Providers["github"].ClientSecret != initial.Providers["github"].ClientSecret || saved.Providers["google"] != initial.Providers["google"] {
		t.Fatal("successful update did not preserve omitted and unrelated provider secrets")
	}
	var redacted struct {
		Data struct {
			Providers map[string]struct {
				HasSecret bool `json:"hasClientSecret"`
			} `json:"providers"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &redacted) != nil || !redacted.Data.Providers["github"].HasSecret || strings.Contains(response.Body.String(), "synthetic-existing-") {
		t.Fatal("OAuth save response did not safely redact preserved secrets")
	}
	response = oct02OAuthUpdate(ctx, server, `{"providers":{"github":{"enabled":true,"clientId":"updated-github","clientSecret":"synthetic-replacement-github-secret","redirectUri":"https://example.invalid/github"}}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("explicit-secret replacement status=%d", response.Code)
	}
	if err = server.openSystemSetting([]byte(oct02OAuthStored(t, ctx, server)), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Providers["github"].ClientSecret != "synthetic-replacement-github-secret" || saved.Providers["google"] != initial.Providers["google"] {
		t.Fatal("explicit replacement did not preserve unrelated provider configuration")
	}
}

type oct02OAuthNoRequestTransport struct{ calls atomic.Int64 }

func (transport *oct02OAuthNoRequestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	return nil, errors.New("controlled provider request must not be reached")
}

func TestOCT02OAuthSettingsReadCallersFailClosedIntegration(t *testing.T) {
	transport := &oct02OAuthNoRequestTransport{}
	previousClient := oauthHTTPClient
	oauthHTTPClient = &http.Client{Transport: transport}
	defer func() { oauthHTTPClient = previousClient }()
	for _, endpoint := range []string{"admin-overview", "login-start", "callback"} {
		for _, faultName := range []string{"database-read", "wrong-encryption-key"} {
			t.Run(endpoint+"/"+faultName, func(t *testing.T) {
				ctx, server, fault := oct02OAuthFixture(t)
				raw, err := server.sealSystemSetting(oauthConfigPayload{Providers: map[string]oauthProviderConfig{
					"github": {Enabled: true, ClientID: "existing-github", ClientSecret: "synthetic-existing-github-secret", RedirectURI: "https://example.invalid/github"},
				}})
				if err != nil {
					t.Fatal(err)
				}
				oct02OAuthSeed(t, ctx, server, raw)
				before := oct02OAuthStored(t, ctx, server)
				if faultName == "database-read" {
					fault.armed.Store(true)
				} else {
					server.cfg.SettingsEncryptionKey = "different-synthetic-oauth-settings-key-32"
				}
				request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/github/callback?state=fixture-state&code=fixture-code", nil).WithContext(ctx)
				request.SetPathValue("provider", "github")
				request.AddCookie(&http.Cookie{Name: oauthStateCookie("github"), Value: "fixture-state"})
				response := httptest.NewRecorder()
				switch endpoint {
				case "admin-overview":
					server.adminConfig(response, request)
				case "login-start":
					server.oauthStart(response, request)
				case "callback":
					server.oauthCallback(response, request)
				}
				oct02OAuthRequireUnavailable(t, response)
				if len(response.Result().Cookies()) != 0 {
					t.Fatal("unavailable OAuth configuration mutated state/session cookies")
				}
				if after := oct02OAuthStored(t, ctx, server); after != before {
					t.Fatal("read-only OAuth failure changed durable configuration")
				}
				if faultName == "database-read" && fault.readFail.Load() != 1 {
					t.Fatalf("actual database read fault count=%d; want1", fault.readFail.Load())
				}
			})
		}
	}
	if transport.calls.Load() != 0 {
		t.Fatalf("unavailable config reached controlled provider transport %d times", transport.calls.Load())
	}
}

func TestOCT02OAuthSettingsWriteFailureRollsBackAndRetriesIntegration(t *testing.T) {
	ctx, server, _ := oct02OAuthFixture(t)
	initial := oauthConfigPayload{Providers: map[string]oauthProviderConfig{
		"github": {ClientID: "existing-github", ClientSecret: "synthetic-existing-github-secret"},
		"google": {ClientID: "existing-google", ClientSecret: "synthetic-existing-google-secret"},
	}}
	raw, err := server.sealSystemSetting(initial)
	if err != nil {
		t.Fatal(err)
	}
	oct02OAuthSeed(t, ctx, server, raw)
	before := oct02OAuthStored(t, ctx, server)
	if _, err = server.db.Exec(ctx, `alter table system_settings add constraint reject_oauth_update_for_test check(updated_by<>17)`); err != nil {
		t.Fatal(err)
	}
	payload := `{"providers":{"github":{"clientId":"retry-github"}}}`
	response := oct02OAuthUpdate(ctx, server, payload)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("rejected config write status=%d; want500", response.Code)
	}
	if after := oct02OAuthStored(t, ctx, server); after != before {
		t.Fatal("rejected OAuth configuration write changed durable setting")
	}
	if _, err = server.db.Exec(ctx, `alter table system_settings drop constraint reject_oauth_update_for_test`); err != nil {
		t.Fatal(err)
	}
	response = oct02OAuthUpdate(ctx, server, payload)
	if response.Code != http.StatusOK {
		t.Fatalf("retry after write repair status=%d", response.Code)
	}
	var saved oauthConfigPayload
	if err = server.openSystemSetting([]byte(oct02OAuthStored(t, ctx, server)), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Providers["github"].ClientID != "retry-github" || saved.Providers["github"].ClientSecret != initial.Providers["github"].ClientSecret || saved.Providers["google"] != initial.Providers["google"] {
		t.Fatal("repaired retry lost omitted or unrelated provider secrets")
	}
}

func TestOCT02OAuthSettingsRejectNormalizedProviderAliasesIntegration(t *testing.T) {
	ctx, server, _ := oct02OAuthFixture(t)
	raw, err := server.sealSystemSetting(oauthConfigPayload{Providers: map[string]oauthProviderConfig{
		"google": {ClientID: "existing-google", ClientSecret: "synthetic-existing-google-secret"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	oct02OAuthSeed(t, ctx, server, raw)
	before := oct02OAuthStored(t, ctx, server)
	response := oct02OAuthUpdate(ctx, server, `{"providers":{"google":{"clientId":"one"}," GOOGLE ":{"clientId":"two"}}}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("normalized duplicate provider status=%d; want400", response.Code)
	}
	if after := oct02OAuthStored(t, ctx, server); after != before {
		t.Fatal("ambiguous provider aliases changed durable configuration")
	}
}
