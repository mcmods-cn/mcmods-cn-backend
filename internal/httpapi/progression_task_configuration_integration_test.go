package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/progression"
	"mcmods-cn-backend/internal/security"
)

func TestInvalidActiveTaskConfigurationCannotAdvanceOrRewardIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify task configuration failure safety")
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
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'task-configuration-test',true) returning id`,
		fmt.Sprintf("task-config-%d", suffix), fmt.Sprintf("task-config-%d@example.invalid", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	event := activity.Event{
		UserID: userID, ActionID: activity.ActionCreate, ObjectTypeID: activity.ObjectUser,
		OccurredAt: time.Now().UTC(),
	}
	service := progression.NewService(pool, nil)
	server := &Server{db: pool}

	invalidConditionTask := insertTaskConfigurationFixture(t, ctx, pool, suffix, "condition",
		`{"action":"create","objectType":"user","metric":"count","target":"one"}`,
		`{"experience":1,"currencies":{}}`)
	if err = service.ProcessActivityBatch(ctx, []activity.Event{event}); err == nil {
		t.Fatal("invalid active task condition was silently skipped")
	}
	assertTaskRewardState(t, ctx, pool, userID, invalidConditionTask, 0, 0, 0)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil).WithContext(
		context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}),
	)
	response := httptest.NewRecorder()
	server.userTasks(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("user task invalid condition status=%d body=%s; want 500", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	server.adminTasks(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/tasks", nil).WithContext(ctx))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("admin task invalid condition status=%d body=%s; want 500", response.Code, response.Body.String())
	}
	deleteTaskConfigurationFixture(t, ctx, pool, invalidConditionTask)

	invalidRewardsTask := insertTaskConfigurationFixture(t, ctx, pool, suffix, "rewards",
		`{"action":"create","objectType":"user","metric":"count","target":1}`, `{}`)
	if err = service.ProcessActivityBatch(ctx, []activity.Event{event}); err == nil {
		t.Fatal("empty active task rewards were interpreted as a successful reward")
	}
	assertTaskRewardState(t, ctx, pool, userID, invalidRewardsTask, 0, 0, 0)
	deleteTaskConfigurationFixture(t, ctx, pool, invalidRewardsTask)

	unknownCurrencyTask := insertTaskConfigurationFixture(t, ctx, pool, suffix, "currency",
		`{"action":"create","objectType":"user","metric":"count","target":1}`,
		`{"experience":0,"currencies":{"arch014_missing":1}}`)
	if err = service.ProcessActivityBatch(ctx, []activity.Event{event}); err == nil {
		t.Fatal("unavailable active task reward currency was not rejected at the enabled-task gate")
	}
	assertTaskRewardState(t, ctx, pool, userID, unknownCurrencyTask, 0, 0, 0)
	deleteTaskConfigurationFixture(t, ctx, pool, unknownCurrencyTask)

	validTask := insertTaskConfigurationFixture(t, ctx, pool, suffix, "valid",
		`{"action":"create","objectType":"user","metric":"count","target":1}`,
		`{"experience":1,"currencies":{}}`)
	if err = service.ProcessActivityBatch(ctx, []activity.Event{event}); err != nil {
		t.Fatalf("valid active task: %v", err)
	}
	assertTaskRewardState(t, ctx, pool, userID, validTask, 1, 1, 1)
	var experience int64
	if err = pool.QueryRow(ctx, `select experience from user_experience where user_id=$1`, userID).Scan(&experience); err != nil || experience != 1 {
		t.Fatalf("valid experience=%d err=%v; want 1/nil", experience, err)
	}
}

func insertTaskConfigurationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix int64, label, condition, rewards string) int64 {
	t.Helper()
	var taskID int64
	if err := pool.QueryRow(ctx, `insert into task_definitions(code,name,condition,rewards,status)
		values($1,$2,$3::jsonb,$4::jsonb,'active') returning id`,
		fmt.Sprintf("task-config-%s-%d", label, suffix), label, condition, rewards).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	return taskID
}

func deleteTaskConfigurationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `delete from task_definitions where id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
}

func assertTaskRewardState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, taskID, wantProgress, wantCompleted, wantRewarded int64) {
	t.Helper()
	var progress, completed, rewarded int64
	if err := pool.QueryRow(ctx, `select coalesce(max(progress),0),count(completed_at),count(rewarded_at)
		from user_task_progress where user_id=$1 and task_id=$2`, userID, taskID).Scan(&progress, &completed, &rewarded); err != nil {
		t.Fatal(err)
	}
	if progress != wantProgress || completed != wantCompleted || rewarded != wantRewarded {
		t.Fatalf("task reward state=(%d,%d,%d); want (%d,%d,%d)", progress, completed, rewarded, wantProgress, wantCompleted, wantRewarded)
	}
}
