package database

import (
	"strings"
	"testing"
)

func TestDraftSchemaHasIndependentPayloadBudget(t *testing.T) {
	definition := strings.ToLower(strings.Join(draftSchemaStatements(), "\n"))
	if !strings.Contains(definition, "check(octet_length(payload::text) <= 524288)") {
		t.Fatal("draft payload database ceiling is missing")
	}
}

func TestDraftSchemaRequiresOneCompletionAuthorityAndPagedIndexes(t *testing.T) {
	definition := strings.ToLower(strings.Join(draftSchemaStatements(), "\n"))
	for _, required := range []string{
		"change_request_id is not null and review_target_type='' and review_target_id is null",
		"change_request_id is null and review_target_type='server' and review_target_id is not null",
		"where submitted_at is null",
		"where submitted_at is not null",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("draft authority/page schema is missing %q", required)
		}
	}
}
