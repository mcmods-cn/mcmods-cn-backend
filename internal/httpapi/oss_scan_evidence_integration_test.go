package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestScanWaitsForReportEvidenceRetentionBeforeLockingItsFileIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
	var fileID int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(object_key,bucket,endpoint,region,source,uploader_id)
		values('synthetic/report','owned','https://oss.example.test','local','report_evidence',$1)
		returning id,public_id`, f.userIDs[f.editor]).Scan(&fileID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into report_evidence(uploader_id,object_key,original_name,content_type,byte_size,sha256,cleanup_after)
		values($1,'synthetic/report','sample.txt','text/plain',1,repeat('a',64),now()-interval '1 hour')`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	retention, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer retention.Rollback(context.Background())
	if _, err = retention.Exec(f.ctx, `select id from report_evidence where object_key='synthetic/report' for update`); err != nil {
		t.Fatal(err)
	}

	finished := make(chan test037HTTPResult, 1)
	go func() {
		status, raw, err := test048HTTPRequest(f.ctx, f.origin.Client(), f.origin.URL, f.editor, http.MethodPatch,
			"/api/v1/admin/oss/files/"+publicID+"/scan", `{"status":"clean"}`)
		finished <- test037HTTPResult{status, raw, err}
	}()
	wait, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var blocked bool
		if err := f.db.QueryRow(wait, `select exists(select 1 from pg_stat_activity
			where datname=current_database() and $1=any(pg_blocking_pids(pid)))`, int32(retention.Conn().PgConn().PID())).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case res := <-finished:
			t.Fatalf("scan ended before retention released its claim: %d %v", res.status, res.err)
		case <-wait.Done():
			t.Fatal(wait.Err())
		case <-tick.C:
		}
	}
	// The scanner is waiting on the claimed evidence. Retention must be free
	// to lock and retire the file, then the scanner must see that retirement.
	if _, err = retention.Exec(f.ctx, `select id from oss_files where id=$1 for update nowait`, fileID); err != nil {
		t.Fatal(err)
	}
	if err = f.server.tombstoneOSSFileTx(f.ctx, retention, fileID, "report_evidence_retention_expired"); err != nil {
		t.Fatal(err)
	}
	if _, err = retention.Exec(f.ctx, `update report_evidence set status='deleted',deleted_at=now() where object_key='synthetic/report'`); err != nil {
		t.Fatal(err)
	}
	if err = retention.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	result := <-finished
	if result.err != nil || result.status != http.StatusConflict {
		t.Fatalf("retired file scan status=%d err=%v body=%s", result.status, result.err, result.raw)
	}
}
