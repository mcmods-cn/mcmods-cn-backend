package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestInventoryConnectionFailureDoesNotRevealCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	executable := filepath.Join(t.TempDir(), "database-inventory")
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", executable, ".").CombinedOutput(); err != nil {
		t.Fatalf("build inventory CLI: %v\n%s", err, output)
	}
	const syntheticPassword = "oct02_fixture_password"
	command := exec.CommandContext(ctx, executable, "-output", filepath.Join(t.TempDir(), "catalog.md"))
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "DATABASE_URL=") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "DATABASE_URL=postgresql://fixture:"+syntheticPassword+"@%zz/database")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("malformed connection unexpectedly succeeded")
	}
	if strings.Contains(string(output), syntheticPassword) || strings.Contains(string(output), "postgresql://") {
		t.Fatal("connection failure exposed credentials or connection URI")
	}
}

func TestInventoryColumnsExcludeViewsIntegration(t *testing.T) {
	target := os.Getenv("MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET")
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || target == "" {
		t.Skip("requires an explicitly confirmed exclusively owned test database")
	}
	cfg := config.Load()
	configured, err := cfg.DB.EffectiveName()
	if err != nil || configured != target {
		t.Fatal("configured database does not match the exclusively owned target")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var actual string
	if err := pool.QueryRow(ctx, "select current_database()").Scan(&actual); err != nil || actual != target {
		t.Fatal("connected database does not match the exclusively owned target")
	}
	suffix := time.Now().UnixNano()
	tableName := fmt.Sprintf("oct02_inventory_table_%d", suffix)
	viewName := fmt.Sprintf("oct02_inventory_view_%d", suffix)
	tableSQL, viewSQL := pgx.Identifier{"public", tableName}.Sanitize(), pgx.Identifier{"public", viewName}.Sanitize()
	if _, err := pool.Exec(ctx, "create table "+tableSQL+" (id bigint)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "drop view if exists "+viewSQL+"; drop table "+tableSQL); err != nil {
			t.Errorf("remove exclusively owned inventory fixture: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, "create view "+viewSQL+" as select id from "+tableSQL); err != nil {
		t.Fatal(err)
	}
	var sawTable bool
	for _, item := range queryColumns(ctx, pool) {
		if item.table == viewName {
			t.Fatal("view columns were counted as base table columns")
		}
		sawTable = sawTable || item.table == tableName
	}
	if !sawTable {
		t.Fatal("base table columns disappeared from the inventory")
	}
}
