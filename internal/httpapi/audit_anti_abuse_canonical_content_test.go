package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"mcmods-cn-backend/internal/antiabuse"
)

func TestAuditAntiAbuseCanonicalObjectOrderPreservesArrayOrder(t *testing.T) {
	const left = `{"summary":"root summary gamma","localizations":[{"summary":"first summary delta","name":"first name beta"},{"name":"second name epsilon","summary":"second summary zeta"}],"bodyMarkdown":"root body alpha","ignored":"not content"}`
	const right = `{"ignored":"not content","bodyMarkdown":"root body alpha","localizations":[{"name":"first name beta","summary":"first summary delta"},{"summary":"second summary zeta","name":"second name epsilon"}],"summary":"root summary gamma"}`
	want := "root body alpha\nfirst name beta\nfirst summary delta\nsecond name epsilon\nsecond summary zeta\nroot summary gamma"
	for iteration := range 100 {
		for _, input := range []string{left, right} {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/mods", strings.NewReader(input))
			r.Header.Set("Content-Type", "application/json")
			raw, content := inspectAntiAbuseBody(r)
			if !bytes.Equal(raw, []byte(input)) {
				t.Fatal("body bytes changed during inspection")
			}
			if content != want {
				t.Fatalf("iteration=%d unstable content order: got=%q want=%q", iteration, content, want)
			}
		}
	}
}

func TestAuditAntiAbuseCanonicalConcurrentFingerprintsAgree(t *testing.T) {
	const expected = "Detailed body distinct from summary\nEnglish local name\nEnglish local summary\nShort summary distinct from body"
	want := antiabuse.ContentHash(antiabuse.NormalizeContent(expected))
	errors := make(chan string, 64)
	var workers sync.WaitGroup
	for worker := range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 20 {
				input := `{"summary":"Short summary distinct from body","localizations":[{"summary":"English local summary","name":"English local name"}],"bodyMarkdown":"Detailed body distinct from summary"}`
				if worker%2 == 0 {
					input = `{"bodyMarkdown":"Detailed body distinct from summary","localizations":[{"name":"English local name","summary":"English local summary"}],"summary":"Short summary distinct from body"}`
				}
				r := httptest.NewRequest(http.MethodPost, "/api/v1/mods", strings.NewReader(input))
				r.Header.Set("Content-Type", "application/json")
				_, content := inspectAntiAbuseBody(r)
				if antiabuse.ContentHash(antiabuse.NormalizeContent(content)) != want {
					errors <- fmt.Sprintf("worker=%d inconsistent canonical hash", worker)
					return
				}
			}
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestAuditAntiAbuseCanonicalCollectionKeepsExistingBudgets(t *testing.T) {
	// Object order is deterministic, but the existing array order, sixteen
	// collected-field limit and twenty-thousand-rune per-field limit remain.
	values := make([]any, 20)
	for i := range values {
		values[i] = map[string]any{"name": fmt.Sprintf("entry-%02d", i)}
	}
	var parts []string
	collectContentStrings(map[string]any{"items": values, "summary": "past the field budget"}, "", &parts)
	if len(parts) != 16 || parts[0] != "entry-00" || parts[15] != "entry-15" {
		t.Fatalf("collection budget/order changed: %v", parts)
	}
	parts = nil
	collectContentStrings(map[string]any{"name": strings.Repeat("界", 20001), "summary": "different second field"}, "", &parts)
	if len(parts) != 2 || utf8.RuneCountInString(parts[0]) != 20000 || parts[1] != "different second field" {
		t.Fatal("per-field budget or key ordering changed")
	}
	parts = nil
	collectContentStrings(map[string]any{"ignored": "secret", "body": nil, "message": 32, "name": ""}, "", &parts)
	if len(parts) != 0 {
		t.Fatal("unregistered or non-string content was collected")
	}
}
