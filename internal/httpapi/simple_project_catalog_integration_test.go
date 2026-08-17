package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestSimpleProjectCatalogCountIntegration(t *testing.T) {
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
			rows.Close()
		})
	}
}
