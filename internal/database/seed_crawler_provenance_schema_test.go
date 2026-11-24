package database

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSeedCrawlerCandidateProvenanceUsesSeparateFirstAndLastRuns(t *testing.T) {
	statements := governanceAutomationSchemaStatements()
	var candidateTable string
	for _, statement := range statements {
		if strings.HasPrefix(statement, "create table seed_crawler_candidates (") {
			candidateTable = statement
		}
	}
	for _, col := range []string{"first_seen_run_id", "last_seen_run_id"} {
		if !strings.Contains(candidateTable, col+" bigint not null references seed_crawler_runs(id)") {
			t.Errorf("candidate provenance lacks the %s relation", col)
		}
	}
	for _, index := range []string{
		"create index idx_seed_crawler_candidates_first_seen_run on seed_crawler_candidates(first_seen_run_id)",
		"create index idx_seed_crawler_candidates_last_seen_run on seed_crawler_candidates(last_seen_run_id)",
	} {
		if !slices.Contains(statements, index) {
			t.Errorf("missing candidate provenance index %q", index)
		}
	}
	for _, definition := range strings.Split(candidateTable, "\n") {
		if strings.HasPrefix(strings.TrimSpace(definition), "run_id ") {
			t.Fatal("ambiguous candidate run_id column")
		}
	}
}

func TestSeedCrawlerCandidateStatusConstraintIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db := newBaselineTestDatabase(t, ctx)
	if err := Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	var run int64
	if err := db.QueryRow(ctx, `insert into seed_crawler_runs default values returning id`).Scan(&run); err != nil {
		t.Fatal(err)
	}
	var id int64
	var initial string
	if err := db.QueryRow(ctx, `insert into seed_crawler_candidates(first_seen_run_id,last_seen_run_id,external_project_id,project_type)
		values($1,$1,'candidate-status-contract','mod') returning id,status`, run).Scan(&id, &initial); err != nil || initial != "candidate" {
		t.Fatalf("candidate initial status=%q, err=%v", initial, err)
	}
	for _, status := range []string{"submitting", "failed", "existing", "draft", "submitted", "draft_unavailable", "candidate"} {
		if _, err := db.Exec(ctx, `update seed_crawler_candidates set status=$2 where id=$1`, id, status); err != nil {
			t.Fatalf("candidate status %q is not supported: %v", status, err)
		}
	}
	_, err := db.Exec(ctx, `update seed_crawler_candidates set status='processing' where id=$1`, id)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.TableName != "seed_crawler_candidates" {
		t.Fatalf("candidate accepted another task domain's status or rejected it for a different reason: %v", err)
	}
}
