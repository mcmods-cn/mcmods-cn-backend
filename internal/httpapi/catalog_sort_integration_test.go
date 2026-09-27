package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestPublicCatalogCoreSortQueriesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute catalog queries against the development database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	server := &Server{db: pool}

	type catalogEndpoint struct {
		name    string
		path    string
		handler http.HandlerFunc
		prepare func(*http.Request)
	}
	endpoints := []catalogEndpoint{
		{name: "mods", path: "/api/v1/mods", handler: server.publicMods},
		{name: "modpacks", path: "/api/v1/modpacks", handler: server.modpacks},
		{name: "plugins", path: "/api/v1/content-projects/plugin", handler: server.simpleProjects, prepare: func(request *http.Request) { request.SetPathValue("projectType", "plugin") }},
		{name: "addons", path: "/api/v1/content-projects/addon", handler: server.simpleProjects, prepare: func(request *http.Request) { request.SetPathValue("projectType", "addon") }},
		{name: "datapacks", path: "/api/v1/content-projects/datapack", handler: server.simpleProjects, prepare: func(request *http.Request) { request.SetPathValue("projectType", "datapack") }},
		{name: "maps", path: "/api/v1/content-projects/map", handler: server.simpleProjects, prepare: func(request *http.Request) { request.SetPathValue("projectType", "map") }},
		{name: "resource_packs", path: "/api/v1/content-projects/resource_pack", handler: server.simpleProjects, prepare: func(request *http.Request) { request.SetPathValue("projectType", "resource_pack") }},
		{name: "shaders", path: "/api/v1/content-projects/shader_pack", handler: server.simpleProjects, prepare: func(request *http.Request) { request.SetPathValue("projectType", "shader_pack") }},
		{name: "tutorials", path: "/api/v1/community/posts?kind=tutorial", handler: server.communityPosts},
		{name: "issues", path: "/api/v1/community/posts?kind=issue", handler: server.communityPosts},
		{name: "news", path: "/api/v1/community/posts?kind=news", handler: server.communityPosts},
		{name: "discussions", path: "/api/v1/community/posts?kind=discussion", handler: server.communityPosts},
		{name: "skins", path: "/api/v1/skins", handler: server.skins},
		{name: "blueprints", path: "/api/v1/blueprints", handler: server.blueprints},
		{name: "authors", path: "/api/v1/creators?kind=author", handler: server.creators},
		{name: "teams", path: "/api/v1/creators?kind=team", handler: server.creators},
	}
	fields := []string{"published", "updated", "heat", "views"}
	for _, endpoint := range endpoints {
		endpoint := endpoint
		t.Run(endpoint.name, func(t *testing.T) {
			for index, field := range fields {
				direction := "desc"
				if index%2 == 1 {
					direction = "asc"
				}
				separator := "?"
				if requestPathHasQuery(endpoint.path) {
					separator = "&"
				}
				request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s%ssort=%s&order=%s&limit=1", endpoint.path, separator, field, direction), nil).WithContext(ctx)
				if endpoint.prepare != nil {
					endpoint.prepare(request)
				}
				response := httptest.NewRecorder()
				endpoint.handler(response, request)
				if response.Code != http.StatusOK {
					t.Fatalf("%s %s returned %d: %s", field, direction, response.Code, response.Body.String())
				}
			}
		})
	}

	for _, endpoint := range []catalogEndpoint{
		{name: "modpacks_containing_mod", path: "/api/v1/modpacks?mods=minecraft", handler: server.modpacks},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, endpoint.path, nil).WithContext(ctx)
			response := httptest.NewRecorder()
			endpoint.handler(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("contained-mod filter returned %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func requestPathHasQuery(path string) bool {
	for _, character := range path {
		if character == '?' {
			return true
		}
	}
	return false
}
