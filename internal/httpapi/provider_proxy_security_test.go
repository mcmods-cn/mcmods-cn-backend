package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderClientDoesNotDelegateOriginValidationToEnvironmentProxy(t *testing.T) {
	var proxyRequests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		_, _ = io.WriteString(w, "proxy bypass")
	}))
	defer proxy.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "validated origin")
	}))
	defer origin.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	defaultTransport := previous.(*http.Transport).Clone()
	defaultTransport.Proxy = http.ProxyURL(proxyURL)
	http.DefaultTransport = defaultTransport
	defer func() {
		http.DefaultTransport = previous
		defaultTransport.CloseIdleConnections()
	}()
	client, err := newProviderHTTPClient(2*time.Second, origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "validated origin" || proxyRequests.Load() != 0 {
		t.Fatal("provider request delegated origin resolution to the environment proxy")
	}
}

func TestProviderAddressRejectsMappedPrivateAndSharedNetwork(t *testing.T) {
	for _, raw := range []string{"::ffff:127.0.0.1", "10.1.2.3", "100.100.100.200", "100.64.1.2", "0.0.0.0", "ff02::1"} {
		if !isPrivateProviderAddress(netip.MustParseAddr(raw)) {
			t.Errorf("unsafe provider address accepted: %s", raw)
		}
	}
}
