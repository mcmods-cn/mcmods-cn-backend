// Command db-index-repair applies only forward change20261003_01. It reads an
// explicit URL from the environment, never dotenv or deployment defaults.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
)

func main() {
	if err := run(); err != nil {
		log.Printf("tag prefix index repair failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	apply := flag.Bool("apply", false, "apply forward change 20261003_01 concurrently")
	confirmed := flag.String("confirm-database", "", "exact database name required for apply")
	repairInvalid := flag.Bool("repair-invalid", false, "rebuild only a reviewed invalid index with no active build")
	backup := flag.String("backup", "", "new private metadata file required before an index change")
	flag.Parse()
	if flag.NArg() != 0 || (!*apply && (*confirmed != "" || *repairInvalid || *backup != "")) || (*apply && (*confirmed == "" || *backup == "")) {
		return errors.New("use inspection, or -apply with -confirm-database and new -backup; repair-invalid requires apply")
	}
	connectionURL := os.Getenv("MCMODS_INDEX_REPAIR_DATABASE_URL")
	if connectionURL == "" {
		return errors.New("explicit MCMODS_INDEX_REPAIR_DATABASE_URL is required; defaults are never used")
	}
	cfg, err := pgxpool.ParseConfig(connectionURL)
	if err != nil {
		return errors.New("invalid explicit database target configuration")
	}
	if *apply && cfg.ConnConfig.Database != *confirmed {
		return errors.New("database confirmation does not match the explicit configured target")
	}
	cfg.MaxConns, cfg.MinConns = 1, 0
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("cannot initialize the explicit database connection")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("cannot connect to the explicit database target")
	}
	if *apply {
		if err := database.ApplyCatalogTagPrefixIndex(ctx, pool, *confirmed, *repairInvalid, func(metadata database.CatalogTagPrefixIndexBackup) error {
			return persistMetadata(*backup, metadata)
		}); err != nil {
			return err
		}
		fmt.Println("forward change 20261003_01 verified; existing indexes, business rows and generation 168 retained")
		return nil
	}
	state, err := database.InspectCatalogTagPrefixIndex(ctx, pool)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(state)
}

func persistMetadata(path string, metadata database.CatalogTagPrefixIndexBackup) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("metadata destination must be a new writable file; existing files are never overwritten")
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(metadata); err != nil {
		return errors.New("cannot write prior index metadata")
	}
	if err := file.Sync(); err != nil {
		return errors.New("cannot persist prior index metadata")
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot close prior index metadata")
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("cannot open index metadata directory")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return errors.New("cannot persist index metadata directory entry")
	}
	return nil
}
