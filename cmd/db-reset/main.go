// Command db-reset recreates the local development database from the current
// authoritative schema and seeds, then exits. The same production refusal,
// effective database-name check and explicit confirmation used at application
// startup are mandatory here.
package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func main() {
	cfg := config.Load()
	if !cfg.DB.ResetOnStart {
		log.Fatal("refusing reset: set DB_RESET_ON_START=true and DB_RESET_CONFIRM explicitly")
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("refusing reset: %v", err)
	}
	databaseName, err := cfg.DB.EffectiveName()
	if err != nil {
		log.Fatalf("resolve database name: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, cfg.DB.ConnString())
	if err != nil {
		log.Fatalf("connect %s: %v", databaseName, err)
	}
	defer db.Close()
	if err = database.ResetDevelopmentSchema(ctx, db); err != nil {
		log.Fatalf("reset %s: %v", databaseName, err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		log.Fatalf("install schema in %s: %v", databaseName, err)
	}
	if err = database.SeedRBAC(ctx, db); err != nil {
		log.Fatalf("seed %s: %v", databaseName, err)
	}
	log.Printf("development database %s reset to the current schema and seed baseline", databaseName)
}
