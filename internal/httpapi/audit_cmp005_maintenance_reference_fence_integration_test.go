package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// This regression is source-compatible with the pre-fence tool. It schedules
// only an ordinary DB content writer; it performs no second client PUT or
// mutation of upload bytes. The bad path rejects DELETE before any side effect.
func TestAuditCMP005MaintenanceReferenceWriterIsFencedBeforeRawDeleteIntegration(t *testing.T) {
	f, ticket, _, plan := auditOSSByteRawFixture(t)
	if _, err := f.db.Exec(f.ctx, `create table audit_owned_ref_race(body text)`); err != nil {
		t.Fatal(err)
	}
	// Read the new report via its JSON contract so the identical assertion can
	// run against the earlier tool, where the extra reviewed fields are ignored.
	diagnosis, err := RunOSSByteMaintenance(f.ctx, f.db, f.server.cfg, plan, OSSByteMaintenanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(diagnosis)
	var report map[string]any
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(plan)
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["referenceWritesQuiesced"] = true
	fields["referenceWriteTables"] = []string{"oss_files", "audit_owned_ref_race"}
	fields["referenceSchemaSha256"] = strings.Repeat("a", 64)
	quiesced := []string{}
	if scope, ok := report["referenceScope"].(map[string]any); ok {
		fields["referenceSchemaSha256"] = scope["referenceSchemaSha256"]
		if tables, ok := scope["potentialReferenceTables"].([]any); ok {
			for _, table := range tables {
				if name, ok := table.(string); ok && name != "oss_files" && name != "audit_owned_ref_race" {
					quiesced = append(quiesced, name)
				}
			}
		}
	}
	fields["quiescedReferenceTables"] = quiesced
	raw, _ = json.Marshal(fields)
	if err = json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	run := ossMaintenanceRun{s: f.server, plan: plan, opts: OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned synthetic snapshot"}, hasJournal: true}
	_, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	file, proof, err := run.loadRawProof(f.ctx, plan.Entries[0], cfg)
	if err != nil {
		t.Fatal(err)
	}
	token, err := run.claimAction(f.ctx, cfg, plan.Entries[0], file, proof.Key, proof.SourceKey, "processing")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(f.ctx)
	var writerPID int
	if err = writer.QueryRow(f.ctx, `select pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Exec(f.ctx, `insert into audit_owned_ref_race values($1)`, "ordinary manual body retains "+ticket.ObjectKey); err != nil {
		t.Fatal(err)
	}
	atDelete := make(chan struct{})
	var signal sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/"+ticket.ObjectKey) {
			signal.Do(func() { close(atDelete) })
			<-r.Context().Done()
			return
		}
		f.provider.Config.Handler.ServeHTTP(w, r)
	}))
	defer provider.Close()
	ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- run.executeRawCleanup(ctx, plan.Entries[0], token, file, proof, cfg, newOSSClient(cfg, provider.URL, true))
	}()
	consumed := false
	defer func() {
		cancel()
		_ = writer.Rollback(context.Background())
		if !consumed {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("owned maintenance executor did not stop after cancellation")
			}
		}
	}()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	blocked := false
	for !blocked {
		select {
		case <-atDelete:
			// No delete is forwarded by this provider gate. This is a precise pre-fix
			// failure: a currently writing reference table was never fenced.
			t.Fatal("raw DELETE reached provider while the ordinary reference writer was uncommitted; no write-conflict fence preceded the reference scan")
		case err := <-done:
			consumed = true
			t.Fatalf("executor ended before controlled writer barrier: %v", err)
		case <-deadline.C:
			t.Fatal("executor neither acquired the expected write-conflict wait nor reached provider within barrier budget")
		case <-poll.C:
			if err = f.db.QueryRow(f.ctx, `select exists(select 1 from pg_stat_activity where datname=current_database() and $1=any(pg_blocking_pids(pid)) and query ilike '%lock table only%audit_owned_ref_race%')`, writerPID).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = writer.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		consumed = true
	case <-ctx.Done():
		t.Fatal("executor failed to finish after reference writer committed")
	}
	if !errors.Is(err, errOSSMaintenanceConflict) {
		t.Fatalf("committed raw reference was not rejected by the locked rescan: %v", err)
	}
	select {
	case <-atDelete:
		t.Fatal("reference-conflicted cleanup attempted physical DELETE")
	default:
	}
	f.store.mu.Lock()
	_, exists := f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if !exists {
		t.Fatal("ordinary reference conflict lost the retained upload")
	}
	var count int
	var state string
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from audit_owned_ref_race),state from oss_upload_maintenance_actions where id=$1::uuid`, plan.Entries[0].ActionID).Scan(&count, &state); err != nil || count != 1 || state != "processing" {
		t.Fatalf("conflicted executor changed durable writer/intent: count=%d state=%s err=%v", count, state, err)
	}
}
