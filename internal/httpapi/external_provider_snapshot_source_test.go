package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestExternalProviderHTTPFlowsHaveOneSharedAuthority(t *testing.T) {
	authorityRaw, err := os.ReadFile("external_provider_snapshot.go")
	if err != nil {
		t.Fatal(err)
	}
	authority := string(authorityRaw)
	for literal, want := range map[string]int{
		`"/mods/search"`: 1,
		`"/description"`: 1,
		`"/team/"`:       1,
		`"/project/"`:    3,
	} {
		if count := strings.Count(authority, literal); count != want {
			t.Fatalf("shared provider authority contains %s %d times, want %d", literal, count, want)
		}
	}
	// Initial ingestion takes one shared snapshot. Reading a stored result has a
	// separate live visibility check; it must not reload private secondary data.
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, "external_provider_snapshot.go", authorityRaw, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := make(map[string]string)
	for _, declaration := range parsed.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			functions[function.Name.Name] = authority[files.Position(function.Pos()).Offset:files.Position(function.End()).Offset]
		}
	}
	if count := strings.Count(functions["loadModrinthProviderSnapshot"], `"/project/"`); count != 2 {
		t.Fatalf("initial Modrinth snapshot has %d project/version reads, want 2", count)
	}
	revalidation := functions["verifyPublicImportSource"]
	if count := strings.Count(revalidation, `"/project/"`); count != 1 {
		t.Fatalf("stored result visibility revalidation has %d project reads, want 1", count)
	}
	for _, forbidden := range []string{`"/version"`, `"/team/"`, "loadModrinthProviderSnapshot("} {
		if strings.Contains(revalidation, forbidden) {
			t.Fatalf("stored result visibility revalidation fetches secondary source data: %s", forbidden)
		}
	}

	for _, name := range []string{"mod_import_worker.go", "simple_project_import_worker.go", "modpack_import_worker.go"} {
		raw, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		source := string(raw)
		for _, forbidden := range []string{`"/mods/search"`, `"/description"`, `"/team/"`, `"/project/"`} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s retained provider HTTP flow %s", name, forbidden)
			}
		}
		if strings.Count(source, "loadModrinthProviderSnapshot(") != 1 || strings.Count(source, "loadCurseForgeProviderSnapshot(") != 1 {
			t.Fatalf("%s does not consume each shared provider snapshot exactly once", name)
		}
	}
}
