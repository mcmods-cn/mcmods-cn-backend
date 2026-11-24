package httpapi

import (
	"errors"
	"testing"
	"time"
)

func TestGalleryRehomeSettlementsHonorTheLeaseAfterWaitingIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	worker := &OSSRehomeWorker{server: f.origin.Config.Handler.(*Server), workerID: "gallery-rehome-first"}
	if _, err := f.db.Exec(f.ctx, `insert into oss_rehome_jobs(mod_id) values($1)
		on conflict(mod_id) do update set status='queued',attempts=0,next_attempt_at=now()`, f.modID); err != nil {
		t.Fatal(err)
	}
	job, err := worker.claim(f.ctx)
	if err != nil || job.ModID != f.modID {
		t.Fatalf("claim: %#v %v", job, err)
	}
	var deadline time.Time
	if err = f.db.QueryRow(f.ctx, `update oss_rehome_jobs set lease_expires_at=clock_timestamp()+interval '2 seconds'
		where id=$1 returning lease_expires_at`, job.ID).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	holder, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(f.ctx)
	if _, err = holder.Exec(f.ctx, `select id from oss_rehome_jobs where id=$1 for no key update`, job.ID); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { _, err := worker.complete(f.ctx, job); results <- err }()
	go func() {
		_, err := worker.recordFailure(f.ctx, job, errors.New("fixture gallery copy failed"))
		results <- err
	}()
	auditCompletionAwaitLocks(t, f, 2)
	var beforeDeadline bool
	if err = holder.QueryRow(f.ctx, "select clock_timestamp()<$1::timestamptz", deadline).Scan(&beforeDeadline); err != nil || !beforeDeadline {
		t.Fatalf("both settlement attempts must be waiting before the lease expires: %v", err)
	}
	if _, err = holder.Exec(f.ctx, `select pg_sleep(greatest(0, extract(epoch from $1::timestamptz-clock_timestamp())))`, deadline); err != nil {
		t.Fatal(err)
	}
	if err = holder.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = <-results; !errors.Is(err, errOSSRehomeLeaseLost) {
			t.Errorf("expired settlement result: %v", err)
		}
	}
	newOwner := &OSSRehomeWorker{server: worker.server, workerID: "gallery-rehome-restarted"}
	retry, err := newOwner.claim(f.ctx)
	if err != nil || retry.ID != job.ID || retry.LockToken == job.LockToken || retry.Attempts != job.Attempts+1 {
		t.Fatalf("fresh claimant: %#v %v", retry, err)
	}
	if status, err := newOwner.complete(f.ctx, retry); err != nil || status != "completed" {
		t.Fatalf("fresh settlement: %s %v", status, err)
	}
}
