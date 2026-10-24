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
	"mcmods-cn-backend/internal/httpapi"
)

func main() {
	target := flag.String("database", "", "exact expected database name (required)")
	revision := flag.String("revision", "", "optional revision ID scope")
	limit := flag.Int("limit", 1000, "maximum inspected rows (1..100000); reports truncated")
	flag.Parse()
	if *target == "" || *limit < 1 || *limit > 100000 {
		fmt.Fprintln(os.Stderr, "--database and valid bounded --limit are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg := config.Load()
	poolCfg, err := pgxpool.ParseConfig(cfg.DB.ConnString())
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid database configuration")
		os.Exit(1)
	}
	if poolCfg.ConnConfig.Database != *target {
		fmt.Fprintln(os.Stderr, "configured database does not match --database")
		os.Exit(2)
	}
	poolCfg.MaxConns = 1
	poolCfg.ConnConfig.RuntimeParams["search_path"] = "public,pg_catalog"
	poolCfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect diagnostic database failed")
		os.Exit(1)
	}
	defer pool.Close()
	report, err := httpapi.DiagnoseImportPrivacy(ctx, pool, *revision, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read-only diagnostic query failed")
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "write diagnostic report failed")
		os.Exit(1)
	}
}
