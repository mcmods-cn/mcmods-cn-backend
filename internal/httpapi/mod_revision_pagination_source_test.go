package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestModRevisionHistoryUsesBoundedKeysetPageAndJoinedReviewFacts(t *testing.T) {
	handlerSource, err := os.ReadFile("mod_revision_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	paginationSource, err := os.ReadFile("mod_revision_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(handlerSource) + string(paginationSource))
	for _, required := range []string{
		"parsemodrevisionhistorypagerequest",
		"request.limit+1",
		`"hasmore"`,
		`"nextcursor"`,
		"left join lateral",
		"left join users submitter",
		"left join users reviewer",
		"left join content_revisions base",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Mod revision history is missing %q", required)
		}
	}
	for _, obsolete := range []string{
		"select account.public_id from users",
		"select event.note from review_events",
		"select event.created_at from review_events",
		"select base.public_id from content_revisions",
	} {
		if strings.Contains(text, obsolete) {
			t.Errorf("Mod revision query retains correlated lookup %q", obsolete)
		}
	}
}
