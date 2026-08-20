package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestCatalogCreationEntrypointsCannotGrantProjectRoles(t *testing.T) {
	t.Parallel()
	for _, entrypoint := range []struct {
		file     string
		function string
		covers   string
	}{
		{file: "mod_handlers.go", function: "createMod", covers: "mod"},
		{file: "modpack_handlers.go", function: "createModpack", covers: "modpack"},
		{file: "simple_project_handlers.go", function: "createSimpleProject", covers: "plugin/map/resource_pack/shader_pack/datapack/addon"},
		{file: "server_catalog_handlers.go", function: "createMinecraftServer", covers: "verified server owner"},
	} {
		entrypoint := entrypoint
		t.Run(entrypoint.covers, func(t *testing.T) {
			t.Parallel()
			body := parsedFunctionBody(t, entrypoint.file, entrypoint.function)
			ast.Inspect(body, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.BasicLit:
					if value.Kind != token.STRING {
						break
					}
					literal, err := strconv.Unquote(value.Value)
					if err == nil && strings.Contains(strings.ToLower(literal), "user_role_bindings") {
						t.Fatalf("%s writes user_role_bindings directly", entrypoint.function)
					}
				}
				return true
			})
		})
	}
}

func TestLegacyProjectDeveloperApplicationRoutesAreRemoved(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "server.go"))
	if err != nil {
		t.Fatal(err)
	}
	routes := string(source)
	for _, legacy := range []string{"/mod-applications", "/mods/{siteId}/applications", "reviewModApplication"} {
		if strings.Contains(routes, legacy) {
			t.Fatalf("legacy project-developer application route %q is still registered", legacy)
		}
	}
	for _, required := range []string{
		"/api/v1/projects/{projectType}/{projectId}/editor-applications",
		"/api/v1/admin/project-editor-applications",
	} {
		if !strings.Contains(routes, required) {
			t.Fatalf("project editor application route %q is missing", required)
		}
	}
}

func TestProjectRelationshipReviewUsesTheAuthoritativePermissionAuditLog(t *testing.T) {
	body := parsedFunctionBody(t, "project_authorship_handlers.go", "reviewProjectAuthorshipRelation")
	var source strings.Builder
	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if ok && literal.Kind == token.STRING {
			value, err := strconv.Unquote(literal.Value)
			if err == nil {
				source.WriteString(value)
			}
		}
		return true
	})
	if strings.Contains(source.String(), "admin_operation_logs") {
		t.Fatal("project relationship review still writes the removed admin_operation_logs table")
	}
	if !strings.Contains(source.String(), "permission_audit_logs") {
		t.Fatal("project relationship review does not write the authoritative permission audit log")
	}
}

func parsedFunctionBody(t *testing.T, fileName, functionName string) *ast.BlockStmt {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	path := filepath.Join(filepath.Dir(currentFile), fileName)
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == functionName {
			return function.Body
		}
	}
	t.Fatalf("function %s not found in %s", functionName, fileName)
	return nil
}
