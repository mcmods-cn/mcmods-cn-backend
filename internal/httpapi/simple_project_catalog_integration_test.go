package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

// This is a separate schema smoke test, not catalog behavior evidence.
func TestSimpleProjectCatalogSchemaSQLSmokeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to inspect the development database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	for _, projectType := range []string{"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"} {
		t.Run(projectType, func(t *testing.T) {
			empty := []string{}
			var total int
			if err := pool.QueryRow(ctx, `select count(*) from simple_projects project `+simpleProjectCatalogFilter,
				projectType, int64(0), "", empty, empty, empty, false, []int64{}, empty, empty,
				empty, empty, empty, empty, empty, empty, 0, "any").Scan(&total); err != nil {
				t.Fatal(err)
			}
			rows, err := pool.Query(ctx, `select project.id from simple_projects project `+simpleProjectCatalogFilter+`
				order by case when $7 then array_position($8::bigint[],project.id) end,project.updated_at desc,project.id desc
				limit $19 offset $20`, projectType, int64(0), "", empty, empty, empty, false, []int64{}, empty, empty,
				empty, empty, empty, empty, empty, empty, 0, "any", 24, 0)
			if err != nil {
				t.Fatal(err)
			}
			var scanned int
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				scanned++
			}
			rowErr := rows.Err()
			rows.Close()
			if rowErr != nil {
				t.Fatal(rowErr)
			}
			if scanned > total || scanned > 24 {
				t.Fatalf("smoke rows=%d total=%d", scanned, total)
			}
		})
	}
}

func TestSimpleProjectReadsDistinguishStorageFailure(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), "postgres://unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	for _, tc := range []struct {
		name    string
		method  string
		handler http.HandlerFunc
	}{
		{"detail", http.MethodGet, server.simpleProjectItem},
		{"editor", http.MethodGet, server.simpleProjectEditor},
		{"revision", http.MethodPut, server.simpleProjectItem},
		{"history", http.MethodGet, server.simpleProjectHistory},
		{"gallery", http.MethodGet, server.simpleProjectGalleryImage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/", nil)
			request.SetPathValue("projectType", "map")
			request.SetPathValue("siteId", "sample")
			request.SetPathValue("publicId", "testimage")
			response := httptest.NewRecorder()
			tc.handler(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("project read with unavailable storage returned %d", response.Code)
			}
		})
	}
}
