package database

import (
	"strings"
	"testing"
)

func TestProjectAutoUpdateRunStatusMatchesWorkerStateMachine(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(governanceAutomationSchemaStatements(), "\n"))
	if !strings.Contains(definition, "check(status in ('pending','running','completed','dead_letter'))") {
		t.Fatal("project auto-update run status constraint does not match the worker state machine")
	}
	if strings.Contains(definition, "'completed','failed','dead_letter'") {
		t.Fatal("project auto-update run status constraint still admits unreachable failed state")
	}
}

func TestPopularitySchemaHasOneEffectiveCommenterAuthority(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(ratingSchemaStatements(), "\n"))
	for _, dead := range []string{"direct_commenters", "child_commenters"} {
		if strings.Contains(definition, dead) {
			t.Fatalf("popularity schema restored dead aggregate %q", dead)
		}
	}
	if !strings.Contains(definition, "effective_commenter_count") {
		t.Fatal("popularity schema lost the authoritative effective commenter fact")
	}
}
