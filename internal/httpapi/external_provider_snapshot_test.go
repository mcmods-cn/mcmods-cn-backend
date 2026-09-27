package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExternalProviderSnapshotsReturnValidatedSharedFacts(t *testing.T) {
	t.Run("modrinth", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			switch request.URL.Path {
			case "/project/example":
				_, _ = response.Write([]byte(`{"id":"project-id","project_type":"mod","team":"team-id","slug":"example"}`))
			case "/project/project-id/version":
				_, _ = response.Write([]byte(`[{"id":"version-id","game_versions":["1.21.1"],"loaders":["fabric"]}]`))
			case "/team/team-id/members":
				_, _ = response.Write([]byte(`[{"role":"Developer","user":{"username":"alice"}}]`))
			default:
				http.NotFound(response, request)
			}
		}))
		defer server.Close()
		cfg := defaultModImportConfig()
		cfg.Modrinth.BaseURL = server.URL
		snapshot, err := loadModrinthProviderSnapshot(context.Background(), server.Client(), cfg, "example")
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Project.ID != "project-id" || len(snapshot.Versions) != 1 || snapshot.Versions[0].ID != "version-id" ||
			len(snapshot.Authors) != 1 || snapshot.Authors[0].Name != "alice" || snapshot.Authors[0].Kind != "author" {
			t.Fatalf("unexpected Modrinth snapshot: %#v", snapshot)
		}
	})

	t.Run("curseforge", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			switch request.URL.Path {
			case "/mods/search":
				_, _ = response.Write([]byte(`{"data":[{"id":34,"classId":6,"slug":"example","authors":[{"name":"bob"}]}]}`))
			case "/mods/34/description":
				_, _ = response.Write([]byte(`{"data":"<p>Body</p>"}`))
			default:
				http.NotFound(response, request)
			}
		}))
		defer server.Close()
		cfg := defaultModImportConfig()
		cfg.CurseForge.BaseURL = server.URL
		snapshot, err := loadCurseForgeProviderSnapshot(context.Background(), server.Client(), cfg, http.Header{}, 6, "example")
		if err != nil {
			t.Fatal(err)
		}
		authors := curseForgeAuthors(snapshot.Project)
		if snapshot.Project.ID != 34 || snapshot.DescriptionHTML != "<p>Body</p>" || len(authors) != 1 || authors[0].Name != "bob" {
			t.Fatalf("unexpected CurseForge snapshot: %#v authors=%#v", snapshot, authors)
		}
	})
}
