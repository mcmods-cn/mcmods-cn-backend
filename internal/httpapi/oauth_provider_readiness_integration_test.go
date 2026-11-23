package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuthEndpointsAgreeOnProviderReadinessIntegration(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		config       oauthProviderConfig
		wantStart    int
		wantCallback int
	}{
		{name: "disabled", config: oauthProviderConfig{
			ClientID: "synthetic-client", ClientSecret: "synthetic-secret", RedirectURI: "https://example.invalid/callback",
		}, wantStart: http.StatusServiceUnavailable, wantCallback: http.StatusServiceUnavailable},
		{name: "incomplete", config: oauthProviderConfig{
			Enabled: true, ClientID: "synthetic-client", RedirectURI: "https://example.invalid/callback",
		}, wantStart: http.StatusServiceUnavailable, wantCallback: http.StatusServiceUnavailable},
		{name: "ready", config: oauthProviderConfig{
			Enabled: true, ClientID: "synthetic-client", ClientSecret: "synthetic-secret", RedirectURI: "https://example.invalid/callback",
		}, wantStart: http.StatusFound, wantCallback: http.StatusBadRequest},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, server, _ := oct02OAuthFixture(t)
			raw, err := server.sealSystemSetting(oauthConfigPayload{Providers: map[string]oauthProviderConfig{"github": scenario.config}})
			if err != nil {
				t.Fatal(err)
			}
			oct02OAuthSeed(t, ctx, server, raw)
			for _, endpoint := range []struct {
				name   string
				handle http.HandlerFunc
				want   int
			}{
				{name: "start", handle: server.oauthStart, want: scenario.wantStart},
				{name: "callback", handle: server.oauthCallback, want: scenario.wantCallback},
			} {
				// No authorization code or session cookie is needed to check the
				// availability response; the test cannot contact a provider.
				request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/github/"+endpoint.name, nil).WithContext(ctx)
				request.SetPathValue("provider", "github")
				response := httptest.NewRecorder()
				endpoint.handle.ServeHTTP(response, request)
				if response.Code != endpoint.want {
					t.Errorf("%s status=%d, want %d", endpoint.name, response.Code, endpoint.want)
				}
				if endpoint.want == http.StatusServiceUnavailable && len(response.Result().Cookies()) != 0 {
					t.Errorf("%s changed cookies for an unavailable provider", endpoint.name)
				}
			}
		})
	}
}
