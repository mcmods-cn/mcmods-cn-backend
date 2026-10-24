// audit-identity-oss reports schema and legacy-hash counts. It never fetches
// private bytes or adjusts data balances. Apply expands schema only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func main() {
	apply := flag.Bool("apply", false, "apply the additive identity/OSS schema expansion")
	databaseName := flag.String("database", "", "exact database name required for apply")
	metadataFile := flag.String("backup-metadata", "", "new private schema metadata backup file required for apply")
	backupReference := flag.String("backup-reference", "", "operator's existing backup reference required for apply")
	flag.Parse()
	if flag.NArg() != 0 || (*apply && (*databaseName == "" || *backupReference == "" || *metadataFile == "")) {
		fmt.Fprintln(os.Stderr, "apply requires exact database, backup reference and new backup-metadata file")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		fmt.Fprintln(os.Stderr, "database configuration is invalid")
		os.Exit(1)
	}
	defer pool.Close()
	var state database.AuditIdentityOSSState
	if *apply {
		before, inspectErr := database.InspectAuditIdentityOSS(ctx, pool)
		if inspectErr != nil || before.Database != *databaseName {
			fmt.Fprintln(os.Stderr, "inspection failed or database does not match explicit target")
			os.Exit(1)
		}
		backup, backupErr := os.OpenFile(*metadataFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if backupErr != nil {
			fmt.Fprintln(os.Stderr, "metadata backup must be a new private writable file")
			os.Exit(1)
		}
		backupErr = json.NewEncoder(backup).Encode(before)
		if backupErr == nil {
			backupErr = backup.Sync()
		}
		closeErr := backup.Close()
		if backupErr != nil || closeErr != nil {
			fmt.Fprintln(os.Stderr, "cannot persist pre-change schema metadata")
			os.Exit(1)
		}
		state, err = database.ApplyAuditIdentityOSSUpgrade(ctx, pool, *databaseName, *backupReference)
	} else {
		state, err = database.InspectAuditIdentityOSS(ctx, pool)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "identity/OSS diagnosis or expansion failed:", err)
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"applied": *apply, "state": state, "legacy_hash_action": "diagnose and independently verify source bytes; never infer verification from claimed metadata"}); err != nil {
		os.Exit(1)
	}
}
