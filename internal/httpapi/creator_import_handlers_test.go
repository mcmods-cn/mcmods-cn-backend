package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseCreatorImportReference(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		raw        string
		provider   string
		kind       string
		identifier string
		wantErr    bool
	}{
		{name: "modrinth organization", raw: "https://modrinth.com/organization/mekanism", provider: "modrinth", kind: "team", identifier: "mekanism"},
		{name: "modrinth user", raw: "https://www.modrinth.com/user/pupnewfster", provider: "modrinth", kind: "author", identifier: "pupnewfster"},
		{name: "curseforge member", raw: "https://www.curseforge.com/members/bradyaidanc/projects", provider: "curseforge", kind: "author", identifier: "bradyaidanc"},
		{name: "reject arbitrary host", raw: "https://example.com/user/pupnewfster", wantErr: true},
		{name: "reject insecure link", raw: "http://modrinth.com/user/pupnewfster", wantErr: true},
		{name: "reject encoded path separator", raw: "https://modrinth.com/user/a%2Fb", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference, err := parseCreatorImportReference(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseCreatorImportReference(%q) unexpectedly succeeded", test.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCreatorImportReference(%q): %v", test.raw, err)
			}
			if reference.Provider != test.provider || reference.Kind != test.kind || reference.Identifier != test.identifier {
				t.Fatalf("unexpected reference: %#v", reference)
			}
		})
	}
}

func TestImportedCreatorRoleCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		role  string
		owner bool
		want  string
	}{
		{role: "Dev", want: "developer"},
		{role: "Maintainer", want: "maintainer"},
		{role: "Artist", want: "artist"},
		{role: "Project Lead", want: "leader"},
		{role: "Supporter", want: "contributor"},
		{role: "Ownership supporter", want: "contributor"},
		{role: strings.Repeat("developer", 1000), want: "contributor"},
		{role: "Dev", owner: true, want: "owner"},
	}
	for _, test := range tests {
		if got := importedCreatorRoleCode(test.role, test.owner); got != test.want {
			t.Errorf("importedCreatorRoleCode(%q, %t) = %q, want %q", test.role, test.owner, got, test.want)
		}
	}
}

func TestCreatorImportPreviewHasNoPersistentIdentitiesOrUnsafeAvatar(t *testing.T) {
	t.Parallel()
	preview, err := buildCreatorImportPreview(importedCreatorProfile{
		Kind: "team", Name: " Example Team ", AvatarURL: "https://tracker.example/team.png",
		Members: []importedCreatorMember{
			{Name: "Supporter", AvatarURL: "https://cdn.modrinth.com/data/supporter.webp", Role: "Supporter",
				Links: []creatorLinkPayload{{Type: "modrinth", URL: "https://modrinth.com/user/supporter"}}},
			{Name: "Maintainer", AvatarURL: "http://cdn.modrinth.com/data/maintainer.webp", Role: "Maintainer"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Name != "Example Team" || preview.AvatarURL != "" || len(preview.Members) != 2 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	if preview.Members[0].SuggestedRoleCode != "contributor" ||
		preview.Members[0].AvatarURL == "" || preview.Members[0].ProfileURL == "" {
		t.Fatalf("unsafe contributor preview: %#v", preview.Members[0])
	}
	if preview.Members[1].SuggestedRoleCode != "maintainer" || preview.Members[1].AvatarURL != "" {
		t.Fatalf("unexpected maintainer preview: %#v", preview.Members[1])
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"creatorId", "roleId", "avatarFileId", "createdMembers"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("preview leaked persistent field %q: %s", forbidden, raw)
		}
	}
}

func TestCreatorImportPreviewCannotPersistExternalAvatarURL(t *testing.T) {
	t.Parallel()
	snapshot := creatorSnapshot{Kind: "author", Name: "Preview", AvatarURL: "https://cdn.modrinth.com/data/avatar.webp"}
	if err := (&Server{}).resolveCreatorAvatarTx(context.Background(), nil, 42, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.AvatarURL != "" || snapshot.AvatarInternalID != nil {
		t.Fatalf("external preview avatar survived persistence normalization: %#v", snapshot)
	}
}

func TestCreatorIdentityLink(t *testing.T) {
	t.Parallel()
	linkType, linkURL := creatorIdentityLink([]creatorLinkPayload{
		{Type: "website", URL: "https://example.com"},
		{Type: "modrinth", URL: "https://modrinth.com/user/test"},
	})
	if linkType != "modrinth" || linkURL != "https://modrinth.com/user/test" {
		t.Fatalf("unexpected identity link: %q %q", linkType, linkURL)
	}
}

func TestExtractHTMLMetadata(t *testing.T) {
	t.Parallel()
	metadata := extractHTMLMetadata(`<html><head>
		<meta content="https://media.forgecdn.net/avatar.png" property="og:image">
		<meta name='twitter:title' content='Author &amp; profile'>
	</head></html>`)
	if metadata["og:image"] != "https://media.forgecdn.net/avatar.png" {
		t.Fatalf("unexpected og:image: %q", metadata["og:image"])
	}
	if metadata["twitter:title"] != "Author & profile" {
		t.Fatalf("unexpected twitter:title: %q", metadata["twitter:title"])
	}
}

func TestImportModrinthOrganization(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/organization/mekanism" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("User-Agent") != "mcmods-cn-test" {
			t.Errorf("unexpected user agent: %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"org-id","slug":"mekanism","name":"Mekanism","icon_url":"https://cdn.modrinth.com/data/team.webp",
			"members":[{"user":{"id":"user-id","username":"pupnewfster","name":null,"avatar_url":"https://cdn.modrinth.com/data/user.webp"},"role":"Dev","is_owner":true}]
		}`))
	}))
	defer server.Close()

	cfg := defaultModImportConfig()
	cfg.UserAgent = "mcmods-cn-test"
	cfg.Modrinth.BaseURL = server.URL + "/v2"
	profile, err := importModrinthCreator(context.Background(), server.Client(), cfg, creatorImportReference{
		Provider: "modrinth", Kind: "team", Identifier: "mekanism", URL: "https://modrinth.com/organization/mekanism",
	})
	if err != nil {
		t.Fatalf("importModrinthCreator: %v", err)
	}
	if profile.Kind != "team" || profile.Name != "Mekanism" || len(profile.Members) != 1 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	if profile.Members[0].Name != "pupnewfster" || !profile.Members[0].Owner || profile.Members[0].Role != "Dev" {
		t.Fatalf("unexpected member: %#v", profile.Members[0])
	}
}

func TestImportCurseForgeAuthor(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mods/search" || r.URL.Query().Get("searchFilter") != "bradyaidanc" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("CurseForge API key leaked to custom origin: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"authors":[{
			"name":"bradyaidanc","url":"https://www.curseforge.com/members/bradyaidanc/projects","avatarUrl":"https://media.forgecdn.net/avatar.png"
		}]}]}`))
	}))
	defer server.Close()

	cfg := defaultModImportConfig()
	cfg.CurseForge.BaseURL = server.URL + "/v1"
	cfg.CurseForge.APIKey = "test-key"
	profile, err := importCurseForgeAuthor(context.Background(), server.Client(), cfg, creatorImportReference{
		Provider: "curseforge", Kind: "author", Identifier: "bradyaidanc", URL: "https://www.curseforge.com/members/bradyaidanc/projects",
	})
	if err != nil {
		t.Fatalf("importCurseForgeAuthor: %v", err)
	}
	if profile.Kind != "author" || profile.Name != "bradyaidanc" || profile.AvatarURL != "https://media.forgecdn.net/avatar.png" {
		t.Fatalf("unexpected profile: %#v", profile)
	}
}
