package database

import (
	"strings"
	"testing"
)

func TestProjectFilesDoNotRegisterUnreachablePublicRoutes(t *testing.T) {
	definition := strings.ToLower(strings.Join(projectFileSchemaStatements(), "\n"))
	for _, unreachable := range []string{
		"register_project_file_public_route",
		"trg_project_files_public_route",
		"remove_project_file_public_route",
		"trg_project_files_remove_public_route",
		"/api/v1/project-files/",
		"'project_file',new.id",
	} {
		if strings.Contains(definition, unreachable) {
			t.Errorf("unreachable project file route schema remains: %s", unreachable)
		}
	}
}
