package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestProviderAuthorAvatarFieldsDecode(t *testing.T) {
	var modrinthMember modrinthTeamMember
	if err := json.Unmarshal([]byte(`{"role":"Developer","user":{"username":"alice","avatar_url":"https://cdn.modrinth.com/user/alice.png"}}`), &modrinthMember); err != nil {
		t.Fatal(err)
	}
	if modrinthMember.User.AvatarURL != "https://cdn.modrinth.com/user/alice.png" {
		t.Fatalf("Modrinth avatar URL was not decoded: %#v", modrinthMember)
	}

	var curseForgeProject curseForgeMod
	if err := json.Unmarshal([]byte(`{"authors":[{"name":"bob","avatarUrl":"https://media.forgecdn.net/avatars/bob.png"}]}`), &curseForgeProject); err != nil {
		t.Fatal(err)
	}
	if len(curseForgeProject.Authors) != 1 || curseForgeProject.Authors[0].AvatarURL == "" {
		t.Fatalf("CurseForge avatar URL was not decoded: %#v", curseForgeProject.Authors)
	}

	var repository githubRepository
	if err := json.Unmarshal([]byte(`{"owner":{"login":"carol","avatar_url":"https://avatars.githubusercontent.com/u/1?v=4"}}`), &repository); err != nil {
		t.Fatal(err)
	}
	if repository.Owner.AvatarURL != "https://avatars.githubusercontent.com/u/1?v=4" {
		t.Fatalf("GitHub avatar URL was not decoded: %#v", repository.Owner)
	}
}

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
			provider, sourceURL, reference, err := parseProjectImportSource("mod", test.provider, test.input)
			if err != nil {
				t.Fatal(err)
			}
			if provider != test.provider || sourceURL != test.wantURL || reference != test.wantRef {
				t.Fatalf("parseProjectImportSource() = %q, %q, %q", provider, sourceURL, reference)
			}
		})
	}
}

func TestParseModImportSourceRejectsForeignHost(t *testing.T) {
	if _, _, _, err := parseProjectImportSource("mod", "github", "https://example.com/owner/repo"); err == nil {
		t.Fatal("expected foreign GitHub host to be rejected")
	}
	if _, _, _, err := parseProjectImportSource("mod", "modrinth", "https://github.com/mod/ferrite-core"); err == nil {
		t.Fatal("expected a URL from another provider to be rejected")
	}
	if _, _, _, err := parseProjectImportSource("mod", "github", "https://github.com:8443/owner/repo"); err == nil {
		t.Fatal("expected a non-standard provider port to be rejected")
	}
	if _, _, _, err := parseProjectImportSource("mod", "github", "https://github.com/owner/repo%20name"); err == nil {
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

func TestLoadersFromTextUsesTokenBoundariesAndKeepsNeoForgeDistinct(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   []string
	}{
		{name: "neoforge compact", values: []string{"A NeoForge mod"}, want: []string{"NeoForge"}},
		{name: "neoforge hyphen", values: []string{"neo-forge"}, want: []string{"NeoForge"}},
		{name: "neoforge words", values: []string{"neo forge loader"}, want: []string{"NeoForge"}},
		{name: "forge", values: []string{"MinecraftForge mod"}, want: []string{"Forge"}},
		{name: "both explicit", values: []string{"NeoForge and Forge builds"}, want: []string{"NeoForge", "Forge"}},
		{name: "fabric quilt", values: []string{"fabric-api", "quiltmc"}, want: []string{"Fabric", "Quilt"}},
		{name: "no substring", values: []string{"forged tools and quilted cloth"}, want: []string{}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := loadersFromText(testCase.values...); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("loadersFromText(%q) = %#v, want %#v", testCase.values, got, testCase.want)
			}
		})
	}
}

func TestModImportSecondaryMetadataFailuresAreVisible(t *testing.T) {
	failures := []struct {
		name   string
		status int
		body   string
	}{
		{name: "not found", status: http.StatusNotFound, body: `missing`},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `slow down`},
		{name: "upstream failure", status: http.StatusInternalServerError, body: `failed`},
		{name: "invalid JSON", status: http.StatusOK, body: `{`},
		{name: "oversized", status: http.StatusOK, body: strings.Repeat("x", int(maxModImportResponseBytes)+1)},
	}

	for _, failure := range failures {
		t.Run("modrinth team/"+failure.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/project/example":
					_, _ = response.Write([]byte(`{"id":"project-id","slug":"example","title":"Example","description":"Summary","body":"Body","project_type":"mod","team":"team-id","status":"approved","license":{"id":"MIT"}}`))
				case "/project/project-id/version":
					_, _ = response.Write([]byte(`[]`))
				default:
					writeProviderTestResponse(response, failure.status, failure.body)
				}
			}))
			defer server.Close()
			cfg := defaultModImportConfig()
			cfg.Modrinth.BaseURL = server.URL
			if _, err := importModrinthProject(context.Background(), server.Client(), cfg, "example"); err == nil {
				t.Fatal("team metadata failure was ignored")
			}
		})

		t.Run("curseforge description/"+failure.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				if request.URL.Path == "/mods/search" {
					_, _ = response.Write([]byte(`{"data":[{"id":34,"classId":6,"slug":"example","name":"Example","summary":"Summary","isAvailable":true}]}`))
					return
				}
				writeProviderTestResponse(response, failure.status, failure.body)
			}))
			defer server.Close()
			cfg := defaultModImportConfig()
			cfg.CurseForge.BaseURL = server.URL
			cfg.CurseForge.APIKey = ""
			if _, err := importCurseForgeProject(context.Background(), server.Client(), cfg, "example"); err == nil {
				t.Fatal("description metadata failure was ignored")
			}
		})
	}

	for _, failure := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, body: `slow down`},
		{name: "upstream failure", status: http.StatusInternalServerError, body: `failed`},
		{name: "oversized", status: http.StatusOK, body: strings.Repeat("x", int(maxModImportResponseBytes)+1)},
	} {
		t.Run("github readme/"+failure.name, func(t *testing.T) {
			server := newGitHubImportTestServer(t, failure.status, failure.body)
			defer server.Close()
			cfg := defaultModImportConfig()
			cfg.GitHub.BaseURL = server.URL
			if _, err := importGitHubRepository(context.Background(), server.Client(), cfg, "example/repository"); err == nil {
				t.Fatal("README metadata failure was ignored")
			}
		})
	}

	t.Run("github missing readme is explicit absence", func(t *testing.T) {
		server := newGitHubImportTestServer(t, http.StatusNotFound, `missing`)
		defer server.Close()
		cfg := defaultModImportConfig()
		cfg.GitHub.BaseURL = server.URL
		if _, err := importGitHubRepository(context.Background(), server.Client(), cfg, "example/repository"); err != nil {
			t.Fatalf("missing README should remain a valid empty field: %v", err)
		}
	})
}

func newGitHubImportTestServer(t *testing.T, readmeStatus int, readmeBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/repos/example/repository" {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":1,"name":"repository","full_name":"example/repository","html_url":"https://github.com/example/repository","description":"Summary","owner":{"login":"alice"},"license":{"spdx_id":"MIT"}}`))
			return
		}
		writeProviderTestResponse(response, readmeStatus, readmeBody)
	}))
}

func writeProviderTestResponse(response http.ResponseWriter, status int, body string) {
	response.WriteHeader(status)
	_, _ = response.Write([]byte(body))
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

func TestPrepareModImportConfigUpdateDoesNotCarryCredentialsAcrossOrigins(t *testing.T) {
	current := defaultModImportConfig()
	current.Modrinth.Token = "modrinth-secret"
	current.CurseForge.APIKey = "curseforge-secret"
	current.GitHub.Token = "github-secret"

	next := current
	next.Modrinth.BaseURL = "https://metadata.example/modrinth"
	next.Modrinth.Token = ""
	next.CurseForge.Enabled = false
	next.CurseForge.BaseURL = "https://metadata.example/curseforge"
	next.CurseForge.APIKey = ""
	next.GitHub.BaseURL = "https://metadata.example/github"
	next.GitHub.Token = ""

	if err := prepareModImportConfigUpdate(&next, current); err != nil {
		t.Fatal(err)
	}
	if next.Modrinth.Token != "" || next.CurseForge.APIKey != "" || next.GitHub.Token != "" {
		t.Fatalf("provider credentials crossed origins: %#v", next)
	}
}

func TestNormalizeModImportConfigRejectsCredentialsForNonOfficialOrigin(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*modImportConfig)
	}{
		{
			name: "modrinth token",
			mutate: func(cfg *modImportConfig) {
				cfg.Modrinth.BaseURL = "https://metadata.example/modrinth"
				cfg.Modrinth.Token = "secret"
			},
		},
		{
			name: "curseforge api key",
			mutate: func(cfg *modImportConfig) {
				cfg.CurseForge.BaseURL = "https://metadata.example/curseforge"
				cfg.CurseForge.APIKey = "secret"
			},
		},
		{
			name: "github token",
			mutate: func(cfg *modImportConfig) {
				cfg.GitHub.BaseURL = "https://metadata.example/github"
				cfg.GitHub.Token = "secret"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := defaultModImportConfig()
			cfg.CurseForge.Enabled = false
			test.mutate(&cfg)
			if err := normalizeModImportConfig(&cfg); err == nil {
				t.Fatal("expected a credential bound to a non-official origin to be rejected")
			}
		})
	}
}

func TestProviderCredentialHeadersFailClosedForWrongOrigin(t *testing.T) {
	headers := providerCredentialHeaders(
		"mcmods-cn/test",
		"github",
		"https://metadata.example/github",
		"Bearer github-secret",
		"curseforge-secret",
	)
	if headers.Get("Authorization") != "" || headers.Get("x-api-key") != "" {
		t.Fatalf("credentials were attached to an untrusted origin: %#v", headers)
	}

	headers = providerCredentialHeaders(
		"mcmods-cn/test",
		"github",
		"https://api.github.com",
		"Bearer github-secret",
		"",
	)
	if got := headers.Get("Authorization"); got != "Bearer github-secret" {
		t.Fatalf("official GitHub origin did not receive its credential: %q", got)
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
