package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestProjectFileListingUsesBoundedPerSourcePages(t *testing.T) {
	sourceBytes, err := os.ReadFile("project_file_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, required := range []string{
		"parseProjectFilePageRequest",
		"internalProjectFilePage",
		"loadModrinthProjectFilePage",
		"loadCurseForgeProjectFilePage",
		"getProviderJSONLimited",
		"writeBoundedCatalogJSON",
		"loadModrinthProjectFileByHash",
		"loadCurseForgeProjectFileByID",
		`"hasMore"`,
		`"nextCursor"`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("project file listing is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"s.internalProjectFiles(r.Context(), project)",
		`+ "/version"`,
		"pages < 10",
		"append(modrinthFiles, curseForgeFiles...)",
		"cachedProviderProjectFiles(r.Context()",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("project file listing still contains unbounded path %q", forbidden)
		}
	}
}
