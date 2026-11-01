package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func auditOSSByteMaintenancePlan(t *testing.T, f test037Fixture, e OSSByteMaintenanceEntry) OSSByteMaintenancePlan {
	t.Helper()
	var name string
	if err := f.db.QueryRow(f.ctx, `select current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return OSSByteMaintenancePlan{Version: 1, Database: name, MaxDurationSeconds: 60, MaxFiles: 1, MaxReadBytes: 1 << 20, MaxReferenceRows: 100000, MaxReferenceCols: 2048, MaxReferenceBytes: 4 << 20, Entries: []OSSByteMaintenanceEntry{e}}
}

func auditOSSByteMaintenanceRun(t *testing.T, f test037Fixture, pool *pgxpool.Pool, plan OSSByteMaintenancePlan, apply bool) OSSByteMaintenanceResult {
	t.Helper()
	report, err := RunOSSByteMaintenance(f.ctx, pool, f.server.cfg, plan, OSSByteMaintenanceOptions{Apply: apply, BackupReference: "owned synthetic before-state retained by this fixture"})
	if err != nil || len(report.Results) != 1 {
		t.Fatalf("scoped maintenance report unavailable: %v", err)
	}
	return report.Results[0]
}

func auditOSSByteLegacyFile(t *testing.T, f test037Fixture, converted bool) (string, string, string, []byte, []byte) {
	t.Helper()
	source := []byte("owned synthetic old raw upload; one initial object only")
	stored := source
	original, sourceName, contentType := "legacy.zip", "legacy.zip", "application/zip"
	if converted {
		var err error
		source, err = os.ReadFile("testdata/audit-20261024/oss-atomic-source.png")
		if err != nil {
			t.Fatal(err)
		}
		stored, err = os.ReadFile("testdata/audit-20261024/oss-atomic-source.webp")
		if err != nil {
			t.Fatal(err)
		}
		original, sourceName, contentType = "legacy.webp", "legacy.png", "image/webp"
	}
	key := fmt.Sprintf("mcmods/user/%d/files/legacy/%s", f.userIDs[f.editor], original)
	sourceKey := key
	if converted {
		sourceKey = strings.TrimSuffix(key, ".webp") + ".png"
	}
	// Initial owned old-storage fixture, not an upload mutation during any
	// verification/registration. Bytes remain unchanged throughout promotion.
	f.store.mu.Lock()
	f.store.objects[key] = test036Object{data: bytes.Clone(stored), contentType: contentType, hash: strings.Repeat("e", 64)}
	if sourceKey != key {
		f.store.objects[sourceKey] = test036Object{data: bytes.Clone(source), contentType: "image/png", hash: sha256Hex(source)}
	}
	f.store.mu.Unlock()
	sourceMetaSHA := sha256Hex(source)
	if !converted {
		sourceMetaSHA = strings.Repeat("f", 64)
	}
	var publicID string
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,content_type,size_bytes,source_size_bytes,sha256,stored_sha256,source_hash_verified,uploader_id,status,scan_status) values($1,$2,$3,$4,$5,'legacy-fixture',$6,$7,$8,$9,$10,$11,$12,false,$13,'active','pending') returning public_id`, f.cfg.Bucket, f.cfg.displayEndpoint(), f.cfg.Region, key, ossCategoryFromObjectKey(key, f.cfg.Prefix), original, sourceName, contentType, len(stored), len(source), sourceMetaSHA, strings.Repeat("e", 64), f.userIDs[f.editor]).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	return publicID, key, sourceKey, source, stored
}

func TestAuditCMP005MaintenanceLegacyPromotionAndRawRetentionKeepPublicIdentityIntegration(t *testing.T) {
	for _, converted := range []bool{false, true} {
		t.Run(fmt.Sprint(converted), func(t *testing.T) {
			f := newTEST037Fixture(t)
			id, oldKey, sourceKey, source, stored := auditOSSByteLegacyFile(t, f, converted)
			entry := OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: id, ExpectedDisplayEndpoint: f.cfg.displayEndpoint(), ExpectedSourceSHA: sha256Hex(source), ExpectedStoredSHA: sha256Hex(stored), ExpectedSourceSize: int64(len(source)), ExpectedStoredSize: int64(len(stored))}
			if converted {
				entry.OriginalSourceKey = sourceKey
			}
			plan := auditOSSByteMaintenancePlan(t, f, entry)
			poolCfg := f.db.Config()
			poolCfg.MaxConns, poolCfg.MinConns = 2, 1
			pool, err := pgxpool.NewWithConfig(f.ctx, poolCfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			before := auditOSSAllFacts(t, f)
			if got := auditOSSByteMaintenanceRun(t, f, pool, plan, false); got.Status != "READY_PINNED_PROMOTION" || got.SourceObservedSHA != sha256Hex(source) || got.StoredObservedSHA != sha256Hex(stored) || !got.ReferencesKnown {
				t.Fatalf("dry-run did not observe exact actual source/stored bytes: %+v", got)
			}
			if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(after, before) {
				t.Fatal("dry-run changed business facts")
			}
			if got := auditOSSByteMaintenanceRun(t, f, pool, plan, true); got.Status != "PROMOTED" {
				t.Fatalf("actual scoped promotion: %+v", got)
			}
			var key, bindingID string
			var files, actions int
			if err = f.db.QueryRow(f.ctx, `select file.object_key,binding.id::text,(select count(*) from oss_files),(select count(*) from oss_upload_maintenance_actions where state='promoted') from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id and binding.status='committed' where file.public_id=$1`, id).Scan(&key, &bindingID, &files, &actions); err != nil {
				t.Fatal(err)
			}
			if key == oldKey || !isOSSSealedObjectKey(key) || files != 1 || actions != 1 {
				t.Fatal("promotion changed file identity, duplicated rows or failed to adopt server-only bytes")
			}
			f.store.mu.Lock()
			actual := f.store.objects[key]
			old := f.store.objects[oldKey]
			f.store.mu.Unlock()
			if !bytes.Equal(actual.data, stored) || !bytes.Equal(old.data, stored) {
				t.Fatal("promotion altered pixels/business bytes or removed original input")
			}
			before = auditOSSAllFacts(t, f)
			if got := auditOSSByteMaintenanceRun(t, f, pool, plan, true); got.Status != "ALREADY_VERIFIED" {
				t.Fatalf("promotion replay: %+v", got)
			}
			if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(after, before) {
				t.Fatal("promotion replay changed business facts")
			}
			// Backdate only this owned synthetic receipt to exercise the explicit
			// seven-day retention guard. No real historical rows are modified.
			if _, err = f.db.Exec(f.ctx, `update oss_upload_byte_bindings set created_at=clock_timestamp()-interval '10 days' where id=$1::uuid`, bindingID); err != nil {
				t.Fatal(err)
			}
			cleanup := auditOSSByteMaintenancePlan(t, f, OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "cleanup_raw", BindingID: bindingID})
			cleanup.RetentionCutoff = time.Now().Add(-8 * 24 * time.Hour)
			if got := auditOSSByteMaintenanceRun(t, f, pool, cleanup, false); got.Status != "READY_RAW_RETENTION_CLEANUP" {
				t.Fatalf("raw retention dry-run: %+v", got)
			}
			if got := auditOSSByteMaintenanceRun(t, f, pool, cleanup, true); got.Status != "RAW_REMOVED" {
				t.Fatalf("actual raw retention removal: %+v", got)
			}
			if got := auditOSSByteMaintenanceRun(t, f, pool, cleanup, true); got.Status != "ALREADY_COMPLETED" {
				t.Fatalf("raw retention replay: %+v", got)
			}
			f.store.mu.Lock()
			_, rawExists := f.store.objects[oldKey]
			adopted := f.store.objects[key]
			f.store.mu.Unlock()
			if rawExists || !bytes.Equal(adopted.data, stored) {
				t.Fatal("raw retention affected adopted/shared bytes or did not remove exact raw key")
			}
			access := decodeTEST022Data[struct {
				URL string `json:"url"`
			}](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/files/presign", map[string]any{"objectKey": oldKey}, 200))
			if !bytes.Equal(f.get(t, access.URL, 200), stored) {
				t.Fatal("old alias did not retain normal owner-authorized actual download after cleanup")
			}
			if converted {
				cleanup = auditOSSByteMaintenancePlan(t, f, OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "cleanup_raw", BindingID: bindingID, RawSourceKind: "legacy_original"})
				cleanup.RetentionCutoff = time.Now().Add(-8 * 24 * time.Hour)
				if got := auditOSSByteMaintenanceRun(t, f, pool, cleanup, true); got.Status != "RAW_REMOVED" {
					t.Fatalf("explicit recorded converted original cleanup: %+v", got)
				}
			}
		})
	}
}

func TestAuditCMP005MaintenanceMissingSourceWrongPinsAndReferencesRemainUnchangedIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	id, key, sourceKey, source, stored := auditOSSByteLegacyFile(t, f, true)
	entry := OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: id, ExpectedDisplayEndpoint: f.cfg.displayEndpoint(), ExpectedSourceSHA: sha256Hex(source), ExpectedStoredSHA: sha256Hex(stored), ExpectedSourceSize: int64(len(source)), ExpectedStoredSize: int64(len(stored))}
	plan := auditOSSByteMaintenancePlan(t, f, entry)
	before := auditOSSAllFacts(t, f)
	if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, true); got.Status != "MISSING_ORIGINAL_SOURCE" {
		t.Fatalf("inferred a missing original: %+v", got)
	}
	plan.Entries[0].OriginalSourceKey = sourceKey
	plan.Entries[0].ExpectedSourceSHA = strings.Repeat("f", 64)
	if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, true); got.Status != "BYTE_PIN_MISMATCH" {
		t.Fatalf("wrong explicit byte pin accepted: %+v", got)
	}
	plan.Entries[0].ExpectedSourceSHA = sha256Hex(source)
	small := plan
	small.MaxReferenceCols = 1
	if got := auditOSSByteMaintenanceRun(t, f, f.db, small, true); got.Status != "REFERENCE_BUDGET_EXCEEDED" {
		t.Fatalf("incomplete inventory was treated as no references: %+v", got)
	}
	if _, err := f.db.Exec(f.ctx, `create table audit_owned_manual_asset_ref(body jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into audit_owned_manual_asset_ref values(jsonb_build_object('manualText',$1::text))`, "Keep this exact manual URL/key: "+key); err != nil {
		t.Fatal(err)
	}
	if got := auditOSSByteMaintenanceRun(t, f, f.db, plan, true); got.Status != "NEEDS_SCOPED_REFERENCE_PLAN" || got.ReferenceRows < 1 {
		t.Fatalf("manual/shared reference not protected: %+v", got)
	}
	if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(after, before) {
		t.Fatal("rejected promotion changed normal files, quota or history")
	}
	var n int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from oss_upload_byte_bindings)+(select count(*) from oss_upload_maintenance_actions)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rejected data repair created proofs/intents=%d err=%v", n, err)
	}
}

func TestAuditCMP005MaintenancePlanRejectsUnboundedOrUnconfirmedApply(t *testing.T) {
	plan := OSSByteMaintenancePlan{Version: 1, Database: "owned", MaxDurationSeconds: 1, MaxFiles: 1, MaxReadBytes: 1, MaxReferenceRows: 1, MaxReferenceCols: 1, MaxReferenceBytes: 1, Entries: []OSSByteMaintenanceEntry{{ActionID: newExportID(), Operation: "promote", FilePublicID: "abc123456"}}}
	if err := validateOSSByteMaintenancePlan(plan, OSSByteMaintenanceOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := validateOSSByteMaintenancePlan(plan, OSSByteMaintenanceOptions{Apply: true}); err == nil {
		t.Fatal("apply accepted no backup or byte pins")
	}
	plan.MaxReadBytes = 0
	if err := validateOSSByteMaintenancePlan(plan, OSSByteMaintenanceOptions{}); err == nil {
		t.Fatal("unbounded byte inventory accepted")
	}
}
