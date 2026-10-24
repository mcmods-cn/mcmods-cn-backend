package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Downloads are uncredentialed data requests, separate from same-origin API
// calls. Neither an upstream URL nor a redirect can grant the API fixture's
// loopback exception. Keep the CDN registry exact and tied to each provider.
func providerDownloadFamily(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return "", errors.New("provider download requires a public HTTPS URL")
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return "github", nil
	case "cdn.modrinth.com":
		return "modrinth", nil
	case "edge.forgecdn.net", "mediafilez.forgecdn.net", "media.forgecdn.net":
		return "curseforge", nil
	default:
		return "", errors.New("provider download host is not an approved provider CDN")
	}
}

func newProviderDownloadHTTPClient(timeout time.Duration, rawURL string, trustedBaseURLs ...string) (*http.Client, error) {
	family, err := providerDownloadFamily(rawURL)
	policyBase := "https://provider-download.invalid"
	trustedOrigin := ""
	// Only an independently configured provider API can authorize its own exact
	// origin. In particular, rawURL is never passed as the API policy base.
	if len(trustedBaseURLs) == 1 && sameProviderOrigin(rawURL, trustedBaseURLs[0]) {
		parsed, parseErr := url.Parse(rawURL)
		if parseErr == nil && parsed.Scheme == "https" && parsed.User == nil && parsed.Fragment == "" {
			if normalized, normalizeErr := normalizeProviderBaseURL(trustedBaseURLs[0], ""); normalizeErr == nil {
				policyBase, trustedOrigin = normalized, normalized
				family, err = "configured", nil
			}
		}
	}
	if err != nil {
		return nil, err
	}
	// The independently selected policy base controls the production DNS/IP
	// guard. The default never enables a local-address exception.
	client, err := newProviderHTTPClient(timeout, policyBase)
	if err != nil {
		return nil, err
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many provider download redirects")
		}
		nextFamily, err := providerDownloadFamily(request.URL.String())
		if trustedOrigin != "" && sameProviderOrigin(request.URL.String(), trustedOrigin) && request.URL.Fragment == "" {
			nextFamily, err = "configured", nil
		}
		if err != nil {
			return err
		}
		if nextFamily != family {
			return errors.New("provider download redirect changed provider")
		}
		request.Header.Del("Authorization")
		request.Header.Del("Proxy-Authorization")
		request.Header.Del("x-api-key")
		request.Header.Del("Cookie")
		return nil
	}
	return client, nil
}

func providerDownloadTrustedBase(source string, cfg modImportConfig) string {
	switch source {
	case "modrinth":
		return cfg.Modrinth.BaseURL
	case "curseforge":
		return cfg.CurseForge.BaseURL
	case "github":
		return cfg.GitHub.BaseURL
	default:
		return ""
	}
}
