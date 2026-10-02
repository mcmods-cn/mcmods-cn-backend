package httpapi

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestRelationshipSyncUsesLockedDifferentialUpdates(t *testing.T) {
	tests := []struct {
		fileName      string
		functionNames []string
		tableName     string
		required      []string
	}{
		{
			"project_authorship_handlers.go",
			[]string{"syncProjectCreatorBindingsTx", "applyProjectCreatorBindingMutationsTx"},
			"content_creator_bindings",
			[]string{"insert into content_creator_bindings", "on conflict", "unnest(", "is distinct from", "revoked"},
		},
		{
			"creator_mutation_handlers.go",
			[]string{"replaceCreatorTeamMembersTx"},
			"creator_team_members",
			[]string{"for update", "update creator_team_members", "status='revoked'", "insert into creator_team_members"},
		},
	}
	for _, test := range tests {
		t.Run(test.tableName, func(t *testing.T) {
			var source strings.Builder
			for _, functionName := range test.functionNames {
				body := parsedFunctionBody(t, test.fileName, functionName)
				ast.Inspect(body, func(node ast.Node) bool {
					literal, ok := node.(*ast.BasicLit)
					if ok && literal.Kind == token.STRING {
						value, err := strconv.Unquote(literal.Value)
						if err == nil {
							source.WriteString(strings.ToLower(value))
							source.WriteByte('\n')
						}
					}
					return true
				})
			}
			queries := source.String()
			for _, required := range test.required {
				if !strings.Contains(queries, required) {
					t.Errorf("%v does not contain differential-sync operation %q", test.functionNames, required)
				}
			}
			if strings.Contains(queries, "delete from "+test.tableName) {
				t.Errorf("%v still rebuilds stable audit rows with DELETE", test.functionNames)
			}
		})
	}
	if !strings.Contains(strings.ToLower(projectCreatorBindingsForUpdateSQL), "for update") {
		t.Error("project creator-binding differential sync does not lock existing rows")
	}
}
