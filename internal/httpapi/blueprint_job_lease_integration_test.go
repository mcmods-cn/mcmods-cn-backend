package httpapi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestBlueprintJobLeaseRecoveryAndLateWorkerFencingIntegration(t *testing.T) {
	s, owner := ossUploadTestServer(t, 1024)
	worker := &BlueprintWorker{db: s.db, api: s}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var blueprintID, jobID int64
	if err := s.db.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format,status)
		values($1,'synthetic lease fixture','json','ready') returning id`, owner.Subject).Scan(&blueprintID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.db.Exec(context.Background(), `delete from blueprints where id=$1`, blueprintID); err != nil {
			t.Error(err)
		}
	})
	if err := s.db.QueryRow(ctx, `insert into blueprint_jobs(blueprint_id,operation) values($1,'normalize') returning id`, blueprintID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		claim blueprintJobClaim
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var competitors sync.WaitGroup
	for range 2 {
		competitors.Add(1)
		go func() {
			defer competitors.Done()
			<-start
			claim, err := worker.claimBlueprintJob(ctx, jobID)
			results <- result{claim, err}
		}()
	}
	close(start)
	competitors.Wait()
	close(results)
	var original blueprintJobClaim
	winners, conflicts := 0, 0
	for result := range results {
		if result.err == nil {
			winners++
			original = result.claim
		} else if errors.Is(result.err, pgx.ErrNoRows) {
			conflicts++
		} else {
			t.Fatal(result.err)
		}
	}
	if winners != 1 || conflicts != 1 || original.RunToken == "" {
		t.Fatalf("uncontrolled concurrent claim: winners=%d conflicts=%d", winners, conflicts)
	}
	// Simulate a crashed process by ageing its durable heartbeat, with no live
	// heartbeat goroutine. The restarted worker recovers and claims a new token.
	if _, err := s.db.Exec(ctx, `update blueprint_jobs set heartbeat_at=now()-interval '6 minutes' where id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := worker.recoverStaleBlueprintJobs(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := worker.claimBlueprintJob(ctx, jobID)
	if err != nil || restarted.RunToken == original.RunToken || restarted.RunToken == "" {
		t.Fatalf("crashed job was not recovered with a fresh token: err=%v", err)
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, original.RunToken, 99); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("late progress overwrote a new claim: %v", err)
	}
	if err = worker.normalizeBlueprint(ctx, jobID, original.RunToken, blueprintID, 0); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("late worker resumed publication: %v", err)
	}
	if err = worker.finishBlueprintJob(ctx, jobID, original.RunToken, errors.New("old process failed")); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("late failure overwrote a new claim: %v", err)
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, restarted.RunToken, 90); err != nil {
		t.Fatal(err)
	}
	if err = worker.finishBlueprintJob(ctx, jobID, restarted.RunToken, nil); err != nil {
		t.Fatal(err)
	}
	var status, blueprintStatus, runToken string
	if err = s.db.QueryRow(ctx, `select job.status,job.run_token,blueprint.status
		from blueprint_jobs job join blueprints blueprint on blueprint.id=job.blueprint_id where job.id=$1`, jobID).Scan(&status, &runToken, &blueprintStatus); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || runToken != "" || blueprintStatus != "ready" {
		t.Fatalf("completed claim was corrupted: job=%s resource=%s tokenCleared=%v", status, blueprintStatus, runToken == "")
	}
	if err = worker.finishBlueprintJob(ctx, jobID, original.RunToken, nil); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("duplicate completion was accepted: %v", err)
	}
}

func TestBlueprintWorkerDoesNotRestoreDeletedResourceOrInvalidateReadyFormatsIntegration(t *testing.T) {
	s, owner := ossUploadTestServer(t, 1024)
	worker := &BlueprintWorker{db: s.db, api: s}
	ctx := context.Background()
	var blueprintID, normalizeJobID, convertJobID int64
	if err := s.db.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format,status)
		values($1,'synthetic deleted fixture','json','ready') returning id`, owner.Subject).Scan(&blueprintID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.db.Exec(ctx, `delete from blueprints where id=$1`, blueprintID); err != nil {
			t.Error(err)
		}
	})
	if err := s.db.QueryRow(ctx, `insert into blueprint_jobs(blueprint_id,operation) values($1,'convert') returning id`, blueprintID).Scan(&convertJobID); err != nil {
		t.Fatal(err)
	}
	convertClaim, err := worker.claimBlueprintJob(ctx, convertJobID)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.finishBlueprintJob(ctx, convertJobID, convertClaim.RunToken, errors.New("unsupported format fixture")); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = s.db.QueryRow(ctx, `select status from blueprints where id=$1`, blueprintID).Scan(&status); err != nil || status != "ready" {
		t.Fatalf("one failed conversion made ready formats unusable: status=%s err=%v", status, err)
	}
	if err = s.db.QueryRow(ctx, `insert into blueprint_jobs(blueprint_id,operation) values($1,'normalize') returning id`, blueprintID).Scan(&normalizeJobID); err != nil {
		t.Fatal(err)
	}
	claim, err := worker.claimBlueprintJob(ctx, normalizeJobID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(ctx, `update blueprints set status='deleted' where id=$1`, blueprintID); err != nil {
		t.Fatal(err)
	}
	failure := worker.normalizeBlueprint(ctx, normalizeJobID, claim.RunToken, blueprintID, 0)
	if !errors.Is(failure, pgx.ErrNoRows) {
		t.Fatalf("deleted resource was processed: %v", failure)
	}
	if err = worker.finishBlueprintJob(ctx, normalizeJobID, claim.RunToken, failure); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(ctx, `select status from blueprints where id=$1`, blueprintID).Scan(&status); err != nil || status != "deleted" {
		t.Fatalf("worker restored a deleted resource: status=%s err=%v", status, err)
	}
}

func TestBlueprintWorkerCrashRecoveryHasTerminalLimitIntegration(t *testing.T) {
	s, owner := ossUploadTestServer(t, 1024)
	worker := &BlueprintWorker{db: s.db, api: s}
	ctx := context.Background()
	var blueprintID, jobID int64
	if err := s.db.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format,status) values($1,'synthetic exhausted lease','json','processing') returning id`, owner.Subject).Scan(&blueprintID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.db.Exec(context.Background(), `delete from blueprints where id=$1`, blueprintID); err != nil {
			t.Error(err)
		}
	})
	if err := s.db.QueryRow(ctx, `insert into blueprint_jobs(blueprint_id,operation,status,attempts,run_token,heartbeat_at)
		values($1,'normalize','processing',3,'crashed-worker',now()-interval '6 minutes') returning id`, blueprintID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err := worker.recoverStaleBlueprintJobs(ctx); err != nil {
		t.Fatal(err)
	}
	var jobStatus, resourceStatus string
	if err := s.db.QueryRow(ctx, `select job.status,blueprint.status from blueprint_jobs job join blueprints blueprint on blueprint.id=job.blueprint_id where job.id=$1`, jobID).Scan(&jobStatus, &resourceStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" || resourceStatus != "failed" {
		t.Fatalf("crashed job did not reach terminal state: job=%s resource=%s", jobStatus, resourceStatus)
	}
	if _, err := worker.claimBlueprintJob(ctx, jobID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("exhausted crashed job was claimed again: %v", err)
	}
}
