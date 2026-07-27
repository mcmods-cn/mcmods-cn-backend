package database

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestCompatibleBuiltinModContentTemplatesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify compatible built-in templates")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `select count(*)::int
		from mod_content_templates
		where builtin and status='active' and default_display_mode='large'
		  and (
			(code='loot_table' and definition->'resourceKinds' ? 'minecraft.loot_table')
			or
			(code='game_setting' and definition->'resourceKinds' ? 'minecraft.game_setting')
		  )`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected loot_table and game_setting built-in templates, got %d", count)
	}
}
