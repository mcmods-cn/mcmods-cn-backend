// Command diagnose-activity-intents is a bounded, read-only dry run. A missing
// event is a review candidate, never authority to replay rewards or fabricate
// historical activity. There is intentionally no apply mode.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/config"
)

const missingSQL = `select comment.public_id,author.public_id,comment.created_at,
 exists(select 1 from user_task_progress progress where progress.user_id=author.id and progress.rewarded_at is not null),
 exists(select 1 from experience_transactions experience where experience.user_id=author.id),
 exists(select 1 from currency_transactions currency where currency.user_id=author.id)
 from comments comment join users author on author.id=comment.author_id
 join public_routes route on route.entity_type='comment' and route.internal_id=comment.id
 where comment.status='published' and comment.created_at>=$1 and comment.created_at<$2
 and ($3='' or author.public_id=$3)
 and not exists(select 1 from user_activity_events event where event.user_id=author.id and event.object_route_id=route.id and event.action_id=$4 and event.object_type_id=$5)
 and not exists(select 1 from activity_event_outbox intent where intent.user_id=author.id and intent.action_id=$4 and intent.object_type_id=$5
 and (intent.object_route_id=route.id or intent.object_public_id=comment.public_id or (intent.object_entity_type='comment' and intent.object_internal_id=comment.id)))
 order by comment.created_at,comment.id limit $6`

type candidate struct {
	CommentID                 string    `json:"comment_id"`
	AuthorID                  string    `json:"author_id"`
	CreatedAt                 time.Time `json:"created_at"`
	AuthorHasRewardedTask     bool      `json:"author_has_any_rewarded_task"`
	AuthorHasExperienceLedger bool      `json:"author_has_any_experience_ledger"`
	AuthorHasCurrencyLedger   bool      `json:"author_has_any_currency_ledger"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "activity diagnostic failed:", err)
		os.Exit(1)
	}
}
func run() error {
	sinceText := flag.String("since", "", "required inclusive RFC3339 start; at most 31 days")
	untilText := flag.String("until", "", "exclusive RFC3339 end (default now)")
	author := flag.String("author-public-id", "", "optional exact public user identity")
	limit := flag.Int("limit", 1000, "maximum candidates, 1..10000")
	showSQL := flag.Bool("sql", false, "print query for review without connecting")
	flag.Parse()
	if *showSQL {
		fmt.Println(missingSQL)
		return nil
	}
	since, err := time.Parse(time.RFC3339, *sinceText)
	if err != nil {
		return fmt.Errorf("--since must be an explicit RFC3339 value")
	}
	until := time.Now().UTC()
	if *untilText != "" {
		until, err = time.Parse(time.RFC3339, *untilText)
		if err != nil {
			return fmt.Errorf("invalid --until")
		}
	}
	if !since.Before(until) || until.Sub(since) > 31*24*time.Hour || *limit < 1 || *limit > 10000 {
		return fmt.Errorf("require positive interval <=31 days and limit 1..10000")
	}
	if *author != "" {
		if len(*author) != 9 {
			return fmt.Errorf("invalid public author identity")
		}
		for _, c := range *author {
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
				return fmt.Errorf("invalid public author identity")
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		return fmt.Errorf("database configuration unavailable")
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, missingSQL, since, until, *author, activity.ActionCreate, activity.ObjectComment, *limit)
	if err != nil {
		return fmt.Errorf("query activity candidates: %w", err)
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.CommentID, &item.AuthorID, &item.CreatedAt, &item.AuthorHasRewardedTask, &item.AuthorHasExperienceLedger, &item.AuthorHasCurrencyLedger); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	var pending, failed int64
	if err = tx.QueryRow(ctx, `select count(*),count(*) filter(where attempts>0) from activity_event_outbox intent join users author on author.id=intent.user_id where intent.created_at>=$1 and intent.created_at<$2 and ($3='' or author.public_id=$3)`, since, until, *author).Scan(&pending, &failed); err != nil {
		return err
	}
	result := map[string]any{"dry_run": true, "repair_supported": false, "scope": map[string]any{"since": since, "until": until, "author_public_id": *author, "limit": *limit}, "missing_create_activity_candidates": candidates, "result_may_be_truncated": len(candidates) == *limit, "pending_intents_in_scope": pending, "previously_failed_intents_in_scope": failed, "interpretation": "Candidates require immutable business/audit evidence and exact ledger correlation. Author-level ledger flags do not prove this comment was rewarded. Do not fabricate historical activity or replay rewards from absence alone. No balances, ledgers, comments or intents have been changed."}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
