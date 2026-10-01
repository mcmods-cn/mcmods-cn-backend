// test-setup initializes only a cluster created by scripts/test-services.sh.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/testenv"
)

func main() {
	if err := setup(); err != nil {
		log.Fatal(err)
	}
	log.Print("owned isolated database migrated and seeded")
}

func setup() error {
	cfg := config.Load()
	if err := testenv.ValidateOwnedDatabaseTarget(cfg, ""); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect isolated database: %w", err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate isolated database: %w", err)
	}
	if err = database.SeedRBAC(ctx, pool); err != nil {
		return fmt.Errorf("seed isolated database: %w", err)
	}
	return nil
}
