// Read-only diagnosis is the default. Applying DDL never cancels a job or
// modifies a source file, balance or publication.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"os"
	"time"
)

func main() {
	apply := flag.Bool("apply", false, "install import publication policy extension")
	target := flag.String("database", "", "exact target database required for apply")
	backup := flag.String("backup-reference", "", "verified backup reference required for apply")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid database configuration")
		os.Exit(1)
	}
	defer db.Close()
	var name string
	var generation int
	if err = db.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).Scan(&name, &generation); err != nil || generation != 168 {
		fmt.Fprintln(os.Stderr, "requires accessible generation-168 database")
		os.Exit(1)
	}
	var present bool
	if err = db.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name='catalog_import_jobs' and column_name='requested_publication_mode')`).Scan(&present); err != nil {
		fmt.Fprintln(os.Stderr, "failed to inspect schema")
		os.Exit(1)
	}
	predicate := "true"
	if present {
		predicate = "requested_publication_mode='legacy_unknown'"
	}
	var count int64
	if err = db.QueryRow(ctx, `select count(*) from public.catalog_import_jobs where status in ('queued','validating','confirmation_required','importing') and `+predicate).Scan(&count); err != nil {
		fmt.Fprintln(os.Stderr, "failed to diagnose legacy jobs")
		os.Exit(1)
	}
	ids := []string{}
	rows, err := db.Query(ctx, `select id from public.catalog_import_jobs where status in ('queued','validating','confirmation_required','importing') and `+predicate+` order by id limit 1000`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to list legacy jobs")
		os.Exit(1)
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err != nil || rows.Err() != nil {
		fmt.Fprintln(os.Stderr, "failed to scan legacy jobs")
		os.Exit(1)
	}
	if *apply {
		if *target != name || *backup == "" {
			fmt.Fprintln(os.Stderr, "apply requires exact --database and --backup-reference")
			os.Exit(1)
		}
		tx, e := db.Begin(ctx)
		if e != nil {
			fmt.Fprintln(os.Stderr, "failed to begin migration")
			os.Exit(1)
		}
		defer tx.Rollback(context.Background())
		_, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-cn-schema-migrations'))`)
		if err == nil {
			for _, statement := range database.CatalogImportPolicySchemaStatements() {
				if _, err = tx.Exec(ctx, statement); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = database.RequireCatalogImportPolicySchema(ctx, tx)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "policy migration failed; DDL transaction rolled back")
			os.Exit(1)
		}
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"database": name, "generation": generation, "column_present_before": present, "applied": *apply, "legacy_active_jobs": count, "candidate_ids": ids, "candidate_list_truncated": count > int64(len(ids)), "job_states_modified": false, "worker_effect": "legacy_unknown cancels at execution with audit; re-upload required; no guessed past authorization"})
}
