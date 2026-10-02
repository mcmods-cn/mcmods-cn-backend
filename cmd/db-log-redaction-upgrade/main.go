// Command db-log-redaction-upgrade explicitly installs the generation-168
// applied-redactor marker. Inspection is read-only; applying never resets
// schema, replaces original share identities, or removes business data.
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

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func main() {
	if err := run(); err != nil {
		log.Printf("log redaction schema upgrade failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	apply := flag.Bool("apply", false, "install and validate the applied redactor version column")
	confirmed := flag.String("confirm-database", "", "exact database name required for applying")
	backup := flag.String("backup", "", "new private pre-change schema metadata record (not a data backup)")
	flag.Parse()
	if flag.NArg() != 0 || (*apply && (*confirmed == "" || *backup == "")) ||
		(!*apply && (*confirmed != "" || *backup != "")) {
		return errors.New("use read-only inspection, or -apply with -confirm-database and a new -backup file")
	}
	cfg := config.Load()
	name, err := cfg.DB.EffectiveName()
	if err != nil || (*apply && name != *confirmed) {
		return errors.New("database configuration does not match the confirmed target")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if *apply {
		if err := database.ApplyLogRedactionSchemaUpgrade(ctx, pool, *confirmed,
			func(state database.LogRedactionSchemaState) error { return persistState(*backup, state) }); err != nil {
			return err
		}
	}
	state, err := database.InspectLogRedactionSchema(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Printf("generation=%d applied_version_column=%t constraint_validated=%t upgrade_required=%t applied=%t\n",
		state.Generation, state.Present, state.Validated, !state.Present || !state.Validated, *apply)
	return nil
}

func persistState(path string, state database.LogRedactionSchemaState) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("schema metadata backup must be a new writable private file")
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(state); err != nil {
		return fmt.Errorf("write schema metadata backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync schema metadata backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close schema metadata backup: %w", err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("cannot open schema metadata backup directory")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return errors.New("cannot persist schema metadata backup directory entry")
	}
	return nil
}
