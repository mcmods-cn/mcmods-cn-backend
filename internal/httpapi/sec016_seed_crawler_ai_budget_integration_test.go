package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestSEC016SeedCrawlerConcurrentBudgetReservationsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify concurrent seed crawler AI budget reservations")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	schemaName := fmt.Sprintf("sec016_seed_ai_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, dropErr := adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated SEC-016 schema: %v", dropErr)
		}
	}()

	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.MaxConns = 4
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create table seed_crawler_translation_tasks (
		candidate_id bigint not null,
		locale text not null,
		status text not null default 'pending',
		attempts integer not null default 0,
		input_tokens bigint not null default 0,
		output_tokens bigint not null default 0,
		usage_date date not null default current_date,
		quota_reserved_tokens bigint not null default 0,
		last_error text not null default '',
		updated_at timestamptz not null default now(),
		primary key(candidate_id,locale),
		check(status in ('pending','running','completed','failed')),
		check(input_tokens>=0), check(output_tokens>=0), check(quota_reserved_tokens>=0));
		create index idx_seed_crawler_translation_tasks_daily_budget on seed_crawler_translation_tasks(usage_date)
			include(input_tokens,output_tokens,quota_reserved_tokens);`); err != nil {
		t.Fatal(err)
	}
	worker := NewSeedCrawlerWorker(config.Load(), pool)
	type result struct {
		candidateID int64
		reservation seedTranslationBudgetReservation
		allowed     bool
		err         error
	}
	results := make(chan result, 2)
	for candidateID := int64(1); candidateID <= 2; candidateID++ {
		candidateID := candidateID
		go func() {
			reservation, allowed, reserveErr := worker.reserveSeedTranslationBudget(ctx, candidateID, "en-US", 100, 60)
			results <- result{candidateID: candidateID, reservation: reservation, allowed: allowed, err: reserveErr}
		}()
	}
	var winner, loser int64
	var winnerReservation seedTranslationBudgetReservation
	for range 2 {
		item := <-results
		if item.err != nil {
			t.Fatalf("concurrent reservation: %v", item.err)
		}
		if item.allowed {
			if winner != 0 {
				t.Fatal("both concurrent requests reserved a budget that only fits one")
			}
			winner, winnerReservation = item.candidateID, item.reservation
		} else {
			loser = item.candidateID
		}
	}
	if winner == 0 || loser == 0 {
		t.Fatalf("winner/loser=%d/%d", winner, loser)
	}
	var reserved int64
	if err = pool.QueryRow(ctx, `select coalesce(sum(quota_reserved_tokens),0) from seed_crawler_translation_tasks where usage_date=current_date`).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != 60 {
		t.Fatalf("reserved tokens=%d, want 60", reserved)
	}

	if err = worker.settleSeedTranslationBudget(ctx, winnerReservation, "completed", aiTaskUsage{InputTokens: 20, OutputTokens: 20}, ""); err != nil {
		t.Fatal(err)
	}
	retryReservation, allowed, err := worker.reserveSeedTranslationBudget(ctx, winner, "en-US", 100, 60)
	if err != nil || !allowed {
		t.Fatalf("same-task retry reservation after settlement allowed=%t err=%v", allowed, err)
	}
	if err = worker.settleSeedTranslationBudget(ctx, retryReservation, "completed", aiTaskUsage{InputTokens: 5, OutputTokens: 5}, ""); err != nil {
		t.Fatal(err)
	}
	var cumulative int64
	if err = pool.QueryRow(ctx, `select input_tokens+output_tokens from seed_crawler_translation_tasks where candidate_id=$1 and locale='en-US'`, winner).Scan(&cumulative); err != nil {
		t.Fatal(err)
	}
	if cumulative != 50 {
		t.Fatalf("same-day retry replaced earlier usage: cumulative=%d, want 50", cumulative)
	}
	secondReservation, allowed, err := worker.reserveSeedTranslationBudget(ctx, loser, "en-US", 100, 50)
	if err != nil || !allowed {
		t.Fatalf("reservation at exact remaining budget allowed=%t err=%v", allowed, err)
	}
	if _, allowed, err = worker.reserveSeedTranslationBudget(ctx, 3, "en-US", 100, 1); err != nil || allowed {
		t.Fatalf("over-budget reservation allowed=%t err=%v", allowed, err)
	}
	if err = worker.settleSeedTranslationBudget(ctx, secondReservation, "failed", aiTaskUsage{InputTokens: 5, OutputTokens: 5}, "invalid result"); err != nil {
		t.Fatal(err)
	}
	uncertainReservation, allowed, err := worker.reserveSeedTranslationBudget(ctx, 3, "en-US", 100, 40)
	if err != nil || !allowed {
		t.Fatalf("known usage did not release the unused reservation: allowed=%t err=%v", allowed, err)
	}
	if err = worker.settleSeedTranslationBudget(ctx, uncertainReservation, "completed", aiTaskUsage{}, ""); err != nil {
		t.Fatal(err)
	}
	if _, allowed, err = worker.reserveSeedTranslationBudget(ctx, 4, "en-US", 100, 1); err != nil || allowed {
		t.Fatalf("successful response without usage reopened the hard budget: allowed=%t err=%v", allowed, err)
	}
	if _, err = pool.Exec(ctx, `insert into seed_crawler_translation_tasks(candidate_id,locale,status,input_tokens,output_tokens,usage_date)
		select 1000+value,'plan','completed',10,5,current_date-((value%30)+1)::int from generate_series(1,100000) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `vacuum analyze seed_crawler_translation_tasks`); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `explain (analyze,buffers,costs off) select coalesce(sum(input_tokens+output_tokens+quota_reserved_tokens),0)
		from seed_crawler_translation_tasks where usage_date=current_date`)
	if err != nil {
		t.Fatal(err)
	}
	planLines := make([]string, 0, 8)
	for rows.Next() {
		var line string
		if scanErr := rows.Scan(&line); scanErr != nil {
			rows.Close()
			t.Fatal(scanErr)
		}
		planLines = append(planLines, line)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	plan := strings.ToLower(strings.Join(planLines, "\n"))
	if !strings.Contains(plan, "idx_seed_crawler_translation_tasks_daily_budget") || strings.Contains(plan, "seq scan") {
		t.Fatalf("100k daily budget plan did not use the covering date index:\n%s", plan)
	}
	t.Logf("100k daily budget plan:\n%s", plan)

	if _, err = pool.Exec(ctx, `alter table seed_crawler_translation_tasks rename to sec016_broken_translation_tasks`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = worker.reserveSeedTranslationBudget(ctx, 5, "en-US", 100, 1); err == nil {
		t.Fatal("budget reservation failed open when PostgreSQL usage state was unavailable")
	}
}
