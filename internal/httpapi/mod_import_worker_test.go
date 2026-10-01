package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func TestModMetadataImportDoesNotAcknowledgeDatabaseFailure(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://fixture:fixture@127.0.0.1:1/fixture?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	if err := server.runModMetadataImport(ctx, "fixture-job"); err == nil {
		t.Fatal("database claim failure was acknowledged as a successful delivery")
	}
}

func TestProviderClientRejectsProtocolDowngrade(t *testing.T) {
	client, err := newProviderHTTPClient(time.Second, "https://provider.example")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://provider.example/project", nil)
	if err := client.CheckRedirect(request, []*http.Request{httptest.NewRequest(http.MethodGet, "https://provider.example", nil)}); err == nil {
		t.Fatal("HTTPS provider redirect was allowed to send credentials over HTTP")
	}
}

func TestProviderClientDoesNotDelegateTargetResolutionToEnvironmentProxy(t *testing.T) {
	client, err := newProviderHTTPClient(time.Second, "https://provider.example")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("provider transport is not inspectable")
	}
	if transport.Proxy != nil {
		t.Fatal("implicit environment proxy bypasses the provider target DNS/IP fence")
	}
}

func TestProviderClientRejectsUnsafeURLStructure(t *testing.T) {
	for _, endpoint := range []string{"ftp://provider.example/file", "https://synthetic:fixture@provider.example/file", "https://provider.example/file#fragment"} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := newProviderHTTPClient(time.Second, endpoint); err == nil {
				t.Fatal("unsafe provider URL was accepted")
			}
		})
	}
	// Signed download URLs may contain queries. API base URL configuration
	// separately rejects queries in normalizeProviderBaseURL.
	if _, err := newProviderHTTPClient(time.Second, "https://provider.example/file?signature=synthetic"); err != nil {
		t.Fatalf("signed provider file URL rejected: %v", err)
	}
}
