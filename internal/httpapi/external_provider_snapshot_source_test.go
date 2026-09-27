package httpapi

import (
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
		`"/project/"`:    2,
	} {
		if count := strings.Count(authority, literal); count != want {
			t.Fatalf("shared provider authority contains %s %d times, want %d", literal, count, want)
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
