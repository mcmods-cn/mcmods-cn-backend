package progression

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestActivityProgressionParticipatesInCallerTransactionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify transactional activity progression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var userID, taskID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'progression-transaction-test',true) returning id`,
		fmt.Sprintf("progression-tx-%d", suffix), fmt.Sprintf("progression-tx-%d@example.invalid", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into task_definitions(code,name,condition,rewards,status)
		values($1,'Transactional progression',
		'{"action":"create","objectType":"user","metric":"count","target":1}'::jsonb,
		'{"experience":1,"currencies":{}}'::jsonb,'active') returning id`, fmt.Sprintf("progression-tx-%d", suffix)).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	event := activity.Event{
		UserID: userID, ActionID: activity.ActionCreate, ObjectTypeID: activity.ObjectUser,
		OccurredAt: time.Now().UTC(),
	}
	service := NewService(pool, nil)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ProcessActivityBatchTx(ctx, tx, []activity.Event{event}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertTaskProgress(t, ctx, pool, userID, taskID, 0)

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ProcessActivityBatchTx(ctx, tx, []activity.Event{event}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	service.ActivityBatchCommitted(ctx, []activity.Event{event})
	assertTaskProgress(t, ctx, pool, userID, taskID, 1)
}

func assertTaskProgress(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, taskID int64, want int64) {
	t.Helper()
	var progress int64
	if err := pool.QueryRow(ctx, `select coalesce(max(progress),0) from user_task_progress
		where user_id=$1 and task_id=$2`, userID, taskID).Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if progress != want {
		t.Fatalf("task progress=%d; want %d", progress, want)
	}
}
