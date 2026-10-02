package httpapi

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSeedCrawlerPaginationRejectsOffsetsInvalidFiltersAndUnscopedCursors(t *testing.T) {
	for name, values := range map[string]url.Values{
		"run offset":       {"offset": {"1"}},
		"run page":         {"page": {"2"}},
		"run zero limit":   {"limit": {"0"}},
		"run excess limit": {"limit": {"101"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSeedCrawlerRunPageRequest(values); err == nil {
				t.Fatal("invalid run page request was accepted")
			}
		})
	}
	if _, err := parseSeedCrawlerCandidatePageRequest(url.Values{"status": {"unknown"}}); err == nil {
		t.Fatal("unknown candidate status was accepted")
	}
	if submitting, err := parseSeedCrawlerCandidatePageRequest(url.Values{"status": {"submitting"}}); err != nil || submitting.Status != "submitting" {
		t.Fatalf("recoverable submitting status was rejected: request=%#v err=%v", submitting, err)
	}

	first, err := parseSeedCrawlerRunPageRequest(url.Values{"limit": {"7"}})
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeSeedCrawlerRunPageCursor(seedCrawlerRunPageCursor{
		Version: seedCrawlerCursorVersion, Scope: first.Scope, CreatedAt: time.Now().UTC(), ID: 42,
	})
	if _, err = parseSeedCrawlerRunPageRequest(url.Values{"limit": {"8"}, "cursor": {cursor}}); err == nil {
		t.Fatal("run cursor was reusable with a different page size")
	}

	candidate, err := parseSeedCrawlerCandidatePageRequest(url.Values{"status": {"failed"}, "limit": {"9"}})
	if err != nil {
		t.Fatal(err)
	}
	candidateCursor := encodeSeedCrawlerCandidatePageCursor(seedCrawlerCandidatePageCursor{
		Version: seedCrawlerCursorVersion, Scope: candidate.Scope, Downloads: 10, ID: 9,
	})
	if _, err = parseSeedCrawlerCandidatePageRequest(url.Values{"status": {"draft"}, "limit": {"9"}, "cursor": {candidateCursor}}); err == nil {
		t.Fatal("candidate cursor was reusable with a different status filter")
	}
	unknownField := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"` + candidate.Scope + `","downloads":10,"id":9,"extra":true}`))
	if _, err = parseSeedCrawlerCandidatePageRequest(url.Values{"status": {"failed"}, "limit": {"9"}, "cursor": {unknownField}}); err == nil {
		t.Fatal("candidate cursor with an unknown field was accepted")
	}
}

func TestSeedCrawlerPageSQLUsesExclusiveStableKeysetsAndSummaryProjection(t *testing.T) {
	runRequest, err := parseSeedCrawlerRunPageRequest(url.Values{"limit": {"12"}})
	if err != nil {
		t.Fatal(err)
	}
	runRequest.Cursor = &seedCrawlerRunPageCursor{CreatedAt: time.Now().UTC(), ID: 77}
	runSQL, runArgs := seedCrawlerRunPageSQL(runRequest)
	if !strings.Contains(runSQL, "(created_at,id)<($1,$2)") || !strings.Contains(runSQL, "limit $3") || len(runArgs) != 3 || runArgs[2] != 13 {
		t.Fatalf("unexpected run keyset query %q args=%v", runSQL, runArgs)
	}

	candidateRequest, err := parseSeedCrawlerCandidatePageRequest(url.Values{"status": {"failed"}, "limit": {"15"}})
	if err != nil {
		t.Fatal(err)
	}
	candidateRequest.Cursor = &seedCrawlerCandidatePageCursor{Downloads: 1000, ID: 88}
	candidateSQL, candidateArgs := seedCrawlerCandidatePageSQL(candidateRequest)
	for _, required := range []string{
		"join seed_crawler_runs first_seen", "join seed_crawler_runs last_seen",
		"left join lateral", "order by value.updated_at desc,value.id desc limit 1",
		"(candidate.downloads,candidate.id)<($2,$3)", "limit $4",
	} {
		if !strings.Contains(candidateSQL, required) {
			t.Errorf("candidate query is missing %q: %s", required, candidateSQL)
		}
	}
	if strings.Contains(candidateSQL, "candidate.payload") {
		t.Fatal("candidate summary query includes its payload")
	}
	if len(candidateArgs) != 4 || candidateArgs[0] != "failed" || candidateArgs[3] != 16 {
		t.Fatalf("unexpected candidate args %v", candidateArgs)
	}
}
