package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestModpackContainedModFilterIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify contained-mod filters against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	suffix := fmt.Sprintf("%08x", uint32(time.Now().UnixNano()))
	projectCode := "t" + suffix
	identifier := "catalog_filter_" + suffix
	var modID int64
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,$3,'approved') returning id`, projectCode, "catalog-filter-mod-"+suffix, "Catalog filter mod").Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_identifiers(mod_id,identifier,is_primary) values($1,$2,true)`, modID, identifier); err != nil {
		t.Fatal(err)
	}
	var modpackID int64
	if err = tx.QueryRow(ctx, `insert into modpacks(slug,primary_name,review_status)
		values($1,$2,'approved') returning id`, "catalog-filter-pack-"+suffix, "Catalog filter pack").Scan(&modpackID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into modpack_mods(modpack_id,mod_id,provider,identifier)
		values($1,$2,'manual',$3)`, modpackID, modID, identifier); err != nil {
		t.Fatal(err)
	}

	empty := []string{}
	count := func(filters []string) int {
		t.Helper()
		var total int
		if queryErr := tx.QueryRow(ctx, `select count(*) from modpacks pack `+publicModpackCatalogFilter,
			int64(0), "", false, []int64{}, empty, empty, empty, empty, "any", empty,
			empty, empty, empty, empty, 0, filters).Scan(&total); queryErr != nil {
			t.Fatal(queryErr)
		}
		return total
	}
	if total := count([]string{identifier}); total != 1 {
		t.Fatalf("expected one modpack containing %q, got %d", identifier, total)
	}
	if total := count([]string{identifier, "missing_mod"}); total != 0 {
		t.Fatalf("all-mode contained-mod filter matched a pack missing one requested mod: %d", total)
	}
}
