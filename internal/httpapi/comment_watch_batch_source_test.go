package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestMyCommentWatchesBatchAssemblesCommentsAndTargets(t *testing.T) {
	t.Parallel()
	sourceBytes, err := os.ReadFile("comment_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	detailBytes, err := os.ReadFile("comment_detail_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes) + "\n" + string(detailBytes)
	start := strings.Index(source, "func (s *Server) myCommentWatches")
	if start < 0 {
		t.Fatal("watch handler bounds missing")
	}
	end := strings.Index(source[start:], "func (s *Server) commentWatchItem")
	if end < 0 {
		t.Fatal("watch handler bounds missing")
	}
	body := source[start : start+end]
	if count := strings.Count(body, "queryCommentItems("); count != 1 {
		t.Fatalf("watch page comment assembly calls=%d want 1", count)
	}
	if !strings.Contains(body, "queryCommentTargetsByInternal") {
		t.Fatal("watch page is missing batched target resolution")
	}
	if strings.Contains(body, "resolveCommentTargetByInternal") {
		t.Fatal("watch page still resolves targets per item")
	}
	if !strings.Contains(body, "commentsByID") || !strings.Contains(body, "targetsByIdentity") {
		t.Fatal("watch page does not assemble from batch result maps")
	}
}

func TestCommentAuthorAvatarsAreResolvedOncePerUniqueStoredURL(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "comment_detail_handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "queryCommentItems" {
			continue
		}
		batchCalls := 0
		inspectCalls := func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch method.Sel.Name {
			case "resolveStoredOSSObjectAccessURLWithConfig":
				t.Error("comment assembly still invokes the per-image database resolver")
			case "resolveStoredOSSImageURLsWithConfig":
				batchCalls++
			}
			return true
		}
		ast.Inspect(function.Body, inspectCalls)
		if batchCalls != 1 {
			t.Fatalf("comment assembly batch calls=%d want exactly one", batchCalls)
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			var body *ast.BlockStmt
			switch loop := node.(type) {
			case *ast.ForStmt:
				body = loop.Body
			case *ast.RangeStmt:
				body = loop.Body
			default:
				return true
			}
			batchCalls = 0
			ast.Inspect(body, inspectCalls)
			if batchCalls != 0 {
				t.Error("comment assembly invokes the batch resolver inside an item loop")
			}
			return false
		})
		return
	}
	t.Fatal("comment assembly function missing")
}
