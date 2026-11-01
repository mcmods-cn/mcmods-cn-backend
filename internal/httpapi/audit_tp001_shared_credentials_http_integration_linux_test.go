package httpapi

// This is ACTUAL_HTTP_POSTGRESQL_WORKER_BODY_CONTROLLED_CREDENTIAL_TRANSPORT.
// The private Worker dependency sends real HTTP through certificate-verified
// task TLS servers. Production DNS/SSRF/redirect behavior is not substituted in
// production or claimed by this layer; its existing tests remain separate.
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type auditTP001CredentialFixture struct {
	test013Fixture
	server *Server
}

func auditTP001CredentialDDLLock(t *testing.T) func() {
	t.Helper()
	path := os.Getenv("MCMODS_AUDIT_PG_LOCK")
	if path == "" {
		return func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("cannot open owned PostgreSQL DDL lock")
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		t.Fatal("cannot acquire owned PostgreSQL DDL lock")
	}
	return func() {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
			t.Error("cannot release owned PostgreSQL DDL lock")
		}
		if err := f.Close(); err != nil {
			t.Error("cannot close owned PostgreSQL DDL lock")
		}
	}
}

func newAuditTP001CredentialFixture(t *testing.T) auditTP001CredentialFixture {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 for the owned HTTP/PostgreSQL credential regression")
	}
	cfg := config.Load()
	cfg.SettingsEncryptionKey = "tp001-owned-fixture-settings-key-at-least-32-characters"
	cfg.JWTSecret = "tp001-owned-fixture-signing-key-at-least-32-characters"
	cfg.Redis.Enabled = false
	cfg.NATS, cfg.SMTP = config.NATSConfig{}, config.SMTPConfig{}
	unlock := auditTP001CredentialDDLLock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	isolated, err := database.NewIsolatedTestSchema(ctx, cfg.DB.ConnString(), 1)
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	db := isolated.Pool
	var server *Server
	var origin *httptest.Server
	t.Cleanup(func() {
		// Stop all owned HTTP/business consumers before reacquiring the DDL lock.
		if origin != nil {
			origin.Close()
		}
		if server != nil {
			shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := server.Shutdown(shutdown); err != nil {
				t.Error(err)
			}
			server.cache.Close()
		}
		unlock := auditTP001CredentialDDLLock(t)
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		err := isolated.Close(cleanup)
		unlock()
		if err != nil {
			t.Error(err)
		}
	})
	if _, err = db.Exec(ctx, "select setval(pg_get_serial_sequence('users','id'),$1,false)", time.Now().UnixNano()%500000000+1000000000); err != nil {
		t.Fatal(err)
	}
	server = NewServer(ctx, cfg, db, nil, nil, nil, nil)
	origin = httptest.NewServer(server)
	f := auditTP001CredentialFixture{test013Fixture: test013Fixture{ctx: ctx, db: db, origin: origin, userIDs: map[string]int64{}}, server: server}
	for i, target := range []*string{&f.editor, &f.otherEditor, &f.denied} {
		id, _, token := createTEST044User(t, ctx, db, cfg, fmt.Sprintf("tp001-shared-%d-%d", i, time.Now().UnixNano()), "UTC")
		*target = token
		f.userIDs[token] = id
	}
	if _, err = db.Exec(ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=any($1::bigint[])`, []int64{f.userIDs[f.editor], f.userIDs[f.otherEditor], f.userIDs[f.denied]}); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, ctx, db, f.userIDs[token], "project.create")
	}
	t.Log("TP001_ENV fresh_owned_schema=true ordinary_creator=true antiabuse_config_unchanged=true redis_enabled=false nats_transport_exercised=false")
	return f
}

// Never print a response document or a token, including when a regression fails.
func (f auditTP001CredentialFixture) http(t *testing.T, token, method, path string, body any, want int) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal("cannot encode ordinary API request")
	}
	request, err := http.NewRequestWithContext(f.ctx, method, f.origin.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal("cannot construct ordinary API request")
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := f.origin.Client().Do(request)
	if err != nil {
		t.Fatal("owned ordinary API request failed")
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode != want {
		t.Fatalf("owned API %s %s status=%d expected=%d responseBytes=%d responseSHA256=%x (body/credentials omitted)", method, path, response.StatusCode, want, len(raw), sha256.Sum256(raw))
	}
	if want >= 400 && bytes.Contains(raw, []byte(`"data":`)) {
		t.Fatal("denied API response contains partial success data (body omitted)")
	}
	return raw
}

type auditTP001CredentialTransport struct {
	origin      string
	destination *url.URL
	allowed     map[string]bool
	transport   *http.Transport
	mu          sync.Mutex
	requests    int
	tlsVerified int
}

func (transport *auditTP001CredentialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// No DNS or hosts changes: only this explicit test dependency rewrites the
	// connection destination. Reject every request not predeclared by the case.
	if request.Method != http.MethodGet || request.URL.Scheme+"://"+request.URL.Host != transport.origin || request.URL.User != nil || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.Fragment != "" || request.URL.RawPath != "" || !transport.allowed[request.URL.Path] {
		return nil, errors.New("controlled credential transport refused an undeclared request")
	}
	clone := request.Clone(request.Context())
	target := *request.URL
	target.Scheme, target.Host = transport.destination.Scheme, transport.destination.Host
	clone.URL, clone.Host = &target, ""
	response, err := transport.transport.RoundTrip(clone)
	if err != nil {
		return nil, errors.New("controlled certificate-verified TLS exchange failed")
	}
	if response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 {
		response.Body.Close()
		return nil, errors.New("controlled TLS exchange lacks a verified certificate chain")
	}
	transport.mu.Lock()
	transport.requests++
	transport.tlsVerified++
	transport.mu.Unlock()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return nil, errors.New("controlled credential transport refuses every redirect")
	}
	return response, nil
}

func newAuditTP001CredentialTransport(t *testing.T, provider string, server *httptest.Server) *auditTP001CredentialTransport {
	t.Helper()
	origin := "https://api.github.com"
	paths := []string{"/repos/owned/source", "/repos/owned/source/readme"}
	if provider == "modrinth" {
		origin = "https://api.modrinth.com"
		paths = []string{"/v2/project/owned-source", "/v2/project/owned-source/version", "/v2/team/owned-team/members"}
	}
	destination, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("invalid owned TLS fixture origin")
	}
	clientTransport := server.Client().Transport.(*http.Transport).Clone()
	clientTransport.Proxy = nil
	if clientTransport.TLSClientConfig == nil || clientTransport.TLSClientConfig.InsecureSkipVerify || clientTransport.TLSClientConfig.RootCAs == nil {
		t.Fatal("controlled fixture must retain certificate verification")
	}
	t.Cleanup(clientTransport.CloseIdleConnections)
	allowed := make(map[string]bool, len(paths))
	for _, path := range paths {
		allowed[path] = true
	}
	return &auditTP001CredentialTransport{origin: origin, destination: destination, allowed: allowed, transport: clientTransport}
}

func TestAuditTP001ControlledCredentialTransportRejectsUndeclaredRequestsAndRedirects(t *testing.T) {
	var received atomic.Int32
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		http.Redirect(w, r, "https://api.github.com/repos/owned/source/readme", http.StatusFound)
	}))
	t.Cleanup(fixture.Close)
	transport := newAuditTP001CredentialTransport(t, "github", fixture)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, tc := range []struct{ method, endpoint string }{
		{http.MethodPost, "https://api.github.com/repos/owned/source"},
		{http.MethodGet, "https://api.github.com.evil.invalid/repos/owned/source"},
		{http.MethodGet, "https://api.github.com:443/repos/owned/source"},
		{http.MethodGet, "https://user@api.github.com/repos/owned/source"},
		{http.MethodGet, "https://api.github.com/repos/owned/source?unexpected=1"},
		{http.MethodGet, "https://api.github.com/repos/undeclared/source"},
	} {
		request, err := http.NewRequest(tc.method, tc.endpoint, nil)
		if err != nil {
			t.Fatal("cannot construct guard case")
		}
		if response, err := client.Do(request); err == nil {
			response.Body.Close()
			t.Fatal("undeclared request reached controlled transport")
		}
	}
	if received.Load() != 0 {
		t.Fatal("a rejected request reached the TLS fixture")
	}
	if response, err := client.Get("https://api.github.com/repos/owned/source"); err == nil {
		response.Body.Close()
		t.Fatal("controlled transport followed a redirect")
	}
	if received.Load() != 1 {
		t.Fatal("redirect was followed or initial verified TLS request was not observed")
	}
}

func TestAuditTP001SharedCredentialWorkerHTTPPostgresControlledTransportIntegration(t *testing.T) {
	for _, provider := range []string{"github", "modrinth"} {
		for _, mode := range []string{"private", "public", "reflecting-error"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				f := newAuditTP001CredentialFixture(t)
				secretBytes := make([]byte, 32)
				if _, err := rand.Read(secretBytes); err != nil {
					t.Fatal("cannot create synthetic shared credential")
				}
				secret := hex.EncodeToString(secretBytes)
				privateBody := "SYNTHETIC_PRIVATE_PROVIDER_DOCUMENT_" + provider
				publicBody := "Public synthetic document for " + provider + " fabric minecraft"
				wantAuthorization := secret
				if provider == "github" {
					wantAuthorization = "Bearer " + secret
				}
				var seen, secondary, badCredential atomic.Int32
				upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					seen.Add(1)
					if request.Header.Get("Authorization") != wantAuthorization || request.Header.Get("x-api-key") != "" {
						badCredential.Add(1)
						http.Error(w, "synthetic credential mismatch (values omitted)", 502)
						return
					}
					if mode == "reflecting-error" {
						http.Error(w, privateBody+" "+secret, 403)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if provider == "github" && request.URL.Path == "/repos/owned/source" {
						description := "Public synthetic metadata"
						if mode == "private" {
							description = privateBody + " " + secret
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"name": "owned-source", "full_name": "owned/source", "private": mode == "private", "description": description, "owner": map[string]string{"login": "owned"}, "license": map[string]string{"spdx_id": "MIT"}})
						return
					}
					if provider == "modrinth" && request.URL.Path == "/v2/project/owned-source" {
						status, body := "approved", publicBody
						if mode == "private" {
							status, body = "draft", privateBody+" "+secret
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned-source", "slug": "owned-source", "title": "Owned source Mod", "description": "Public synthetic metadata", "body": body, "project_type": "mod", "status": status, "team": "owned-team", "client_side": "required", "server_side": "required", "categories": []string{"utility"}, "license": map[string]string{"id": "MIT"}})
						return
					}
					secondary.Add(1)
					if provider == "github" && request.URL.Path == "/repos/owned/source/readme" {
						_, _ = io.WriteString(w, publicBody)
						return
					}
					if request.URL.Path == "/v2/project/owned-source/version" {
						_, _ = io.WriteString(w, `[{"status":"listed","loaders":["fabric"],"game_versions":["1.20.1"]}]`)
						return
					}
					if request.URL.Path == "/v2/team/owned-team/members" {
						_, _ = io.WriteString(w, `[{"role":"Owner","user":{"username":"owned","name":"Owned Author"}}]`)
						return
					}
					http.NotFound(w, request)
				}))
				t.Cleanup(upstream.Close)
				transport := newAuditTP001CredentialTransport(t, provider, upstream)
				cfg := defaultModImportConfig()
				cfg.CurseForge.Enabled = false
				cfg.RequestTimeoutSeconds = 5
				if provider == "github" {
					cfg.GitHub.Token = secret
				} else {
					cfg.Modrinth.Token = secret
				}
				if err := normalizeModImportConfig(&cfg); err != nil {
					t.Fatal("synthetic shared credential did not pass unchanged origin/settings validation")
				}
				sealed, err := f.server.sealSystemSetting(cfg)
				if err != nil || strings.Contains(sealed, secret) {
					t.Fatal("shared settings must be encrypted (credential omitted)")
				}
				if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
					t.Fatal("cannot persist encrypted owned provider settings")
				}
				sourceURL := "https://github.com/owned/source"
				if provider == "modrinth" {
					sourceURL = "https://modrinth.com/mod/owned-source"
				}
				raw := f.http(t, f.editor, http.MethodPost, "/api/v1/mod-imports", modMetadataImportRequest{Provider: provider, URL: sourceURL}, 202)
				job := decodeTEST022Data[modMetadataImportJobResponse](t, raw)
				if job.ID == "" || job.Status != "queued" || bytes.Contains(raw, []byte(secret)) {
					t.Fatal("ordinary task creation did not preserve queued identity or credential privacy")
				}
				facts := auditResumeModpackFacts(t, auditFixGRFixture{test013Fixture: f.test013Fixture, server: f.server})
				factoryCalls := 0
				factory := func(timeout time.Duration, baseURL string) (*http.Client, error) {
					factoryCalls++
					wantBase := cfg.GitHub.BaseURL
					if provider == "modrinth" {
						wantBase = cfg.Modrinth.BaseURL
					}
					if baseURL != wantBase || !providerCredentialOriginAllowed(provider, baseURL) {
						return nil, errors.New("controlled factory received an undeclared API origin")
					}
					return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
						return errors.New("controlled credential client refuses redirects")
					}}, nil
				}
				if err = f.server.runModMetadataImportWithProviderClientFactory(f.ctx, job.ID, factory); err != nil {
					t.Fatal("actual Worker business body failed to finish its database state transition")
				}
				var status, result, savedError string
				var progress int
				var resultNull, finished bool
				var creator int64
				if err = f.db.QueryRow(f.ctx, `select status,progress,result is null,coalesce(result::text,''),error,finished_at is not null,user_id from mod_metadata_import_jobs where public_id=$1`, job.ID).Scan(&status, &progress, &resultNull, &result, &savedError, &finished, &creator); err != nil {
					t.Fatal(err)
				}
				if !finished || creator != f.userIDs[f.editor] || strings.Contains(result+savedError, secret) || strings.Contains(result+savedError, privateBody) {
					t.Fatal("stored task lost creator/finality or disclosed private response/credential (values omitted)")
				}
				path := "/api/v1/mod-imports/" + job.ID
				if mode == "public" {
					var draft createModRequest
					if status != "completed" || progress != 100 || resultNull || savedError != "" || json.Unmarshal([]byte(result), &draft) != nil || draft.BodyMarkdown != publicBody || draft.PrimaryName == "" || draft.SiteID == "" || draft.DefaultLocale == "" {
						t.Fatal("credentialed public source did not persist a complete usable draft")
					}
					wantSecondary := int32(1)
					if provider == "modrinth" {
						wantSecondary = 2
					}
					if secondary.Load() != wantSecondary {
						t.Fatal("public source did not execute exactly its declared secondary requests")
					}
					// Public owner GET invokes an independent production-network source
					// verifier; do not contact that network or claim it was injected.
					// The clean252 matrix separately covers its normal HTTP public read.
				} else {
					if status != "failed" || progress != 25 || !resultNull || secondary.Load() != 0 {
						t.Fatal("nonpublic/error source escaped the pre-secondary gate or persisted a draft")
					}
					if mode == "private" && savedError != errProviderSourceNotPublic.Error() {
						t.Fatal("private source did not retain its exact business error")
					}
					if mode == "reflecting-error" && !strings.Contains(savedError, "HTTP 403: provider request failed") {
						t.Fatal("provider failure was not the precise sanitized upstream403")
					}
					raw := f.http(t, f.editor, http.MethodGet, path, nil, 200)
					returned := decodeTEST022Data[modMetadataImportJobResponse](t, raw)
					if returned.ID != job.ID || returned.Status != "failed" || returned.Progress != 25 || len(returned.Result) != 0 || returned.Error != savedError || bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte(privateBody)) {
						t.Fatal("creator task read leaked response/credential or lost the precise failed state")
					}
				}
				for _, denied := range []struct {
					token string
					code  int
				}{{f.otherEditor, 404}, {f.denied, 403}, {"", 401}} {
					raw := f.http(t, denied.token, http.MethodGet, path, nil, denied.code)
					if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte(privateBody)) || bytes.Contains(raw, []byte(publicBody)) {
						t.Fatal("denied task reader received source content or credential")
					}
				}
				// Repeat delivery must not create a second request or new side effects.
				if err = f.server.runModMetadataImportWithProviderClientFactory(f.ctx, job.ID, factory); err != nil || factoryCalls != 1 {
					t.Fatal("completed/failed task redelivery called its source twice")
				}
				auditResumeRequireFacts(t, facts, auditResumeModpackFacts(t, auditFixGRFixture{test013Fixture: f.test013Fixture, server: f.server}))
				transport.mu.Lock()
				requests, verified := transport.requests, transport.tlsVerified
				transport.mu.Unlock()
				if seen.Load() != 1+secondary.Load() || requests != int(seen.Load()) || verified != requests || badCredential.Load() != 0 {
					t.Fatal("actual credential reception or verified TLS request count is incomplete")
				}
				t.Logf("TP001_SHARED_CREDENTIAL provider=%s mode=%s layer=ACTUAL_HTTP_POSTGRESQL_WORKER_BODY_CONTROLLED_CREDENTIAL_TRANSPORT ordinaryPOST202=true actualCredentialSeen=true verifiedTLSRequests=%d secondary=%d status=%s progress=%d resultSQLNULL=%t creatorPreserved=true exactSafeError=true responseAndCredentialLeak=false zeroPublicationFacts=true repeatNoRequest=true officialNetworkContacted=false publicOwnerGETInjected=false", provider, mode, requests, secondary.Load(), status, progress, resultNull)
			})
		}
	}
}
