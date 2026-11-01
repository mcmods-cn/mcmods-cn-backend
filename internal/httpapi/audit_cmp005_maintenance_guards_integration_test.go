package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

func auditOSSByteRawFixture(t *testing.T) (test037Fixture, test037Ticket, string, OSSByteMaintenancePlan) {
	t.Helper()
	f := newTEST037Fixture(t)
	raw := []byte("ordinary retained upload; one client PUT only")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "guard.zip", "application/zip", "test037-maintenance", raw)
	f.put(t, ticket, raw)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	var binding string
	if err := f.db.QueryRow(f.ctx, `update oss_upload_byte_bindings set created_at=clock_timestamp()-interval '10 days' where file_id=(select id from oss_files where public_id=$1) returning id::text`, file["id"]).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	plan := auditOSSByteMaintenancePlan(t, f, OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "cleanup_raw", BindingID: binding})
	plan.RetentionCutoff = time.Now().Add(-8 * 24 * time.Hour)
	return f, ticket, binding, plan
}

func TestAuditCMP005MaintenanceRawCutoffReservationAttemptSessionAndManualReferencesHoldIntegration(t *testing.T) {
	for _, kind := range []string{"cutoff", "reservation", "attempt", "multipart", "manual"} {
		t.Run(kind, func(t *testing.T) {
			f, ticket, _, plan := auditOSSByteRawFixture(t)
			want := ""
			switch kind {
			case "cutoff":
				plan.RetentionCutoff = time.Now().Add(-time.Hour)
				want = "RETENTION_CUTOFF_NOT_MET"
			case "reservation":
				if err := f.server.reserveUserOSSUploadQuota(f.ctx, f.userIDs[f.editor], ticket.ObjectKey, ticket.SizeBytes, ticket.SizeBytes, time.Now().Add(time.Hour), ossUserQuotaLimits{single: 64 << 20, daily: 256 << 20, total: 256 << 20}); err != nil {
					t.Fatal(err)
				}
				want = "RAW_QUOTA_RESERVATION_PRESENT"
			case "attempt":
				id := newExportID()
				key := path.Join(path.Dir(ticket.ObjectKey), ossSealedUploadComponent, id, "unfinished.zip")
				if _, err := f.db.Exec(f.ctx, `insert into oss_upload_byte_bindings(id,owner_id,bucket,endpoint,region,upload_object_key,logical_category,source,original_name,declared_sha256,declared_size_bytes,sealed_source_key,stored_object_key,run_token,lease_expires_at,file_endpoint) values($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12,'owned-unfinished',clock_timestamp()+interval '1 minute',$4)`, id, f.userIDs[f.editor], f.cfg.Bucket, f.cfg.Endpoint, f.cfg.Region, ticket.ObjectKey, ticket.Category, ticket.Source, ticket.OriginalName, ticket.SHA256, ticket.SizeBytes, key); err != nil {
					t.Fatal(err)
				}
				want = "RAW_INPUT_IN_USE"
			case "multipart":
				req := ossDirectUploadRequest{OriginalName: ticket.OriginalName, ContentType: ticket.ContentType, SizeBytes: ticket.SizeBytes, SHA256: ticket.SHA256, Category: ticket.Category, Source: ticket.Source}
				if err := f.server.registerOSSMultipartSession(f.ctx, f.userIDs[f.editor], f.cfg, req, ticket.ObjectKey, "owned-unsettled-session", time.Now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				want = "RAW_MULTIPART_UNSETTLED"
			case "manual":
				if _, err := f.db.Exec(f.ctx, `create table audit_owned_raw_manual_ref(body text)`); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Exec(f.ctx, `insert into audit_owned_raw_manual_ref values($1)`, "Manual content retains "+ticket.ObjectKey); err != nil {
					t.Fatal(err)
				}
				want = "NEEDS_SCOPED_REFERENCE_PLAN"
			}
			before := f.facts(t)
			if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, true); got.Status != want {
				t.Fatalf("specific %s guard not reached: %+v want=%s", kind, got, want)
			}
			f.unchanged(t, before)
			var intents int
			if err := f.db.QueryRow(f.ctx, `select count(*) from oss_upload_maintenance_actions`).Scan(&intents); err != nil || intents != 0 {
				t.Fatalf("held raw cleanup wrote intent=%d err=%v", intents, err)
			}
			f.store.mu.Lock()
			object, exists := f.store.objects[ticket.ObjectKey]
			f.store.mu.Unlock()
			if !exists || !bytes.Equal(object.data, []byte("ordinary retained upload; one client PUT only")) {
				t.Fatal("held raw cleanup deleted or changed its input")
			}
		})
	}
}

func TestAuditCMP005MaintenanceExecutorRechecksLateReferenceAndRetiresPreparationIntegration(t *testing.T) {
	f, ticket, _, plan := auditOSSByteRawFixture(t)
	run := ossMaintenanceRun{s: f.server, plan: plan, opts: OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned synthetic snapshot"}, hasJournal: true}
	client, cfg, err := f.server.ossClient(f.ctx)
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
	// An ordinary live reference appears after durable intent creation. No
	// source bytes are changed; the executor must recheck this current binding.
	if _, err = f.db.Exec(f.ctx, `create table audit_owned_late_ref(body jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into audit_owned_late_ref values(jsonb_build_object('url',$1::text))`, ticket.ObjectKey); err != nil {
		t.Fatal(err)
	}
	if err = run.executeRawCleanup(f.ctx, plan.Entries[0], token, file, proof, cfg, client); !errors.Is(err, errOSSMaintenanceConflict) {
		t.Fatalf("late live reference did not reject exact cleanup: %v", err)
	}
	var req ossCompleteUploadRequest
	raw, _ := json.Marshal(ticket.completion())
	if err = json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	b := &ossUploadByteBinding{ID: newExportID(), Token: newExportID(), UploadKey: ticket.ObjectKey, cfg: cfg}
	b.SourceKey = path.Join(path.Dir(ticket.ObjectKey), ossSealedUploadComponent, b.ID, path.Base(ticket.ObjectKey))
	b.StoredKey = b.SourceKey
	var before int
	if err = f.db.QueryRow(f.ctx, `select count(*) from oss_upload_byte_bindings`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = f.server.persistOSSByteBindingPreparation(f.ctx, b, req, f.userIDs[f.editor], ticket.SizeBytes); !errors.Is(err, errOSSRawUploadRetired) {
		t.Fatalf("retired original input created a new preparation: %v", err)
	}
	var after int
	if err = f.db.QueryRow(f.ctx, `select count(*) from oss_upload_byte_bindings`).Scan(&after); err != nil || before != after {
		t.Fatal("rejected preparation left a new proof row")
	}
	f.store.mu.Lock()
	_, exists := f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if !exists {
		t.Fatal("late reference guard failed to retain exact input")
	}
}

func TestAuditCMP005MaintenanceActionLeaseAndSnapshotFencesAreExactIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	id, key, _, source, stored := auditOSSByteLegacyFile(t, f, false)
	e := OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: id, ExpectedDisplayEndpoint: f.cfg.displayEndpoint(), ExpectedSourceSHA: sha256Hex(source), ExpectedStoredSHA: sha256Hex(stored), ExpectedSourceSize: int64(len(source)), ExpectedStoredSize: int64(len(stored))}
	plan := auditOSSByteMaintenancePlan(t, f, e)
	run := ossMaintenanceRun{s: f.server, plan: plan, opts: OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned synthetic snapshot"}, hasJournal: true}
	file, err := run.loadFile(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	token, err := run.claimAction(f.ctx, f.cfg, e, file, key, key, "preparing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = run.claimAction(f.ctx, f.cfg, e, file, key, key, "preparing"); !errors.Is(err, errOSSMaintenanceBusy) {
		t.Fatalf("live owner was replaced: %v", err)
	}
	if _, err = f.db.Exec(f.ctx, `update oss_upload_maintenance_actions set lease_expires_at=clock_timestamp()-interval '1 second' where id=$1::uuid`, e.ActionID); err != nil {
		t.Fatal(err)
	}
	newToken, err := run.claimAction(f.ctx, f.cfg, e, file, key, key, "preparing")
	if err != nil || newToken == token {
		t.Fatalf("expired owner retry: %v", err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = lockOSSByteMaintenanceActionTx(f.ctx, tx, e.ActionID, token, "preparing")
	_ = tx.Rollback(f.ctx)
	if !errors.Is(err, errOSSByteBindingLeaseLost) {
		t.Fatalf("superseded owner still had a valid lease: %v", err)
	}
	// Explicit current metadata change prevents the old preimage from claiming
	// or publishing over it; the user's newer name is retained.
	if _, err = f.db.Exec(f.ctx, `update oss_files set original_name='newer-user-name.zip' where public_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update oss_upload_maintenance_actions set lease_expires_at=clock_timestamp()-interval '1 second' where id=$1::uuid`, e.ActionID); err != nil {
		t.Fatal(err)
	}
	if _, err = run.claimAction(f.ctx, f.cfg, e, file, key, key, "preparing"); !errors.Is(err, errOSSMaintenanceConflict) {
		t.Fatalf("stale snapshot accepted or hit an unrelated failure: %v", err)
	}
	var name string
	if err = f.db.QueryRow(f.ctx, `select original_name from oss_files where public_id=$1`, id).Scan(&name); err != nil || name != "newer-user-name.zip" {
		t.Fatal("stale maintenance rewrote the newer user name")
	}
}

func TestAuditCMP005MaintenanceTypedEndpointRestoreKeepsSharedReferencesIntegration(t *testing.T) {
	f, ticket, bindingID, _ := auditOSSByteRawFixture(t)
	var id, oldKey string
	var fileID int64
	if err := f.db.QueryRow(f.ctx, `select file.id,file.public_id,file.object_key from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where binding.id=$1::uuid`, bindingID).Scan(&fileID, &id, &oldKey); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update oss_upload_byte_bindings set file_endpoint='' where id=$1::uuid`, bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update oss_files set source_hash_verified=false where id=$1`, fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `create table audit_owned_shared_file_ref(file_id bigint references oss_files(id),manual_name text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into audit_owned_shared_file_ref values($1,'keep manual shared binding')`, fileID); err != nil {
		t.Fatal(err)
	}
	e := OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: id, ExpectedDisplayEndpoint: f.cfg.displayEndpoint(), ExpectedSourceSHA: ticket.SHA256, ExpectedStoredSHA: ticket.SHA256, ExpectedSourceSize: ticket.SizeBytes, ExpectedStoredSize: ticket.SizeBytes}
	plan := auditOSSByteMaintenancePlan(t, f, e)
	if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, false); got.Status != "READY_TYPED_ENDPOINT_RESTORE" {
		t.Fatalf("typed old mapping diagnosis: %+v", got)
	}
	if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, true); got.Status != "RESTORED_TYPED_ENDPOINT" {
		t.Fatalf("typed byte-evidenced endpoint restore: %+v", got)
	}
	var key, endpoint, manual string
	var refs int
	var verified bool
	if err := f.db.QueryRow(f.ctx, `select file.object_key,file.source_hash_verified,binding.file_endpoint,(select count(*) from audit_owned_shared_file_ref where file_id=file.id),(select manual_name from audit_owned_shared_file_ref where file_id=file.id) from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where binding.id=$1::uuid`, bindingID).Scan(&key, &verified, &endpoint, &refs, &manual); err != nil {
		t.Fatal(err)
	}
	if key != oldKey || !verified || endpoint != f.cfg.displayEndpoint() || refs != 1 || manual != "keep manual shared binding" {
		t.Fatal("typed endpoint restoration moved/deleted shared or manual content")
	}
}

func TestAuditCMP005MaintenanceActionClockRejectsExpiryAcrossUnmodifiedRowWaitIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	id, key, _, source, stored := auditOSSByteLegacyFile(t, f, false)
	e := OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: id, ExpectedDisplayEndpoint: f.cfg.displayEndpoint(), ExpectedSourceSHA: sha256Hex(source), ExpectedStoredSHA: sha256Hex(stored), ExpectedSourceSize: int64(len(source)), ExpectedStoredSize: int64(len(stored))}
	plan := auditOSSByteMaintenancePlan(t, f, e)
	run := ossMaintenanceRun{s: f.server, plan: plan, opts: OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned synthetic snapshot"}, hasJournal: true}
	file, err := run.loadFile(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	token, err := run.claimAction(f.ctx, f.cfg, e, file, key, key, "preparing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update oss_upload_maintenance_actions set lease_expires_at=clock_timestamp()+interval '2 seconds' where id=$1::uuid`, e.ActionID); err != nil {
		t.Fatal(err)
	}
	hold, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(f.ctx)
	var pid int
	if err = hold.QueryRow(f.ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = hold.Exec(f.ctx, `select id from oss_upload_maintenance_actions where id=$1::uuid for update`, e.ActionID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		tx, err := f.db.Begin(f.ctx)
		if err == nil {
			err = lockOSSByteMaintenanceActionTx(f.ctx, tx, e.ActionID, token, "preparing")
			_ = tx.Rollback(f.ctx)
		}
		done <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	wait := func(query string, args ...any) {
		t.Helper()
		for {
			var ready bool
			if err := f.db.QueryRow(f.ctx, query, args...).Scan(&ready); err != nil {
				t.Fatal(err)
			}
			if ready {
				return
			}
			select {
			case <-ticker.C:
			case <-f.ctx.Done():
				t.Fatal("owned maintenance action row barrier was not reached")
			}
		}
	}
	wait(`select exists(select 1 from pg_stat_activity where datname=current_database() and $1=any(pg_blocking_pids(pid)) and query like '%oss_upload_maintenance_actions%')`, pid)
	wait(`select clock_timestamp()>=lease_expires_at from oss_upload_maintenance_actions where id=$1::uuid`, e.ActionID)
	if err = hold.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, errOSSByteBindingLeaseLost) {
		t.Fatalf("expired action admitted after an unchanged-row wait: %v", err)
	}
}

func TestAuditCMP005MaintenanceAlreadyAbsentInputIsRecordedAndSizePinsRemainExactIntegration(t *testing.T) {
	f, ticket, bindingID, plan := auditOSSByteRawFixture(t)
	client, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.DeleteObject(f.ctx, &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)}); err != nil {
		t.Fatal(err)
	}
	if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, false); got.Status != "ALREADY_ABSENT" {
		t.Fatalf("already-absent dry-run: %+v", got)
	}
	if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, true); got.Status != "RAW_ABSENCE_RECORDED" {
		t.Fatalf("absent input did not get a durable retirement without a false removed count: %+v", got)
	}
	var state string
	if err = f.db.QueryRow(f.ctx, `select state from oss_upload_maintenance_actions where id=$1::uuid`, plan.Entries[0].ActionID).Scan(&state); err != nil || state != "completed" {
		t.Fatal("actual absence did not complete its durable scoped intent")
	}
	var id string
	if err = f.db.QueryRow(f.ctx, `select file.public_id from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where binding.id=$1::uuid`, bindingID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	entry := OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: id, ExpectedDisplayEndpoint: cfg.displayEndpoint(), ExpectedSourceSHA: ticket.SHA256, ExpectedStoredSHA: ticket.SHA256, ExpectedSourceSize: ticket.SizeBytes + 1, ExpectedStoredSize: ticket.SizeBytes}
	wrongSize := auditOSSByteMaintenancePlan(t, f, entry)
	if got := auditOSSByteMaintenanceRun(t, f, f.db, wrongSize, true); got.Status != "PLAN_PROOF_MISMATCH" {
		t.Fatalf("existing typed receipt ignored explicit length pin: %+v", got)
	}
}
