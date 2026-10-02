package httpapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBlueprintEntityFidelityFailureIsTerminalOnFirstAttemptIntegration(t *testing.T) {
	pool := openBlueprintJobStateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into blueprints(id,public_id,owner_id,status,title) values
		(76,'fidelity1',7,'ready','Lossless Original'),
		(77,'fidelity2',7,'processing','Lossy Source');
		insert into blueprint_jobs(id,public_id,blueprint_id,operation,target_format,status,max_attempts) values
		(76,'fidelity3',76,'convert','schem','queued',3),
		(77,'fidelity4',77,'normalize','','queued',3)`); err != nil {
		t.Fatal(err)
	}
	worker := &BlueprintWorker{db: pool, api: &Server{db: pool}, workerID: "fidelity-worker"}

	for _, testCase := range []struct {
		jobID         int64
		blueprintID   int64
		wantBlueprint string
	}{
		{jobID: 76, blueprintID: 76, wantBlueprint: "ready"},
		{jobID: 77, blueprintID: 77, wantBlueprint: "failed"},
	} {
		job, claimed, err := worker.claimBlueprintJob(ctx, testCase.jobID)
		if err != nil || !claimed {
			t.Fatalf("claim job %d: %t/%v", testCase.jobID, claimed, err)
		}
		terminal, err := worker.failBlueprintJob(ctx, job, fmt.Errorf("%w: unsupported entity payload", errBlueprintEntityDataWouldBeLost))
		if err != nil || !terminal {
			t.Fatalf("fidelity failure for job %d = terminal %t/error %v", testCase.jobID, terminal, err)
		}
		assertBlueprintJobLeaseState(t, ctx, pool, testCase.jobID, "failed", "", nil, 1)
		assertBlueprintStatus(t, ctx, pool, testCase.blueprintID, testCase.wantBlueprint)
		var lastError string
		if err = pool.QueryRow(ctx, `select last_error from blueprint_jobs where id=$1`, testCase.jobID).Scan(&lastError); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(lastError, "entity") {
			t.Fatalf("job %d last_error = %q, want explicit entity fidelity reason", testCase.jobID, lastError)
		}
	}
}
