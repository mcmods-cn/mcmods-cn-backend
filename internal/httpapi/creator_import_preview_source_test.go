package httpapi

import (
	"go/ast"
	"testing"
)

func TestCreatorImportPreviewCallPathHasNoPersistenceOperations(t *testing.T) {
	for _, functionName := range []string{"importCreator", "buildCreatorImportPreview"} {
		body := parsedFunctionBody(t, "creator_import_handlers.go", functionName)
		ast.Inspect(body, func(node ast.Node) bool {
			var callName string
			switch call := node.(type) {
			case *ast.CallExpr:
				switch target := call.Fun.(type) {
				case *ast.Ident:
					callName = target.Name
				case *ast.SelectorExpr:
					callName = target.Sel.Name
				}
			}
			switch callName {
			case "Begin", "Exec", "CopyFrom", "mirrorExternalCreatorAvatar", "mirrorExternalImage",
				"ensureNamedCreatorSnapshotTx", "createCreatorTx", "importCreatorTeamMembers":
				t.Errorf("%s preview path performs persistence operation %s", functionName, callName)
			}
			return true
		})
	}
}
