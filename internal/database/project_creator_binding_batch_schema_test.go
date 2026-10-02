package database

import (
	"strings"
	"testing"
)

func TestProjectCreatorBindingSideEffectsAreTransactionAndStatementCoalesced(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168", schemaGeneration)
	}

	accessSchema := strings.ToLower(strings.Join(projectAccessSchemaStatements(), "\n"))
	for _, required := range []string{
		"current_setting('mcmods.project_acl_runtime_bumped',true)='1'",
		"set_config('mcmods.project_acl_runtime_bumped','1',true)",
		"trg_content_creator_bindings_project_acl",
		"for each statement execute function bump_project_acl_runtime_version()",
	} {
		if !strings.Contains(accessSchema, required) {
			t.Errorf("project ACL coalescing schema is missing %q", required)
		}
	}

	searchSchema := strings.ToLower(strings.Join(searchSchemaStatements(), "\n"))
	for _, required := range []string{
		"enqueue_search_creator_bindings_statement()",
		"trg_search_creator_bindings_insert",
		"referencing new table as creator_binding_new for each statement",
		"trg_search_creator_bindings_update",
		"referencing old table as creator_binding_old new table as creator_binding_new",
		"trg_search_creator_bindings_delete",
		"referencing old table as creator_binding_old for each statement",
		"select distinct case when binding.subject_type='mod' then 'mod'",
	} {
		if !strings.Contains(searchSchema, required) {
			t.Errorf("creator-binding search coalescing schema is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"enqueue_search_creator_binding()",
		"trg_search_creator_bindings after insert or update or delete",
	} {
		if strings.Contains(searchSchema, forbidden) {
			t.Errorf("creator-binding search schema retained row-level path %q", forbidden)
		}
	}
}
