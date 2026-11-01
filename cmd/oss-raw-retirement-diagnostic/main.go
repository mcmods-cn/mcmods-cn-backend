// Only read-only diagnosis exists; this command has no apply/delete operation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/httpapi"
)

func main() {
	ids := flag.String("file-ids", "", "comma-separated exact public file IDs, at most100")
	path := flag.String("report", "", "new private JSON diagnostic report path")
	flag.Parse()
	if flag.NArg() != 0 || *ids == "" || *path == "" {
		fmt.Fprintln(os.Stderr, "provide exact --file-ids and a new private --report; no apply operation exists")
		os.Exit(1)
	}
	f, err := os.OpenFile(*path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "report must be a new private writable file")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		f.Close()
		fmt.Fprintln(os.Stderr, "invalid database configuration")
		os.Exit(1)
	}
	defer pool.Close()
	report, runErr := httpapi.InspectOSSRawRetirements(ctx, pool, strings.Split(*ids, ","))
	encodeErr := json.NewEncoder(f).Encode(report)
	if encodeErr == nil {
		encodeErr = f.Sync()
	}
	closeErr := f.Close()
	if runErr != nil || encodeErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "read-only diagnosis did not complete; inspect schema and exact public-ID scope")
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"readOnly": report.ReadOnly, "providerBytesObserved": false, "counts": report.Counts, "privateReportWritten": true}); err != nil {
		os.Exit(1)
	}
}
