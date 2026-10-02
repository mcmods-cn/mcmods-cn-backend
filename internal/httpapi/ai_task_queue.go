package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mcmods-cn-backend/internal/queue"
)

const (
	aiTaskRecoveryInterval = time.Minute
	aiTaskStaleAfter       = 15 * time.Minute
	aiTaskRecoveryBatch    = 100
)

type aiTaskExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func enqueueAITaskTx(ctx context.Context, tx pgx.Tx, eventType string, taskID int64, taskUID, taskType, traceID string) error {
	_, err := queue.EnqueueTx(ctx, tx, "ai", eventType, "ai_task", taskUID, traceID, aiTaskMessage{
		TaskID: taskID, TaskUID: taskUID, TaskType: taskType,
	})
	return err
}

func claimAITaskForExecution(ctx context.Context, executor aiTaskExecutor, taskID int64) (bool, error) {
	tag, err := executor.Exec(ctx, `update ai_tasks
		set status='running',started_at=coalesce(started_at,now()),finished_at=null,error='',updated_at=now()
		where id=$1 and status in ('queued','retrying')`, taskID)
	return err == nil && tag.RowsAffected() == 1, err
}

type recoverableAITask struct {
	ID       int64
	UID      string
	TaskType string
	Status   string
}

func recoverAITaskOutboxTx(ctx context.Context, tx pgx.Tx, staleAfter time.Duration, limit int) (int, error) {
	if staleAfter <= 0 {
		staleAfter = aiTaskStaleAfter
	}
	if limit < 1 || limit > 500 {
		limit = aiTaskRecoveryBatch
	}
	rows, err := tx.Query(ctx, `select task.id,task.task_uid,task.task_type,task.status
		from ai_tasks task
		where (task.status in ('queued','retrying') and not exists(
			select 1 from nats_outbox event where event.aggregate_type='ai_task' and event.aggregate_id=task.task_uid
		)) or (task.status='running' and task.updated_at<now()-$1::interval)
		order by task.updated_at,task.id for update of task skip locked limit $2`, fmt.Sprintf("%f seconds", staleAfter.Seconds()), limit)
	if err != nil {
		return 0, err
	}
	tasks := make([]recoverableAITask, 0, limit)
	for rows.Next() {
		var task recoverableAITask
		if err = rows.Scan(&task.ID, &task.UID, &task.TaskType, &task.Status); err != nil {
			rows.Close()
			return 0, err
		}
		tasks = append(tasks, task)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, task := range tasks {
		if task.Status == "running" {
			if _, err = tx.Exec(ctx, `update ai_tasks set status='retrying',started_at=null,finished_at=null,
				error='',queued_at=now(),updated_at=now() where id=$1 and status='running'`, task.ID); err != nil {
				return 0, err
			}
		}
		if err = enqueueAITaskTx(ctx, tx, "ai.task.recovered", task.ID, task.UID, task.TaskType, ""); err != nil {
			return 0, err
		}
	}
	return len(tasks), nil
}
