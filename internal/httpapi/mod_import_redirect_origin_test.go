package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type providerOriginScriptTransport struct {
	requests []*http.Request
	location string
}

func (transport *providerOriginScriptTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, request.Clone(request.Context()))
	status, header := http.StatusOK, make(http.Header)
	if len(transport.requests) == 1 {
		status = http.StatusFound
		header.Set("Location", transport.location)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("{}")), Request: request}, nil
}

// Only transport I/O is scripted. The production client, credential binding,
// standard-library redirect/header forwarding and production policy are real.
func TestProviderRedirectBindsFullOriginBeforeCredentialForwarding(t *testing.T) {
	for _, tc := range []struct {
		name, base, location string
		allowed, credential  bool
	}{
		{"https_downgrade", "https://api.curseforge.com/v1", "http://api.curseforge.com/v1/next", false, true},
		{"https_same_origin", "https://api.curseforge.com/v1", "https://api.curseforge.com/v1/next", true, true},
		{"https_default_port", "https://api.curseforge.com/v1", "https://api.curseforge.com:443/v1/next", true, true},
		{"changed_host", "https://api.curseforge.com/v1", "https://other.invalid/v1/next", false, true},
		{"changed_port", "https://api.curseforge.com/v1", "https://api.curseforge.com:8443/v1/next", false, true},
		{"userinfo", "https://api.curseforge.com/v1", "https://injected@api.curseforge.com/v1/next", false, true},
		{"loopback_same_origin", "http://127.0.0.1:32123/v1", "http://127.0.0.1:32123/v1/next", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := newProviderHTTPClient(5*time.Second, tc.base)
			if err != nil {
				t.Fatal(err)
			}
			if transport, ok := client.Transport.(*http.Transport); ok {
				defer transport.CloseIdleConnections()
			}
			transport := &providerOriginScriptTransport{location: tc.location}
			client.Transport = transport
			request, err := http.NewRequest(http.MethodGet, tc.base+"/start", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header = providerCredentialHeaders("mcmods-test", "curseforge", tc.base, "Bearer synthetic-test-only", "synthetic-test-only")
			response, err := client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			if tc.allowed {
				if err != nil || len(transport.requests) != 2 {
					t.Fatalf("allowed same-origin redirect: requests=%d err=%v", len(transport.requests), err)
				}
				if (transport.requests[1].Header.Get("x-api-key") != "") != tc.credential {
					t.Fatal("credential binding or forwarding changed")
				}
			} else if err == nil || len(transport.requests) != 1 {
				t.Fatalf("unsafe redirect reached transport: requests=%d err=%v", len(transport.requests), err)
			}
		})
	}
}
