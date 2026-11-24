package httpapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Keep the provider URL and credential policy intact. Only the test transport
// maps its TLS connection to this test's listener, using the listener's trust root.
func serveAutomationCurseForgeFixture(t *testing.T, handler http.Handler) {
	t.Helper()
	fixture := httptest.NewTLSServer(http.StripPrefix("/v1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.curseforge.com" || r.Header.Get("X-Api-Key") != "fixture-only" {
			t.Error("provider request did not carry the expected logical authority and fixture credential")
			http.Error(w, "wrong test authority", http.StatusBadRequest)
			return
		}
		handler.ServeHTTP(w, r)
	})))
	t.Cleanup(fixture.Close)
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	// CI may configure an outbound HTTPS proxy. This fixture's TLS connection
	// belongs exclusively to its local listener.
	transport.Proxy = nil
	dialer := tls.Dialer{Config: fixture.Client().Transport.(*http.Transport).TLSClientConfig.Clone()}
	dialer.Config.ServerName = "example.com"
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "api.curseforge.com:443" {
			return nil, fmt.Errorf("unexpected TLS target in provider fixture: %s", addr)
		}
		return dialer.DialContext(ctx, network, fixture.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original; transport.CloseIdleConnections() })
}
