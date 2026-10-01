package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type oauthSecurityTransport func(*http.Request) (*http.Response, error)

func (transport oauthSecurityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestGitHubOAuthRejectsMissingProviderIdentity(t *testing.T) {
	previous := oauthHTTPClient
	t.Cleanup(func() { oauthHTTPClient = previous })
	for _, body := range []string{`{"login":"attacker"}`, `{"id":0,"login":"attacker"}`, `{"id":-1}`} {
		t.Run(body, func(t *testing.T) {
			oauthHTTPClient = &http.Client{Transport: oauthSecurityTransport(func(request *http.Request) (*http.Response, error) {
				responseBody := `{"access_token":"synthetic-test-token"}`
				if request.URL.Path == "/user" {
					responseBody = body
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(responseBody)), Header: make(http.Header)}, nil
			})}
			if _, err := fetchGitHubProfile(context.Background(), oauthProviderConfig{}, "synthetic-code"); err == nil {
				t.Fatal("OAuth accepted a response without a positive provider user ID")
			}
		})
	}
}

func TestOAuthProviderResponsesAreBoundedAndDoNotEchoErrorBody(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "oversized", status: http.StatusOK, body: strings.Repeat("x", maxOAuthResponseBytes+1)},
		{name: "provider error", status: http.StatusUnauthorized, body: "synthetic-secret-from-provider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}
			raw, err := readOAuthResponseBody(response)
			if err == nil || raw != nil {
				t.Fatalf("invalid OAuth response was accepted: bytes=%d err=%v", len(raw), err)
			}
			if strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("OAuth error exposed the provider response body")
			}
		})
	}
}
