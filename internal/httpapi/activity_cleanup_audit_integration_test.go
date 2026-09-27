package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestActivityCleanupProgressAndAuditRepairStayConsistentIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify activity cleanup audit recovery")
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
		values($1,$2,'activity-cleanup-audit-test',true) returning id`,
		fmt.Sprintf("activity-cleanup-audit-%d", suffix), fmt.Sprintf("activity-cleanup-audit-%d@example.invalid", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at)
		values($1,3,7,now()-interval '3 days'),($1,3,7,now()-interval '2 days'),($1,3,7,now()-interval '1 day')`, userID); err != nil {
		t.Fatal(err)
	}
	filter := normalizedActivityCleanupFilter{
		UserIDs: []int64{userID}, ActionIDs: []int16{3}, SnapshotBefore: time.Now().UTC().Add(time.Minute), BatchSize: 1,
		Summary: activityCleanupFilterRequest{Actions: []string{"view"}},
	}
	rawFilter, err := json.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	if err = pool.QueryRow(ctx, `insert into activity_cleanup_runs(source,status,filters,started_at)
		values('manual','running',$1::jsonb,now()-interval '2 minutes') returning public_id`, rawFilter).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table activity_cleanup_runs add constraint reject_second_progress check(deleted_count<=1)`); err != nil {
		t.Fatal(err)
	}
	if deleted, deleteErr := deleteActivityEventsInBatches(ctx, pool, runID, filter); deleteErr == nil || deleted != 1 {
		t.Fatalf("injected progress failure deleted=%d err=%v; want 1/error", deleted, deleteErr)
	}
	assertActivityCleanupAuditState(t, ctx, pool, runID, userID, "running", 1, 2)

	if _, err = pool.Exec(ctx, `alter table activity_cleanup_runs drop constraint reject_second_progress`); err != nil {
		t.Fatal(err)
	}
	if deleted, deleteErr := deleteActivityEventsInBatches(ctx, pool, runID, filter); deleteErr != nil || deleted != 2 {
		t.Fatalf("resumed cleanup deleted=%d err=%v; want 2/nil", deleted, deleteErr)
	}
	assertActivityCleanupAuditState(t, ctx, pool, runID, userID, "running", 3, 0)

	if _, err = pool.Exec(ctx, `alter table activity_cleanup_runs add constraint reject_completed_audit check(status<>'completed')`); err != nil {
		t.Fatal(err)
	}
	if _, err = finalizeActivityCleanupRun(ctx, pool, runID, "completed", ""); err == nil {
		t.Fatal("injected final audit failure was hidden")
	}
	assertActivityCleanupAuditState(t, ctx, pool, runID, userID, "running", 3, 0)
	if _, err = pool.Exec(ctx, `alter table activity_cleanup_runs drop constraint reject_completed_audit`); err != nil {
		t.Fatal(err)
	}
	repairActivityCleanupRuns(ctx, pool)
	assertActivityCleanupAuditState(t, ctx, pool, runID, userID, "completed", 3, 0)
}

func assertActivityCleanupAuditState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID string, userID int64, wantStatus string, wantDeleted, wantEvents int64) {
	t.Helper()
	var status string
	var deleted, events int64
	if err := pool.QueryRow(ctx, `select status,deleted_count,
		(select count(*) from user_activity_events where user_id=$2)
		from activity_cleanup_runs where public_id=$1`, runID, userID).Scan(&status, &deleted, &events); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || deleted != wantDeleted || events != wantEvents {
		t.Fatalf("cleanup audit status=%s deleted=%d events=%d; want %s/%d/%d",
			status, deleted, events, wantStatus, wantDeleted, wantEvents)
	}
}
