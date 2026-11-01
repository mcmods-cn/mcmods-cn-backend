// Command diagnose-review-result-intents never writes business or repair data.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/auditdiag"
	"mcmods-cn-backend/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "review result diagnostic failed:", err)
		os.Exit(1)
	}
}

func run() error {
	kind := flag.String("kind", "", "exact kind: modpack, plugin, map, resource_pack, shader_pack, datapack, addon, creator")
	identity := flag.String("public-id", "", "required exact public aggregate identity")
	sinceText := flag.String("since", "", "required inclusive RFC3339 start")
	untilText := flag.String("until", "", "required exclusive RFC3339 end; interval <=31 days")
	limit := flag.Int("limit", 100, "maximum decisions, 1..1000")
	schema := flag.String("schema", "public", "exact reviewed schema")
	showSQL := flag.Bool("sql", false, "print SELECT for review without connecting")
	flag.Parse()
	if *showSQL {
		fmt.Println(auditdiag.ReviewResultsSQL)
		return nil
	}
	since, err := time.Parse(time.RFC3339, *sinceText)
	if err != nil {
		return errors.New("require an explicit RFC3339 --since")
	}
	until, err := time.Parse(time.RFC3339, *untilText)
	if err != nil {
		return errors.New("require an explicit RFC3339 --until")
	}
	scope := auditdiag.ReviewResultScope{Kind: *kind, PublicID: *identity, Since: since, Until: until, Limit: *limit}
	if err = scope.Validate(); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`).MatchString(*schema) {
		return errors.New("invalid exact schema identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		return errors.New("database configuration unavailable")
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return errors.New("cannot begin read-only snapshot")
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `select set_config('search_path',$1,true),set_config('statement_timeout','10s',true)`, "pg_catalog,"+pgx.Identifier{*schema}.Sanitize()+",pg_temp"); err != nil {
		return errors.New("cannot apply bounded diagnostic scope")
	}
	items, err := auditdiag.ReadReviewResults(ctx, tx, scope)
	if err != nil {
		return errors.New("cannot read review result candidates; no changes applied")
	}
	var snapshot, access string
	if err = tx.QueryRow(ctx, `select pg_current_snapshot()::text,current_setting('transaction_read_only')`).Scan(&snapshot, &access); err != nil || access != "on" {
		return errors.New("cannot confirm read-only snapshot")
	}
	result := map[string]any{"tool": "diagnose-review-result-intents/v1", "read_only": true, "repair_supported": false, "scope": scope, "schema": *schema, "postgres_snapshot": snapshot, "decisions": items, "result_may_be_truncated": len(items) == scope.Limit, "correlation": "Same target, recipient, template and decision-time interval; old payloads have no exact revision ID. Counts do not prove recipient delivery. Retention, deletion, delayed delivery or overlapping decisions require historical correlation. Missing candidates never authorize reconstruction or reward replay. No data changed."}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
