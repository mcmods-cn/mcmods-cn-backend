package httpapi

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditTP006ProductionArchiveEntryRejectsOwnedTrustedTLSWithoutDialOverrides(t *testing.T) {
	var privateHits, probes atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trust-probe" {
			probes.Add(1)
			_, _ = io.WriteString(w, "owned certificate probe")
			return
		}
		privateHits.Add(1)
		_, _ = io.WriteString(w, "SYNTHETIC_LOOPBACK_PRIVATE_DOCUMENT")
	}))
	t.Cleanup(target.Close)
	probe, err := target.Client().Get(target.URL + "/trust-probe")
	if err != nil {
		t.Fatal("owned TLS certificate positive probe failed")
	}
	probe.Body.Close()
	if probe.StatusCode != 200 || probe.TLS == nil || len(probe.TLS.VerifiedChains) == 0 {
		t.Fatal("owned certificate was not genuinely verified")
	}
	path := filepath.Join(t.TempDir(), "must-not-be-created.zip")
	// The exact original production archive entry point: no client, global,
	// environment, resolver, Dial, or DialTLS dependency is replaced here.
	err = downloadProviderArchive(context.Background(), target.URL+"/private-document", path, 2)
	if err == nil || err.Error() != "provider download requires a public HTTPS URL" {
		t.Fatal("random-port untrusted archive did not receive its precise pre-network policy rejection")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) || privateHits.Load() != 0 {
		t.Fatal("rejected actual archive entry wrote output or contacted its private target")
	}
	// Default-port cases isolate the host policy from the earlier port gate.
	for _, raw := range []string{"https://127.0.0.1/private-document", "https://[::1]/private-document", "https://[::ffff:127.0.0.1]/private-document", "https://10.0.0.1/private-document", "https://169.254.169.254/private-document"} {
		client, err := newProviderDownloadHTTPClient(2*time.Second, raw)
		if client != nil || err == nil || err.Error() != "provider download host is not an approved provider CDN" {
			t.Fatal("default-port private address did not fail the precise independent host policy")
		}
	}
	client, err := newProviderHTTPClient(2*time.Second, "https://api.github.com")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.DialTLSContext != nil || transport.DialTLS != nil {
		t.Fatal("production transport unexpectedly delegates its normal dial policy")
	}
	originalDial := reflect.ValueOf(transport.DialContext).Pointer()
	// Trust data on this private client only. The original production dial
	// closure is retained and actually invoked; no http.DefaultTransport edit.
	transport.TLSClientConfig = target.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSClientConfig.InsecureSkipVerify = false
	if transport.TLSClientConfig.RootCAs == nil || reflect.ValueOf(transport.DialContext).Pointer() != originalDial {
		t.Fatal("private certificate trust changed the production dial function")
	}
	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "::ffff:127.0.0.1"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		connection, dialErr := transport.DialContext(ctx, "tcp", net.JoinHostPort(host, u.Port()))
		cancel()
		if connection != nil {
			connection.Close()
		}
		if dialErr == nil || dialErr.Error() != "provider host resolves to a private network" {
			t.Fatal("original production dial did not precisely reject the owned private target")
		}
	}
	if probes.Load() != 1 || privateHits.Load() != 0 {
		t.Fatal("private target was contacted outside the explicit trusted certificate probe")
	}
	t.Log("TP006_THREE_LAYERS actualArchiveEntry=preciseNon443Policy default443PrivateHost=preciseCDNPolicy originalProductionDial=privateNetworkDenied trustedTLSProbe=verified privateTargetHits=0 outputAbsent=true noDefaultTransportOrDialOrDNSOrHostsMutation=true")
}

const auditDGR003SourceURL = "https://github.com/owned/repo/releases/download/v1/owned.jar"
const auditDGR003DestinationURL = "https://release-assets.githubusercontent.com/owned/owned.jar?download=1"

type auditDGR003TLSObservation struct {
	Origin       string
	Path         string
	QueryPresent bool
	Status       int
	VerifiedTLS  bool
}

type auditDGR003DeclaredTLSRoundTripper struct {
	targets      map[string]*url.URL
	transports   map[string]*http.Transport
	mu           sync.Mutex
	observations []auditDGR003TLSObservation
}

func (transport *auditDGR003DeclaredTLSRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	key := request.URL.String()
	target := transport.targets[key]
	if request.Method != http.MethodGet || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Fragment != "" || request.URL.RawPath != "" || target == nil || request.Body != nil && request.Body != http.NoBody || request.Host != "" && request.Host != request.URL.Host {
		return nil, errors.New("controlled TLS transport refused an undeclared request")
	}
	clone := request.Clone(request.Context())
	local := *request.URL
	local.Scheme, local.Host = target.Scheme, target.Host
	clone.URL, clone.Host = &local, ""
	response, err := transport.transports[key].RoundTrip(clone)
	if err != nil {
		return nil, errors.New("controlled TLS exchange failed")
	}
	verified := response.TLS != nil && response.TLS.HandshakeComplete && len(response.TLS.VerifiedChains) > 0
	if !verified {
		response.Body.Close()
		return nil, errors.New("controlled TLS response lacks a verified certificate chain")
	}
	transport.mu.Lock()
	transport.observations = append(transport.observations, auditDGR003TLSObservation{Origin: request.URL.Scheme + "://" + request.URL.Host, Path: request.URL.Path, QueryPresent: request.URL.RawQuery != "", Status: response.StatusCode, VerifiedTLS: verified})
	transport.mu.Unlock()
	return response, nil
}

func auditDGR003TLSMapping(t *testing.T, source, destination *httptest.Server) *auditDGR003DeclaredTLSRoundTripper {
	t.Helper()
	result := &auditDGR003DeclaredTLSRoundTripper{targets: map[string]*url.URL{}, transports: map[string]*http.Transport{}}
	for _, row := range []struct {
		official string
		server   *httptest.Server
	}{{auditDGR003SourceURL, source}, {auditDGR003DestinationURL, destination}} {
		local, err := url.Parse(row.server.URL)
		if err != nil {
			t.Fatal("invalid owned TLS fixture origin")
		}
		clientTransport := row.server.Client().Transport.(*http.Transport).Clone()
		clientTransport.Proxy = nil
		if clientTransport.TLSClientConfig == nil || clientTransport.TLSClientConfig.InsecureSkipVerify || clientTransport.TLSClientConfig.RootCAs == nil {
			t.Fatal("owned TLS mapping requires certificate verification")
		}
		t.Cleanup(clientTransport.CloseIdleConnections)
		result.targets[row.official], result.transports[row.official] = local, clientTransport
	}
	return result
}

func auditDGR003RequireNoCredentials(t *testing.T, request *http.Request) {
	t.Helper()
	for _, name := range []string{"Authorization", "Proxy-Authorization", "x-api-key", "Cookie"} {
		if request.Header.Get(name) != "" {
			t.Error("credential unexpectedly reached the uncredentialed TLS download (value omitted)")
		}
	}
}

func TestAuditDGR003DownloadBodyActualTLSRedirectBytesSizeHashesAndRefusals(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-sha1", "wrong-sha512", "wrong-declared-size", "wrong-streamed-size", "oversized-header", "cross-provider", "private-target", "downgrade", "redirect-loop"} {
		t.Run(mode, func(t *testing.T) {
			var sourceHits, destinationHits, privateHits atomic.Int32
			privateTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				privateHits.Add(1)
				_, _ = io.WriteString(w, "SYNTHETIC_PRIVATE_DOCUMENT")
			}))
			t.Cleanup(privateTarget.Close)
			destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				destinationHits.Add(1)
				auditDGR003RequireNoCredentials(t, r)
				if r.URL.Path != "/owned/owned.jar" || r.URL.RawQuery != "download=1" {
					t.Error("declared path/query did not reach the owned TLS destination")
				}
				if mode == "oversized-header" {
					w.Header().Set("Content-Length", strconv.FormatInt(projectAutomationMaxFileBytes+1, 10))
					w.WriteHeader(200)
					return
				}
				if mode == "wrong-streamed-size" {
					w.(http.Flusher).Flush()
				} else {
					w.Header().Set("Content-Length", "4")
				}
				_, _ = io.WriteString(w, "four")
			}))
			t.Cleanup(destination.Close)
			source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceHits.Add(1)
				auditDGR003RequireNoCredentials(t, r)
				location := auditDGR003DestinationURL
				switch mode {
				case "cross-provider":
					location = "https://cdn.modrinth.com/owned/owned.jar"
				case "private-target":
					location = privateTarget.URL + "/private-document"
				case "downgrade":
					location = "http://release-assets.githubusercontent.com/owned/owned.jar"
				case "redirect-loop":
					location = auditDGR003SourceURL
				}
				http.Redirect(w, r, location, http.StatusFound)
			}))
			t.Cleanup(source.Close)
			transport := auditDGR003TLSMapping(t, source, destination)
			cfg := defaultModImportConfig()
			cfg.RequestTimeoutSeconds = 5
			cfg.GitHub.Token = "synthetic-shared-token-that-the-download-must-never-use"
			payload := []byte("four")
			h1, h256, h512 := sha1.Sum(payload), sha256.Sum256(payload), sha512.Sum512(payload)
			file := providerProjectFile{Source: "github", DirectURL: auditDGR003SourceURL, FileName: "owned.jar", SizeBytes: 4, SHA1: hex.EncodeToString(h1[:]), SHA512: hex.EncodeToString(h512[:])}
			switch mode {
			case "wrong-sha1":
				file.SHA1 = strings.Repeat("0", 40)
			case "wrong-sha512":
				file.SHA512 = strings.Repeat("0", 128)
			case "wrong-declared-size", "wrong-streamed-size":
				file.SizeBytes = 5
			}
			// Only temporary output location is scoped; this is not a policy or
			// client-selection environment setting. No other process is affected.
			spoolDirectory := t.TempDir()
			t.Setenv("TMPDIR", spoolDirectory)
			factoryCalls := 0
			factory := func(timeout time.Duration, endpoint string, trusted ...string) (*http.Client, error) {
				factoryCalls++
				if endpoint != auditDGR003SourceURL || len(trusted) != 1 || trusted[0] != cfg.GitHub.BaseURL {
					return nil, errors.New("controlled download factory received undeclared authority")
				}
				client, err := newProviderDownloadHTTPClient(timeout, endpoint, trusted...)
				if err != nil {
					return nil, err
				}
				// Preserve the real policy and net/http's actual redirect sequence.
				// Only this private test's connection I/O has a declared mapping.
				if client.CheckRedirect == nil {
					return nil, errors.New("production redirect policy is missing")
				}
				client.CloseIdleConnections()
				client.Transport = transport
				return client, nil
			}
			artifact, err := downloadProviderFileWithClientFactory(context.Background(), file, cfg, factory)
			if factoryCalls != 1 {
				t.Fatal("download body did not use exactly the explicit declared dependency")
			}
			if mode == "valid" {
				if err != nil || artifact == nil {
					t.Fatal("allowed production redirect policy did not deliver the actual TLS payload")
				}
				body, readErr := io.ReadAll(io.LimitReader(artifact.File, 5))
				if readErr != nil || !bytes.Equal(body, payload) || artifact.Size != 4 || artifact.SHA256 != hex.EncodeToString(h256[:]) {
					artifact.Close()
					t.Fatal("spooled bytes/size/hash/rewind differ from the actual TLS stream")
				}
				artifact.Close()
			} else {
				if artifact != nil {
					artifact.Close()
					t.Fatal("rejected download returned a usable artifact")
				}
				want := map[string]string{"wrong-sha1": "provider SHA-1 mismatch", "wrong-sha512": "provider SHA-512 mismatch", "wrong-declared-size": "provider file size mismatch", "wrong-streamed-size": "provider file size mismatch", "oversized-header": "provider file exceeds the safe mirror limit", "cross-provider": "provider download redirect changed provider", "private-target": "provider download requires a public HTTPS URL", "downgrade": "provider download requires a public HTTPS URL", "redirect-loop": "too many provider download redirects"}[mode]
				if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "controlled TLS") || strings.Contains(err.Error(), "undeclared") {
					t.Fatalf("rejection did not come from the exact production business policy: mode=%s", mode)
				}
			}
			entries, readErr := os.ReadDir(spoolDirectory)
			if readErr != nil || len(entries) != 0 {
				t.Fatal("owned output directory retains a failed or closed spool")
			}
			wantSource, wantDestination := int32(1), int32(1)
			if mode == "cross-provider" || mode == "private-target" || mode == "downgrade" {
				wantDestination = 0
			}
			if mode == "redirect-loop" {
				wantSource, wantDestination = 10, 0
			}
			if sourceHits.Load() != wantSource || destinationHits.Load() != wantDestination || privateHits.Load() != 0 {
				t.Fatal("actual TLS request bounds/private-target zero-hit assertion failed")
			}
			transport.mu.Lock()
			observed := append([]auditDGR003TLSObservation(nil), transport.observations...)
			transport.mu.Unlock()
			if len(observed) != int(wantSource+wantDestination) {
				t.Fatal("actual verified TLS observations are incomplete")
			}
			for _, observation := range observed {
				if !observation.VerifiedTLS {
					t.Fatal("a declared HTTP response bypassed certificate verification")
				}
			}
			t.Logf("DGR003_TLS_BODY mode=%s layer=ACTUAL_DOWNLOAD_BODY_PRODUCTION_REDIRECT_CONTROLLED_TLS sourceHits=%d cdnHits=%d privateHits=0 verifiedTLSResponses=%d exactBytesSizeHashOrPolicy=true outputDirectoryEmpty=true firstRequestCredentials=false officialNetworkContacted=false realDNSClaim=false", mode, sourceHits.Load(), destinationHits.Load(), len(observed))
		})
	}
}

func TestAuditDGR003ActualClientPolicyStripsFourHeadersAcrossControlledTLSOrigins(t *testing.T) {
	var initialSawAll, finalSawNone atomic.Bool
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		none := true
		for _, name := range []string{"Authorization", "Proxy-Authorization", "x-api-key", "Cookie"} {
			none = none && r.Header.Get(name) == ""
		}
		finalSawNone.Store(none)
		_, _ = io.WriteString(w, "four")
	}))
	t.Cleanup(destination.Close)
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		all := true
		for _, name := range []string{"Authorization", "Proxy-Authorization", "x-api-key", "Cookie"} {
			all = all && r.Header.Get(name) == "synthetic-header-value-not-logged"
		}
		initialSawAll.Store(all)
		http.Redirect(w, r, auditDGR003DestinationURL, http.StatusFound)
	}))
	t.Cleanup(source.Close)
	transport := auditDGR003TLSMapping(t, source, destination)
	client, err := newProviderDownloadHTTPClient(5*time.Second, auditDGR003SourceURL)
	if err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	client.Transport = transport
	request, err := http.NewRequest(http.MethodGet, auditDGR003SourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Authorization", "Proxy-Authorization", "x-api-key", "Cookie"} {
		request.Header.Set(name, "synthetic-header-value-not-logged")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("real client policy failed its declared two-TLS-origin neighbor")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 5))
	if err != nil || response.StatusCode != 200 || string(body) != "four" || !initialSawAll.Load() || !finalSawNone.Load() {
		t.Fatal("actual cross-origin TLS delivery or four-header removal is incomplete")
	}
	t.Log("DGR003_TLS_HEADER_NEIGHBOR initialFourSyntheticHeadersSeen=true nextOriginFourHeadersAbsent=true actualBodyFourBytes=true realProductionCheckRedirect=true thisIsNotTheUncredentialedBusinessRequest=true")
}

func TestAuditDGR003DeclaredTLSDependencyRejectsUndeclaredScope(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(200) }))
	t.Cleanup(server.Close)
	transport := auditDGR003TLSMapping(t, server, server)
	for _, row := range []struct{ method, endpoint string }{{"POST", auditDGR003SourceURL}, {"GET", auditDGR003SourceURL + "?extra=1"}, {"GET", "https://user@github.com/owned/repo/releases/download/v1/owned.jar"}, {"GET", "http://github.com/owned/repo/releases/download/v1/owned.jar"}, {"GET", "https://github.com:443/owned/repo/releases/download/v1/owned.jar"}, {"GET", "https://github.com/another.jar"}, {"GET", auditDGR003DestinationURL + "&extra=1"}} {
		request, err := http.NewRequest(row.method, row.endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := transport.RoundTrip(request)
		if response != nil {
			response.Body.Close()
		}
		if err == nil || err.Error() != "controlled TLS transport refused an undeclared request" {
			t.Fatal("dependency mapping widened beyond its declared authority/method/path/query")
		}
	}
	if hits.Load() != 0 {
		t.Fatal("undeclared dependency request contacted a fixture")
	}
}
