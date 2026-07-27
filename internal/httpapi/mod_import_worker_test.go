package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"testing"
)

func TestParseModImportSource(t *testing.T) {
	tests := []struct {
		provider string
		input    string
		wantURL  string
		wantRef  string
	}{
		{"modrinth", "https://modrinth.com/mod/sodium/versions", "https://modrinth.com/mod/sodium", "sodium"},
		{"curseforge", "https://www.curseforge.com/minecraft/mc-mods/jei/files", "https://www.curseforge.com/minecraft/mc-mods/jei", "jei"},
		{"github", "https://github.com/FabricMC/fabric.git", "https://github.com/FabricMC/fabric", "FabricMC/fabric"},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			provider, sourceURL, reference, err := parseModImportSource(test.provider, test.input)
			if err != nil {
				t.Fatal(err)
			}
			if provider != test.provider || sourceURL != test.wantURL || reference != test.wantRef {
				t.Fatalf("parseModImportSource() = %q, %q, %q", provider, sourceURL, reference)
			}
		})
	}
}

func TestParseModImportSourceRejectsForeignHost(t *testing.T) {
	if _, _, _, err := parseModImportSource("github", "https://example.com/owner/repo"); err == nil {
		t.Fatal("expected foreign GitHub host to be rejected")
	}
	if _, _, _, err := parseModImportSource("modrinth", "https://github.com/mod/ferrite-core"); err == nil {
		t.Fatal("expected a URL from another provider to be rejected")
	}
	if _, _, _, err := parseModImportSource("github", "https://github.com:8443/owner/repo"); err == nil {
		t.Fatal("expected a non-standard provider port to be rejected")
	}
	if _, _, _, err := parseModImportSource("github", "https://github.com/owner/repo%20name"); err == nil {
		t.Fatal("expected an invalid repository name to be rejected")
	}
}

func TestEnvironmentFromSides(t *testing.T) {
	tests := map[[2]string]string{
		{"required", "unsupported"}: "clientOnly",
		{"unsupported", "required"}: "serverOnly",
		{"optional", "required"}:    "clientOptional",
		{"required", "optional"}:    "serverOptional",
		{"required", "required"}:    "bothRequired",
	}
	for input, want := range tests {
		if got := environmentFromSides(input[0], input[1]); got != want {
			t.Fatalf("environmentFromSides(%q, %q) = %q, want %q", input[0], input[1], got, want)
		}
	}
}

func TestExternalMetadataNormalization(t *testing.T) {
	if got := normalizeExternalLicense("LGPL-3.0-or-later"); got != "LGPL-3.0" {
		t.Fatalf("normalizeExternalLicense() = %q", got)
	}
	if got := htmlToMarkdown("<p>Hello<br>world</p><ul><li>One</li></ul>"); got != "Hello\n\nworld\n\n- One" {
		t.Fatalf("htmlToMarkdown() = %q", got)
	}
	if got := normalizeLoaders([]string{"fabric", "neoforge", "fabric"}); !reflect.DeepEqual(got, []string{"Fabric", "NeoForge"}) {
		t.Fatalf("normalizeLoaders() = %#v", got)
	}
}

func TestRedactModImportConfig(t *testing.T) {
	cfg := defaultModImportConfig()
	cfg.Modrinth.Token = "modrinth-secret"
	cfg.CurseForge.APIKey = "curseforge-secret"
	cfg.GitHub.Token = "github-secret"
	redacted := redactModImportConfig(cfg)
	if redacted.Modrinth.Token != "" || redacted.CurseForge.APIKey != "" || redacted.GitHub.Token != "" {
		t.Fatal("redacted config exposed a provider credential")
	}
	if !redacted.Modrinth.HasToken || !redacted.CurseForge.HasAPIKey || !redacted.GitHub.HasToken {
		t.Fatal("redacted config did not preserve configured-state flags")
	}
}

func TestNormalizeModImportConfigRequiresCurseForgeKey(t *testing.T) {
	cfg := defaultModImportConfig()
	cfg.CurseForge.Enabled = true
	cfg.CurseForge.APIKey = ""
	if err := normalizeModImportConfig(&cfg); err == nil {
		t.Fatal("expected enabled CurseForge provider without an API key to fail validation")
	}
}

func TestProviderClientRejectsCrossHostRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	client, err := newProviderHTTPClient(0, source.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Get(source.URL); err == nil {
		t.Fatal("expected redirect to a different API host to be rejected")
	}
}

func TestPrivateProviderAddressClassification(t *testing.T) {
	privateAddresses := []string{"127.0.0.1", "::1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.1.1"}
	for _, raw := range privateAddresses {
		if !isPrivateProviderAddress(netip.MustParseAddr(raw)) {
			t.Fatalf("expected %s to be classified as private", raw)
		}
	}
	if isPrivateProviderAddress(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public provider address was classified as private")
	}
}
