package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProjectAutomationLeaseFencingAndRecoveryCoreIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	var projectID, routeID, settingID, runID int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'synthetic-lease','Synthetic automation project','approved') returning id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled) values($1,'changelog','modrinth','week',true) returning id`, routeID).Scan(&settingID); err != nil {
		t.Fatal(err)
	}
	token := randomHex(16)
	if err := pool.QueryRow(ctx, `insert into project_auto_update_runs(setting_id,status,lease_owner,lease_expires_at,attempts) values($1,'running',$2,clock_timestamp()+interval '1 minute',1) returning id`, settingID, token).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := lockProjectAutomationLeaseTx(ctx, nil); err != nil {
		t.Fatalf("ordinary request was assigned an automation lease: %v", err)
	}
	if err := lockProjectAutomationLeaseTx(withProjectAutomationLease(ctx, 0, ""), nil); !errors.Is(err, errProjectAutomationLeaseLost) {
		t.Fatalf("malformed lease did not fail closed: %v", err)
	}
	leased := withProjectAutomationLease(ctx, runID, token)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err = lockProjectAutomationLeaseTx(leased, tx); err != nil {
		t.Fatal(err)
	}
	if err = refreshProjectAutomationLease(ctx, pool, runID, token); err != nil {
		t.Fatalf("heartbeat blocked on its own fenced transaction: %v", err)
	}
	replacement := randomHex(16)
	replaced := make(chan error, 1)
	pid := make(chan int, 1)
	go func() {
		connection, e := pool.Acquire(ctx)
		if e != nil {
			replaced <- e
			return
		}
		defer connection.Release()
		var backendPID int
		if e = connection.QueryRow(ctx, `select pg_backend_pid()`).Scan(&backendPID); e != nil {
			replaced <- e
			return
		}
		pid <- backendPID
		_, e = connection.Exec(ctx, `update project_auto_update_runs set lease_owner=$2 where id=$1`, runID, replacement)
		replaced <- e
	}()
	var backendPID int
	select {
	case backendPID = <-pid:
	case err = <-replaced:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err = pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where pid=$1 and wait_event_type='Lock')`, backendPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err = <-replaced:
			t.Fatalf("replacement passed the publication lock: %v", err)
		case <-deadline.C:
			t.Fatal("replacement was not observed waiting on row lock")
		case <-ticker.C:
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-replaced; err != nil {
		t.Fatal(err)
	}
	if err = refreshProjectAutomationLease(ctx, pool, runID, token); !errors.Is(err, errProjectAutomationLeaseLost) {
		t.Fatalf("old worker refreshed replacement lease: %v", err)
	}
	late, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = lockProjectAutomationLeaseTx(leased, late); !errors.Is(err, errProjectAutomationLeaseLost) {
		t.Fatalf("late worker passed fence: %v", err)
	}
	if err = late.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = refreshProjectAutomationLease(ctx, pool, runID, replacement); err != nil {
		t.Fatal(err)
	}
	var renewed bool
	if err = pool.QueryRow(ctx, `select lease_expires_at>clock_timestamp()+interval '9 minutes' from project_auto_update_runs where id=$1`, runID).Scan(&renewed); err != nil || !renewed {
		t.Fatalf("10-minute lease was not renewed: %t %v", renewed, err)
	}
	if _, err = pool.Exec(ctx, `update project_auto_update_runs set lease_expires_at=clock_timestamp()-interval '1 second' where id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	if err = refreshProjectAutomationLease(ctx, pool, runID, replacement); !errors.Is(err, errProjectAutomationLeaseLost) {
		t.Fatalf("expired lease was resurrected: %v", err)
	}
	if err = recoverExpiredProjectAutomationRuns(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var status, currentToken string
	if err = pool.QueryRow(ctx, `select status,lease_owner from project_auto_update_runs where id=$1`, runID).Scan(&status, &currentToken); err != nil || status != "pending" || currentToken != "" {
		t.Fatalf("expired run not recovered: %s %s %v", status, currentToken, err)
	}
	if _, err = pool.Exec(ctx, `update project_auto_update_runs set status='running',lease_owner=$2,lease_expires_at=null,attempts=5 where id=$1`, runID, token); err != nil {
		t.Fatal(err)
	}
	if err = recoverExpiredProjectAutomationRuns(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select status from project_auto_update_runs where id=$1`, runID).Scan(&status); err != nil || status != "dead_letter" {
		t.Fatalf("exhausted crashed run retries indefinitely: %s %v", status, err)
	}
	heartbeatCtx, stop := maintainProjectAutomationLeaseInterval(ctx, pool, runID, token, time.Millisecond)
	defer stop()
	select {
	case <-heartbeatCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("lost lease did not cancel worker context")
	}
}

func TestProjectAutomationHeartbeatParentCancellationCore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	derived, stop := maintainProjectAutomationLease(ctx, nil, 1, "synthetic")
	cancel()
	stop()
	if !errors.Is(derived.Err(), context.Canceled) {
		t.Fatalf("parent cancellation did not stop joined heartbeat: %v", derived.Err())
	}
}
