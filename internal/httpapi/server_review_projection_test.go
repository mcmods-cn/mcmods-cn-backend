package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestServerReviewListUsesSummaryProjectionAndOnDemandDetail(t *testing.T) {
	for _, forbidden := range []string{"server.body_markdown", "server.proof_text"} {
		if strings.Contains(strings.ToLower(minecraftServerReviewPageQuery), forbidden) {
			t.Fatalf("server review list still selects %s", forbidden)
		}
	}
	for _, required := range []string{"proof_file_count", "link_count", "mod_count"} {
		if !strings.Contains(strings.ToLower(minecraftServerReviewPageQuery), required) {
			t.Fatalf("server review summary query omitted %s", required)
		}
	}

	sourceBytes, err := os.ReadFile("server_review_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	listStart := strings.Index(source, "func (s *Server) adminMinecraftServerReviews")
	detailStart := strings.Index(source, "func (s *Server) adminMinecraftServerReviewDetail")
	if listStart < 0 || detailStart <= listStart {
		t.Fatal("server review detail handler is not a separate on-demand boundary")
	}
	listBody := source[listStart:detailStart]
	for _, forbidden := range []string{
		"minecraftServerProofFiles(", "minecraftServerLinks(", "minecraftServerMods(",
	} {
		if strings.Contains(listBody, forbidden) {
			t.Fatalf("server review list still loads per-item association through %s", forbidden)
		}
	}

	routeBytes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(routeBytes), `GET /api/v1/admin/server-reviews/{serverId}`) {
		t.Fatal("server review detail GET route is not registered")
	}
}
