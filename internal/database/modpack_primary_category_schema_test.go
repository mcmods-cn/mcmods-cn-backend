package database

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/catalogpolicy"
	"mcmods-cn-backend/internal/config"
)

var bug029ModpackCategories = catalogpolicy.ModpackCategories()

func TestBUG029ModpackPrimaryCategoryIsCheckedByGeneration145Schema(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168 for the new persisted category invariant", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(modpackSchemaStatements(), "\n"))
	constraint := "constraint modpacks_primary_category_check check(primary_category in ('" +
		strings.Join(bug029ModpackCategories, "','") + "'))"
	if !strings.Contains(schema, constraint) {
		t.Fatalf("modpacks schema lacks the authoritative primary-category check:\n%s", constraint)
	}
}

func TestBUG029ModpackPrimaryCategoryDatabaseInvariantIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the modpack category invariant")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	for index, category := range bug029ModpackCategories {
		if _, err = pool.Exec(ctx, `insert into modpacks(slug,primary_name,primary_category)
			values($1,$2,$3)`, "bug029-valid-"+category, "Valid category", category); err != nil {
			t.Fatalf("registered category[%d] %q failed direct insert: %v", index, category, err)
		}
	}
	_, err = pool.Exec(ctx, `insert into modpacks(slug,primary_name,primary_category)
		values('bug029-invalid','Invalid category','orphan_category')`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "modpacks_primary_category_check" {
		t.Fatalf("invalid category error=%v; want check violation from modpacks_primary_category_check", err)
	}
}
