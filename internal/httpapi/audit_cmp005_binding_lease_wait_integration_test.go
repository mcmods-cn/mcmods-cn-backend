package httpapi

import (
	"encoding/json"
	"errors"
	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"testing"
	"time"
)

func TestAuditCMP005LeaseRenewAndConversionPlanRejectExpiryAcrossUnmodifiedRowWaitIntegration(t *testing.T) {
	for _, operation := range []string{"renew", "conversion-plan"} {
		t.Run(operation, func(t *testing.T) {
			f := newTEST037Fixture(t)
			raw := []byte("ordinary lease-bound upload " + operation)
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "lease.zip", "application/zip", "test037-binding", raw)
			f.put(t, ticket, raw)
			client, cfg, err := f.server.ossClient(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			head, err := client.HeadObject(f.ctx, &aliyunoss.HeadObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)})
			if err != nil {
				t.Fatal(err)
			}
			var req ossCompleteUploadRequest
			encoded, _ := json.Marshal(ticket.completion())
			if err = json.Unmarshal(encoded, &req); err != nil {
				t.Fatal(err)
			}
			binding, err := f.server.sealOSSUploadedSource(f.ctx, client, cfg, req, f.userIDs[f.editor], head)
			if err != nil {
				t.Fatal(err)
			}
			binding.stop()
			binding.ctx = f.ctx
			defer f.server.discardOSSByteBinding(binding)
			if _, err = f.db.Exec(f.ctx, `update oss_upload_byte_bindings set lease_expires_at=clock_timestamp()+interval '2 seconds' where id=$1::uuid`, binding.ID); err != nil {
				t.Fatal(err)
			}
			hold, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Rollback(f.ctx)
			if _, err = hold.Exec(f.ctx, `select id from oss_upload_byte_bindings where id=$1::uuid for update`, binding.ID); err != nil {
				t.Fatal(err)
			}
			var holderPID int
			if err = hold.QueryRow(f.ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
				t.Fatal(err)
			}
			var before string
			if err = f.db.QueryRow(f.ctx, `select to_jsonb(binding)::text from oss_upload_byte_bindings binding where id=$1::uuid`, binding.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if operation == "renew" {
					done <- f.server.refreshOSSByteBindingLease(f.ctx, binding)
				} else {
					done <- f.server.planOSSByteBindingStoredKey(binding, persistedWebPObjectKey(binding.SourceKey))
				}
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			wait := func(query string, args ...any) {
				t.Helper()
				for {
					var ready bool
					if e := f.db.QueryRow(f.ctx, query, args...).Scan(&ready); e != nil {
						t.Fatal(e)
					}
					if ready {
						return
					}
					select {
					case <-ticker.C:
					case <-f.ctx.Done():
						t.Fatal("owned receipt barrier not reached")
					}
				}
			}
			wait(`select exists(select 1 from pg_stat_activity where datname=current_database() and $1=any(pg_blocking_pids(pid)) and query like '%oss_upload_byte_bindings%')`, holderPID)
			wait(`select clock_timestamp()>=lease_expires_at from oss_upload_byte_bindings where id=$1::uuid`, binding.ID)
			if err = hold.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-done; !errors.Is(err, errOSSByteBindingLeaseLost) {
				t.Fatalf("unmodified receipt expired across %s lock wait was accepted: %v", operation, err)
			}
			var after string
			if err = f.db.QueryRow(f.ctx, `select to_jsonb(binding)::text from oss_upload_byte_bindings binding where id=$1::uuid`, binding.ID).Scan(&after); err != nil || after != before {
				t.Fatalf("expired %s changed receipt state: %v", operation, err)
			}
			t.Log("Actual waiter observed on unchanged receipt tuple; DB clock crossed original expiry before release; operation rejected without renewing, planning, adopting or publishing")
		})
	}
}
