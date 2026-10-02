package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestProjectFileDownloadRemainsProjectScopedAndStatusAware(t *testing.T) {
	serverBytes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	handlerBytes, err := os.ReadFile("project_file_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	serverSource := string(serverBytes)
	handlerSource := string(handlerBytes)
	if strings.Contains(serverSource, "/api/v1/project-files/") {
		t.Fatal("unscoped project file route is registered")
	}
	if !strings.Contains(serverSource, `POST /api/v1/projects/{projectType}/{projectId}/files/{source}/{fileId}/download`) {
		t.Fatal("project-scoped file download route is missing")
	}
	for _, required := range []string{
		"project_file.project_type=$2", "project_file.project_internal_id=$3", "project_file.status='active'",
		"oss.status='active'", "oss.scan_status in ('clean','trusted_generated')",
	} {
		if !strings.Contains(handlerSource, required) {
			t.Errorf("project-scoped download guard is missing %q", required)
		}
	}
}
