// Command db-function-repair inspects, explicitly repairs, or restores the
// four generation-168 projection trigger functions. It never resets schema
// or modifies business rows. Apply and restore require an exact target and a
// new, private backup file before any transaction can commit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func main() {
	if err := run(); err != nil {
		log.Printf("database function repair failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	apply := flag.Bool("apply", false, "apply the four reviewed functions and comment bindings")
	restorePath := flag.String("restore", "", "restore definitions from a trusted backup produced by this command")
	confirmedDatabase := flag.String("confirm-database", "", "exact database name required for apply or restore")
	backupPath := flag.String("backup", "", "new private backup file required before apply or restore")
	flag.Parse()
	if flag.NArg() != 0 || (*apply && *restorePath != "") {
		return errors.New("use inspection, -apply, or -restore; these modes cannot be combined")
	}
	change := *apply || *restorePath != ""
	if change && (*confirmedDatabase == "" || *backupPath == "") {
		return errors.New("apply or restore requires -confirm-database and a new -backup file")
	}
	if !change && (*confirmedDatabase != "" || *backupPath != "") {
		return errors.New("inspection does not use confirmation or backup flags")
	}
	cfg := config.Load()
	databaseName, err := cfg.DB.EffectiveName()
	if err != nil {
		return errors.New("invalid database target configuration")
	}
	if change && databaseName != *confirmedDatabase {
		return errors.New("confirmation does not match the configured database target")
	}
	var restore *database.ProjectionFunctionBackup
	if *restorePath != "" {
		backup, err := readBackup(*restorePath)
		if err != nil {
			return err
		}
		restore = &backup
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if !change {
		backup, changed, err := database.InspectProjectionFunctions(ctx, pool)
		if err != nil {
			return err
		}
		fmt.Printf("generation=%d checked_functions=%d functions_requiring_repair=%d checked_comment_triggers=%d comment_bindings_require_repair=%t; no changes applied\n",
			backup.Generation, len(backup.Functions), changed, len(backup.CommentTriggers), len(backup.CommentTriggers) != 3)
		return nil
	}
	if err := database.ChangeProjectionFunctions(ctx, pool, *confirmedDatabase, restore,
		func(backup database.ProjectionFunctionBackup) error { return writeBackup(*backupPath, backup) }); err != nil {
		return err
	}
	if restore != nil {
		fmt.Println("four projection functions and comment bindings restored; business rows and schema generation unchanged")
	} else {
		fmt.Println("four projection functions and comment bindings repaired; business rows and schema generation unchanged")
	}
	return nil
}

func writeBackup(path string, backup database.ProjectionFunctionBackup) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("backup must be a new writable file; existing files are never overwritten")
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(backup); err != nil {
		return fmt.Errorf("write function backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync function backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close function backup: %w", err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("cannot sync the backup directory")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return errors.New("cannot persist the backup directory entry")
	}
	return nil
}

func readBackup(path string) (database.ProjectionFunctionBackup, error) {
	var backup database.ProjectionFunctionBackup
	file, err := os.Open(path)
	if err != nil {
		return backup, errors.New("cannot open the selected function backup")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return backup, errors.New("function backup must be a regular file no larger than 1 MiB")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&backup); err != nil {
		return backup, errors.New("invalid function backup JSON")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return backup, errors.New("function backup must contain exactly one JSON document")
	}
	return backup, nil
}
