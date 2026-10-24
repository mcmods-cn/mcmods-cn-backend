// Command import-source-privacy-audit inventories stored provider-import results
// without disclosing their documents, source URLs or upstream error bodies.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type options struct {
	schema, actor, job, out string
	limit                   int
}
type candidate struct {
	Job            string    `json:"jobPublicId"`
	Provider       string    `json:"provider"`
	ProjectType    string    `json:"projectType"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	SourceHMAC     string    `json:"sourceHmacSha256"`
	ResultBytes    int       `json:"resultBytes"`
	ResultHMAC     string    `json:"resultHmacSha256,omitempty"`
	ErrorBytes     int       `json:"errorBytes"`
	ErrorHMAC      string    `json:"errorHmacSha256,omitempty"`
	Classification string    `json:"classification"`
}

var schemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
var publicIDPattern = regexp.MustCompile(`^[a-z0-9]{9}$`)

func parse(args []string) (options, error) {
	var o options
	f := flag.NewFlagSet("import-source-privacy-audit", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.schema, "schema", "", "explicit target schema")
	f.StringVar(&o.actor, "actor-public-id", "", "limit to one creator")
	f.StringVar(&o.job, "job-public-id", "", "limit to one metadata import job")
	f.StringVar(&o.out, "out", "", "new absolute 0600 JSON output file")
	f.IntVar(&o.limit, "max-jobs", 1000, "maximum1..100000 records")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return o, errors.New("invalid flags; use --schema, --actor-public-id and/or --job-public-id, --out, --max-jobs")
	}
	if !schemaPattern.MatchString(o.schema) || o.limit < 1 || o.limit > 100000 || !filepath.IsAbs(o.out) {
		return o, errors.New("explicit valid schema, absolute new output path and bounded max-jobs are required")
	}
	if o.actor == "" && o.job == "" {
		return o, errors.New("at least one actor/job scope is required")
	}
	if o.actor != "" && !publicIDPattern.MatchString(o.actor) || o.job != "" && !publicIDPattern.MatchString(o.job) {
		return o, errors.New("actor and job must be nine-character public IDs")
	}
	return o, nil
}
func fingerprint(key []byte, kind string, data []byte) string {
	sum := hmac.New(sha256.New, key)
	sum.Write([]byte(kind))
	sum.Write([]byte{0})
	sum.Write(data)
	return hex.EncodeToString(sum.Sum(nil))
}
func run(ctx context.Context, o options) error {
	databaseURL := os.Getenv("DATABASE_URL")
	key := []byte(os.Getenv("MCMODS_SOURCE_AUDIT_HMAC_KEY"))
	if databaseURL == "" || len(key) < 32 {
		return errors.New("explicit DATABASE_URL and MCMODS_SOURCE_AUDIT_HMAC_KEY of at least32 bytes are required")
	}
	if _, err := os.Lstat(o.out); err == nil {
		return errors.New("output already exists; preserve earlier inventory")
	} else if !os.IsNotExist(err) {
		return errors.New("cannot inspect output path")
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return errors.New("database connection failed")
	}
	defer conn.Close(context.Background())
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("cannot begin read-only inventory")
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "set local search_path="+pgx.Identifier{o.schema}.Sanitize()+",pg_catalog"); err != nil {
		return errors.New("cannot set explicit inventory schema")
	}
	var readOnly, isolation, snapshot string
	if err = tx.QueryRow(ctx, `select current_setting('transaction_read_only'),current_setting('transaction_isolation'),pg_current_snapshot()::text`).Scan(&readOnly, &isolation, &snapshot); err != nil || readOnly != "on" || isolation != "repeatable read" {
		return errors.New("required read-only repeatable-read transaction not established")
	}
	// Only identifiers enter the report. Documents and URLs are used solely as
	// inputs to keyed fingerprints. No provider request or authorization occurs.
	rows, err := tx.Query(ctx, `select job.public_id,job.provider,job.project_type,job.status,job.created_at,job.source_url,job.result::text,job.error from mod_metadata_import_jobs job join users actor on actor.id=job.user_id where ($1='' or actor.public_id=$1) and ($2='' or job.public_id=$2) and (job.result is not null or job.error<>'') order by job.created_at,job.public_id limit $3`, o.actor, o.job, o.limit+1)
	if err != nil {
		return errors.New("read-only inventory query failed; verify actual migration schema and scope")
	}
	items := []candidate{}
	truncated := false
	for rows.Next() {
		if len(items) == o.limit {
			truncated = true
			break
		}
		var c candidate
		var source, upstreamError string
		var result []byte
		if err = rows.Scan(&c.Job, &c.Provider, &c.ProjectType, &c.Status, &c.CreatedAt, &source, &result, &upstreamError); err != nil {
			rows.Close()
			return errors.New("inventory row could not be read completely")
		}
		c.SourceHMAC = fingerprint(key, "source", []byte(source))
		c.ResultBytes = len(result)
		c.ErrorBytes = len(upstreamError)
		if len(result) > 0 {
			c.ResultHMAC = fingerprint(key, "result", result)
		}
		if upstreamError != "" {
			c.ErrorHMAC = fingerprint(key, "error", []byte(upstreamError))
		}
		c.Classification = "CANDIDATE_REQUIRES_CURRENT_SOURCE_AUTHORIZATION_AND_RETENTION_REVIEW"
		items = append(items, c)
	}
	rows.Close()
	if rows.Err() != nil {
		return errors.New("inventory iteration failed; partial results not emitted")
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("read-only inventory snapshot could not be completed")
	}
	report := map[string]any{"tool": "import-source-privacy-audit/v1", "recordedAt": time.Now().UTC(), "schema": o.schema, "actorPublicId": o.actor, "jobPublicId": o.job, "readOnly": readOnly, "isolation": isolation, "postgresSnapshot": snapshot, "maxJobs": o.limit, "candidateCount": len(items), "hasMore": truncated, "candidates": items, "classification": "Candidate inventory only: no source has been classified as private or public by this offline tool", "sourceVerification": "NOT_RUN: use explicit current authorized provider facts before any disposition", "mutation": "NONE; no task/result/error/project/grant/ledger/assets are modified", "scopeLimit": "Does not locate previously copied/published derivatives or retract third-party downloads; production repair requires a separately authorized reviewed plan"}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return errors.New("cannot encode inventory")
	}
	file, err := os.OpenFile(o.out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot create new private inventory file")
	}
	_, writeErr := file.Write(append(raw, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("inventory output write failed")
	}
	return nil
}
func main() {
	o, err := parse(os.Args[1:])
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = run(ctx, o)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
	fmt.Println("Read-only candidate inventory saved; no private-source determination or data mutation performed.")
}
