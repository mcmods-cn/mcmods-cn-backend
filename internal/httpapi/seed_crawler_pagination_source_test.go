package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestSeedCrawlerListsUseSummaryKeysetsAndOnDemandCandidateDetail(t *testing.T) {
	handlerRaw, err := os.ReadFile("seed_crawler_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	handler := string(handlerRaw)
	start := strings.Index(handler, "func (s *Server) adminSeedCrawlerCandidates")
	if start < 0 {
		t.Fatal("candidate list handler not found")
	}
	candidateList := handler[start:]
	if strings.Contains(candidateList, "candidate.payload") {
		t.Fatal("candidate summary list still selects full payloads")
	}
	combined := handler
	if paginationRaw, readErr := os.ReadFile("seed_crawler_pagination.go"); readErr == nil {
		combined += string(paginationRaw)
	}
	for _, required := range []string{
		"parseSeedCrawlerRunPageRequest", "parseSeedCrawlerCandidatePageRequest", "seedCrawlerCandidateDetail",
		`"hasMore"`, `"nextCursor"`, "limit $", "writeBoundedCatalogJSON",
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("seed crawler pagination is missing %q", required)
		}
	}
	schemaRaw, err := os.ReadFile("../database/governance_automation_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(schemaRaw)
	for _, index := range []string{"idx_seed_crawler_runs_created", "idx_seed_crawler_candidates_downloads", "idx_seed_crawler_candidates_status_downloads"} {
		if !strings.Contains(schema, index) {
			t.Errorf("seed crawler schema is missing %s", index)
		}
	}
}
