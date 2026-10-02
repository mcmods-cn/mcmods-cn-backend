package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCommunityPostReferenceLimitReturnsStableBadRequestBeforeDatabase(t *testing.T) {
	snapshot := communityPostSnapshot{Kind: "tutorial", Category: "general", Title: "bounded", SourceLocale: "en-US", BodyMarkdown: "body"}
	for index := 0; index <= maximumCommunityPostProjectReferences; index++ {
		snapshot.Projects = append(snapshot.Projects, communityPostReference{Type: "mod", Identifier: fmt.Sprintf("mod_%d", index)})
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/community/posts", bytes.NewReader(payload))
	response := httptest.NewRecorder()
	server := &Server{}
	server.createCommunityPost(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "at most 32 project references") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCommunityPostReferenceValidationRejectsAmplificationBeforeTransaction(t *testing.T) {
	base := func() communityPostSnapshot {
		return communityPostSnapshot{Kind: "tutorial", Category: "general", Title: "bounded references", SourceLocale: "en-US", BodyMarkdown: "body"}
	}
	projects := make([]communityPostReference, 33)
	for index := range projects {
		projects[index] = communityPostReference{Type: "mod", Identifier: fmt.Sprintf("mod_%d", index)}
	}
	resources := make([]communityPostReference, 65)
	for index := range resources {
		resources[index] = communityPostReference{Kind: "minecraft.item", Identifier: fmt.Sprintf("minecraft:item_%d", index)}
	}
	for name, mutate := range map[string]func(*communityPostSnapshot){
		"too many projects":  func(snapshot *communityPostSnapshot) { snapshot.Projects = projects },
		"too many resources": func(snapshot *communityPostSnapshot) { snapshot.Resources = resources },
		"long project identifier": func(snapshot *communityPostSnapshot) {
			snapshot.Projects = []communityPostReference{{Type: "mod", Identifier: strings.Repeat("a", 129)}}
		},
		"long resource identifier": func(snapshot *communityPostSnapshot) {
			snapshot.Resources = []communityPostReference{{Kind: "minecraft.item", Identifier: strings.Repeat("a", 257)}}
		},
		"duplicate project": func(snapshot *communityPostSnapshot) {
			snapshot.Projects = []communityPostReference{{Type: "mod", Identifier: "Example_Mod"}, {Type: "mod", Identifier: "example_mod"}}
		},
		"duplicate resource": func(snapshot *communityPostSnapshot) {
			snapshot.Resources = []communityPostReference{{Kind: "minecraft.item", Identifier: "minecraft:stone"}, {Kind: "minecraft.item", Identifier: "MINECRAFT:STONE"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := base()
			mutate(&snapshot)
			if err := normalizeCommunityPostSnapshot(&snapshot); err == nil {
				t.Fatal("expected a stable validation error")
			}
		})
	}

	maximum := base()
	maximum.Projects = projects[:32]
	maximum.Resources = resources[:64]
	if err := normalizeCommunityPostSnapshot(&maximum); err != nil {
		t.Fatalf("maximum valid reference set: %v", err)
	}
}

func TestCommunityPostReferenceResolutionAndReplacementAreBatched(t *testing.T) {
	raw, err := os.ReadFile("community_post_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"maximumCommunityPostProjectReferences = 32",
		"maximumCommunityPostResourceReferences = 64",
		"loadFollowProjectTargetsWithQueryer(ctx, tx, publicIDs, claims)",
		"entity.public_id=any($1::text[])",
		"from unnest($2::text[],$3::bigint[],$4::text[],$5::integer[])",
		"returning id,display_order",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("community reference batch boundary is missing %q", required)
		}
	}
	start := strings.Index(source, "func replaceCommunityPostReferencesTx(")
	if start < 0 {
		t.Fatal("community reference replacement source boundary was not found")
	}
	end := strings.Index(source[start:], "func (s *Server) loadCommunityPost(")
	if end < 0 {
		t.Fatal("community reference replacement source boundary was not found")
	}
	replacement := source[start : start+end]
	for _, forbidden := range []string{"resolveFollowProjectTargetWithQueryer", "tx.QueryRow"} {
		if strings.Contains(replacement, forbidden) {
			t.Errorf("community reference replacement retained per-item operation %q", forbidden)
		}
	}
}
