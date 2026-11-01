// oss-byte-maintenance defaults to scoped read-only diagnosis. Production
// execution needs its own authorization; this command never changes schema.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/httpapi"
)

func readPlan(path string) (httpapi.OSSByteMaintenancePlan, error) {
	var plan httpapi.OSSByteMaintenancePlan
	f, err := os.Open(path)
	if err != nil {
		return plan, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return plan, err
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > 1<<20 {
		return plan, errors.New("plan must be a private regular file of at most1MiB")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&plan); err != nil {
		return plan, err
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return plan, errors.New("plan contains trailing data")
	}
	return plan, nil
}

func main() {
	planPath := flag.String("plan", "", "private reviewed exact-ID plan file")
	reportPath := flag.String("report", "", "new private count/hash diagnostic report file")
	apply := flag.Bool("apply", false, "apply the reviewed plan; default is dry-run")
	databaseName := flag.String("database", "", "exact database confirmation required for apply")
	backupReference := flag.String("backup-reference", "", "existing operator-owned backup reference required for apply")
	flag.Parse()
	if flag.NArg() != 0 || *planPath == "" || *reportPath == "" || (*apply && (*databaseName == "" || *backupReference == "")) {
		fmt.Fprintln(os.Stderr, "provide private --plan and new --report; apply also requires exact --database and --backup-reference")
		os.Exit(1)
	}
	plan, err := readPlan(*planPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid private maintenance plan")
		os.Exit(1)
	}
	if *apply && plan.Database != *databaseName {
		fmt.Fprintln(os.Stderr, "database confirmation does not match reviewed plan")
		os.Exit(1)
	}
	reportFile, err := os.OpenFile(*reportPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "report must be a new private writable file")
		os.Exit(1)
	}
	cfg := config.Load()
	pool, err := pgxpool.New(context.Background(), cfg.DB.ConnString())
	if err != nil {
		reportFile.Close()
		fmt.Fprintln(os.Stderr, "invalid database configuration")
		os.Exit(1)
	}
	defer pool.Close()
	report, runErr := httpapi.RunOSSByteMaintenance(context.Background(), pool, cfg, plan, httpapi.OSSByteMaintenanceOptions{Apply: *apply, BackupReference: *backupReference})
	encodeErr := json.NewEncoder(reportFile).Encode(report)
	if encodeErr == nil {
		encodeErr = reportFile.Sync()
	}
	closeErr := reportFile.Close()
	if runErr != nil || encodeErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "maintenance did not complete; retain the private report and inspect schema/plan conditions")
		os.Exit(1)
	}
	// The private report holds observed hashes. Console output contains only
	// status counts; no object keys, preimages, credentials or signed URLs.
	if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"dryRun": report.DryRun, "readBytes": report.ReadBytes, "counts": report.Counts, "privateReportWritten": true}); err != nil {
		os.Exit(1)
	}
}
