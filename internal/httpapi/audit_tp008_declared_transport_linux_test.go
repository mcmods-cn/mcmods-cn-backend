package httpapi

// This dependency exists only in opt-in tests. It verifies task-owned TLS and
// maps predeclared provider metadata requests; it makes no production DNS/TLS/
// SSRF or official provider claim and never changes a production transport.
import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type auditTP008TransportObservation struct {
	Origin            string `json:"declaredOrigin"`
	Path              string `json:"path"`
	Query             string `json:"canonicalQuery"`
	Status            int    `json:"status"`
	TLSVerified       bool   `json:"taskTLSVerified"`
	CredentialPresent bool   `json:"syntheticCredentialPresent"`
}

type auditTP008DeclaredTransport struct {
	destination *url.URL
	transport   *http.Transport
	allowed     map[string]bool
	cfKey       string
	mu          sync.Mutex
	observed    []auditTP008TransportObservation
	denied      int
}

func auditTP008RequestKey(origin, path, query string) string { return origin + path + "?" + query }

func (transport *auditTP008DeclaredTransport) refuse() (*http.Response, error) {
	transport.mu.Lock()
	transport.denied++
	transport.mu.Unlock()
	return nil, errors.New("TP008 private transport refused an undeclared metadata request")
}

func (transport *auditTP008DeclaredTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	origin := request.URL.Scheme + "://" + request.URL.Host
	parsedQuery, queryErr := url.ParseQuery(request.URL.RawQuery)
	key := auditTP008RequestKey(origin, request.URL.Path, parsedQuery.Encode())
	if queryErr != nil || request.URL.Opaque != "" || request.Header.Get("Cookie") != "" || request.Method != http.MethodGet || request.URL.User != nil || request.URL.Fragment != "" || request.URL.RawPath != "" || request.URL.ForceQuery || !transport.allowed[key] || (request.Host != "" && request.Host != request.URL.Host) {
		return transport.refuse()
	}
	// The request is still the ordinary business client's credentialed official
	// URL. Only the test dependency below rewrites its connection destination.
	cf := origin == "https://api.curseforge.com"
	if request.Header.Get("Authorization") != "" || cf && request.Header.Get("x-api-key") != transport.cfKey || !cf && request.Header.Get("x-api-key") != "" {
		return transport.refuse()
	}
	clone := request.Clone(request.Context())
	target := *request.URL
	target.Scheme, target.Host = transport.destination.Scheme, transport.destination.Host
	clone.URL, clone.Host = &target, ""
	response, err := transport.transport.RoundTrip(clone)
	if err != nil {
		return nil, errors.New("TP008 task-owned verified TLS request failed")
	}
	if response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 {
		response.Body.Close()
		return nil, errors.New("TP008 response lacks a verified task TLS chain")
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return transport.refuse()
	}
	transport.mu.Lock()
	transport.observed = append(transport.observed, auditTP008TransportObservation{Origin: origin, Path: request.URL.Path, Query: request.URL.Query().Encode(), Status: response.StatusCode, TLSVerified: true, CredentialPresent: cf})
	transport.mu.Unlock()
	return response, nil
}

func newAuditTP008DeclaredTransport(t *testing.T, server *httptest.Server, cfKey string, allowed map[string]bool) *auditTP008DeclaredTransport {
	t.Helper()
	destination, err := url.Parse(server.URL)
	if err != nil || destination.Scheme != "https" || destination.Hostname() != "127.0.0.1" {
		t.Fatal("TP008 requires an owned loopback TLS listener")
	}
	wire := server.Client().Transport.(*http.Transport).Clone()
	wire.Proxy = nil
	if wire.TLSClientConfig == nil || wire.TLSClientConfig.InsecureSkipVerify || wire.TLSClientConfig.RootCAs == nil {
		t.Fatal("TP008 controlled TLS must keep certificate verification")
	}
	copyAllowed := map[string]bool{}
	for key, value := range allowed {
		if value {
			copyAllowed[key] = true
		}
	}
	result := &auditTP008DeclaredTransport{destination: destination, transport: wire, allowed: copyAllowed, cfKey: cfKey}
	t.Cleanup(wire.CloseIdleConnections)
	return result
}

func (transport *auditTP008DeclaredTransport) factory(timeout time.Duration, baseURL string) (*http.Client, error) {
	if timeout <= 0 || timeout > 2*time.Minute || baseURL != "https://api.curseforge.com/v1" {
		return nil, errors.New("TP008 factory refused an undeclared API configuration")
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("TP008 private client refuses all redirects")
	}}, nil
}

func (transport *auditTP008DeclaredTransport) snapshot() map[string]any {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return map[string]any{"scope": "controlled credential transport / fixture subscription adapter; not production provider DNS/TLS/SSRF", "observed": append([]auditTP008TransportObservation{}, transport.observed...), "denied": transport.denied, "credentialValuesLogged": false}
}

func TestAuditTP008DeclaredCredentialTransportBindsExactQueriesAndVerifiedTLS(t *testing.T) {
	const key = "tp008-contract-synthetic-key"
	var mu sync.Mutex
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		if r.Header.Get("x-api-key") != key {
			http.Error(w, "wrong synthetic credential", 403)
			return
		}
		if r.URL.Path == "/v1/redirect" {
			w.Header().Set("Location", "/v1/categories")
			w.WriteHeader(302)
			return
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	query := url.Values{"gameId": {"432"}, "classesOnly": {"true"}}.Encode()
	origin := "https://api.curseforge.com"
	transport := newAuditTP008DeclaredTransport(t, server, key, map[string]bool{auditTP008RequestKey(origin, "/v1/categories", query): true, auditTP008RequestKey(origin, "/v1/redirect", ""): true})
	client, err := transport.factory(time.Second, "https://api.curseforge.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	send := func(target, credential string) ([]byte, error) {
		req, e := http.NewRequest(http.MethodGet, target, nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("x-api-key", credential)
		response, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer response.Body.Close()
		return io.ReadAll(response.Body)
	}
	raw, err := send(origin+"/v1/categories?"+query, key)
	if err != nil || string(raw) != `{"data":[]}` {
		t.Fatal("declared actual TLS metadata exchange failed (body omitted)")
	}
	for _, target := range []string{origin + "/v1/categories?" + query + "&extra=true", origin + "/v1/categories?gameId=433&classesOnly=true", origin + "/v1/mods/undeclared/description"} {
		if _, err = send(target, key); err == nil {
			t.Fatal("undeclared metadata request accepted")
		}
	}
	if _, err = send(origin+"/v1/categories?"+query, "wrong"); err == nil {
		t.Fatal("wrong credential reached transport")
	}
	if _, err = send(origin+"/v1/redirect", key); err == nil {
		t.Fatal("metadata redirect was followed")
	}
	if _, err = transport.factory(time.Second, "https://api.curseforge.com/v1/"); err == nil {
		t.Fatal("different undeclared API configuration accepted")
	}
	mu.Lock()
	actual := calls
	mu.Unlock()
	if actual != 2 {
		t.Fatalf("undeclared requests reached owned listener: calls=%d", actual)
	}
	snapshot := transport.snapshot()
	if strings.Contains(fmt.Sprint(snapshot), key) {
		t.Fatal("transport evidence contains credential value")
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.observed) != 1 || !transport.observed[0].TLSVerified || !transport.observed[0].CredentialPresent || transport.denied != 5 {
		t.Fatal("controlled TLS evidence did not reconcile accepted and denied exchanges")
	}
}
