package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/auditdata"
	"mcmods-cn-backend/internal/config"
	"os"
	"time"
)

func main() {
	sinceText := flag.String("since", "", "inclusive RFC3339 creation time; required")
	untilText := flag.String("until", "", "exclusive RFC3339 creation time; required")
	limit := flag.Int("limit", 100, "public-ID sample limit per section, 1..1000")
	flag.Parse()
	since, e1 := time.Parse(time.RFC3339, *sinceText)
	until, e2 := time.Parse(time.RFC3339, *untilText)
	if e1 != nil || e2 != nil || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "explicit RFC3339 --since and --until are required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid database configuration")
		os.Exit(1)
	}
	defer pool.Close()
	report, err := auditdata.Inspect(ctx, pool, since, until, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read-only known-data diagnosis failed:", err)
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(report); err != nil {
		os.Exit(1)
	}
}
