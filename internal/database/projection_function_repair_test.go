package database

import (
	"strings"
	"testing"
)

func TestProjectionRestoreRejectsUnrelatedOrAppendedSQL(t *testing.T) {
	backup := ProjectionFunctionBackup{Version: 2, Database: "owned_fixture", Generation: 168, Functions: make(map[string]string), CommentTriggers: map[string]string{"trg_comments_popularity": legacyCommentPopularityTriggerSQL}}
	for _, name := range projectionFunctionNames {
		backup.Functions[name] = "CREATE OR REPLACE FUNCTION public." + name + "()\n RETURNS trigger\n LANGUAGE plpgsql\nAS $function$begin return new; end$function$\n"
	}
	if err := validateProjectionBackup(backup, backup.Database); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []struct {
		name string
		edit func(*ProjectionFunctionBackup)
	}{
		{"wrong database", func(value *ProjectionFunctionBackup) { value.Database = "another_database" }},
		{"wrong generation", func(value *ProjectionFunctionBackup) { value.Generation-- }},
		{"unknown trigger", func(value *ProjectionFunctionBackup) {
			value.CommentTriggers = map[string]string{"unrelated": "select 1"}
		}},
		{"unknown function", func(value *ProjectionFunctionBackup) { value.Functions["unrelated"] = "select 1" }},
		{"appended statement", func(value *ProjectionFunctionBackup) {
			value.Functions[projectionFunctionNames[0]] += "DROP TABLE public.users;"
		}},
		{"body delimiter", func(value *ProjectionFunctionBackup) {
			value.Functions[projectionFunctionNames[0]] = strings.Replace(value.Functions[projectionFunctionNames[0]],
				"begin return new; end", "begin return new; end$function$; select 1; $function$", 1)
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			copy := backup
			copy.Functions = make(map[string]string, len(backup.Functions))
			for name, definition := range backup.Functions {
				copy.Functions[name] = definition
			}
			mutate.edit(&copy)
			if err := validateProjectionBackup(copy, backup.Database); err == nil {
				t.Fatal("unsafe or unrelated restore backup was accepted")
			}
		})
	}
}
