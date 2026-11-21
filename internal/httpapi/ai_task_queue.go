package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

const (
	aiTaskRecoveryInterval = time.Minute
	aiTaskStaleAfter       = 15 * time.Minute
	aiTaskRecoveryBatch    = 100
)

type aiTaskExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type aiTaskExecutionKey struct{}

type aiTaskExecution struct {
	ID      int64
	Started time.Time
}

var errAITaskExecutionLost = errors.New("AI task execution was canceled or superseded")
var errAIExecutionPolicy = errors.New("AI task requester or translation source is no longer eligible")

func claimAITaskExecution(ctx context.Context, db *pgxpool.Pool, taskID int64) (aiTaskExecution, error) {
	run := aiTaskExecution{ID: taskID}
	err := db.QueryRow(ctx, `update ai_tasks
		set status='running',started_at=clock_timestamp(),finished_at=null,error='',updated_at=now()
		where id=$1 and status in ('queued','retrying') returning started_at`, taskID).Scan(&run.Started)
	return run, err
}

// The task lock fences recovery. Read current actor and RBAC on the same
// transaction connection before reserving a supplier call or writing a result.
func authorizeAITaskExecutionTx(ctx context.Context, tx pgx.Tx) (security.Claims, error) {
	var actor security.Claims
	run, ok := ctx.Value(aiTaskExecutionKey{}).(aiTaskExecution)
	if !ok {
		return actor, nil // Synchronous crawler budget and direct persistence callers.
	}
	var started *time.Time
	var status, taskType string
	var reserved int64
	if err := tx.QueryRow(ctx, `select status,started_at,task_type,quota_reserved_tokens,coalesce(created_by,0)
		from ai_tasks where id=$1 for update`, run.ID).Scan(&status, &started, &taskType, &reserved, &actor.Subject); err != nil {
		return actor, err
	}
	if status != "running" || started == nil || !started.Equal(run.Started) {
		return actor, errAITaskExecutionLost
	}
	err := tx.QueryRow(ctx, `select public_id from users where id=$1 and status='active' for share`, actor.Subject).Scan(&actor.PublicSubject)
	if errors.Is(err, pgx.ErrNoRows) {
		return actor, errAIExecutionPolicy
	}
	if err != nil {
		return actor, err
	}
	resolved, err := resolveUsersRootPermissionsQuery(ctx, tx, []int64{actor.Subject})
	if err != nil {
		return actor, err
	}
	actor.PermissionRules = resolved[actor.Subject].Permissions
	if claimsExplicitlyAllow(actor, "account.banned") {
		return actor, errAIExecutionPolicy
	}
	// User translation routes reserve a positive allowance in the task row.
	// Generic admin tasks carry no user reservation and retain their enqueue gate.
	if reserved == 0 && !claimsAllow(actor, "ai.task.enqueue") {
		return actor, errAIExecutionPolicy
	}
	if reserved > 0 {
		limit := int64(claimsNumericPermissionValue(actor, "user.ai.daily_token_limit"))
		if limit != int64(maxPermissionValue) && reserved > limit {
			return actor, errAIExecutionPolicy
		}
	}
	switch taskType {
	case aiTaskContentTranslation:
		if !claimsAllow(actor, "content.translate") {
			return actor, errAIExecutionPolicy
		}
	case aiTaskNotificationTranslation:
		if !claimsAllow(actor, "notification.translate") {
			return actor, errAIExecutionPolicy
		}
	default:
		if !claimsAllow(actor, "ai.task.enqueue") {
			return actor, errAIExecutionPolicy
		}
	}
	return actor, nil
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
