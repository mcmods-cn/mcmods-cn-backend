package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

// These are explicit one-minute requests through the existing API contract,
// not a shorter product default. The separate 600s/60m proofs keep their pins.
func auditOSSByteOneMinuteTicket(t *testing.T, f test037Fixture, name, contentType string, raw []byte) test037Ticket {
	t.Helper()
	started := time.Now()
	body := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", map[string]any{
		"originalName": name, "contentType": contentType, "source": "test037-maintenance", "category": "test037",
		"sizeBytes": len(raw), "sha256": sha256Hex(raw), "expiresMinutes": 1,
	}, 200)
	finished := time.Now()
	ticket := decodeTEST022Data[test037Ticket](t, body)
	expires := decodeTEST022Data[struct{ ExpiresAt time.Time }](t, body).ExpiresAt
	if !ticket.UploadRequired || ticket.Method != http.MethodPut || ticket.SizeBytes != int64(len(raw)) || ticket.SHA256 != sha256Hex(raw) {
		t.Fatal("one-minute maintenance precondition lacks the exact normal initial PUT ticket")
	}
	auditOSSByteRequireActualMinuteCapability(t, f, ticket, expires, started, finished)
	return ticket
}

// This one declared legacy-conversion setup needs an actual known grant for
// its old stored alias as well as the normal API-issued original grant. The
// SDK creates that precise one-minute grant; no URL is disclosed and no PUT is
// performed here. This is synthetic old-storage setup, not HTTP completion.
func auditOSSByteIssueActualLegacyCapability(t *testing.T, f test037Fixture, key string) {
	t.Helper()
	client, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	signed, err := client.Presign(f.ctx, &aliyunoss.PutObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key), ForbidOverwrite: aliyunoss.Ptr("true")}, aliyunoss.PresignExpires(time.Minute))
	finished := time.Now()
	if err != nil {
		t.Fatal("cannot prepare actual SDK grant for the declared initial legacy alias")
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err = recordOSSUploadCapabilityTx(f.ctx, tx, cfg, key, f.userIDs[f.editor], ossUploadCapability{Kind: "put", ExpiresAt: signed.Expiration}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	auditOSSByteRequireActualMinuteCapability(t, f, test037Ticket{Method: http.MethodPut, URL: signed.URL, ObjectKey: key}, signed.Expiration, started, finished)
}

// The SDK fixes precise absolute Expiration before it samples signing time.
// Its relative query field subtracts Unix seconds and can be59 on a UTC-second
// boundary. Require the exact absolute endpoint and the requested one-minute
// issuance interval instead of weakening the check to a relative59..60 range.
func auditOSSByteMinuteDeadline(location string, expiration, started, finished time.Time) (int64, error) {
	if expiration.IsZero() || finished.Before(started) || expiration.Before(started.Add(time.Minute)) || expiration.After(finished.Add(time.Minute)) {
		return 0, errors.New("SDK absolute expiration does not match the actual one-minute issuance interval")
	}
	parsed, err := url.Parse(location)
	if err != nil || parsed.User != nil {
		return 0, errors.New("invalid owned signed URL")
	}
	query := parsed.Query()
	signingTime, timeErr := time.Parse("20060102T150405Z", query.Get("x-oss-date"))
	seconds, durationErr := strconv.ParseInt(query.Get("x-oss-expires"), 10, 64)
	if timeErr != nil || durationErr != nil || seconds <= 0 || seconds > 60 || query.Get("x-oss-signature-version") != "OSS4-HMAC-SHA256" {
		return 0, errors.New("invalid one-minute signature deadline fields")
	}
	if signingTime.Before(started.Truncate(time.Second)) || signingTime.After(finished.Truncate(time.Second)) || !signingTime.Add(time.Duration(seconds)*time.Second).Equal(expiration.Truncate(time.Second)) {
		return 0, errors.New("signed endpoint does not equal the precise SDK expiration truncated to seconds")
	}
	return seconds, nil
}

func auditOSSByteRequireActualMinuteCapability(t *testing.T, f test037Fixture, ticket test037Ticket, expected, started, finished time.Time) {
	t.Helper()
	seconds, err := auditOSSByteMinuteDeadline(ticket.URL, expected, started, finished)
	if err != nil {
		t.Fatal(err)
	}
	var expiration time.Time
	var actor, count int64
	var kind, upload string
	if err = f.db.QueryRow(f.ctx, `select max_expires_at,last_actor_id,grant_count,grant_kind,upload_id from public.oss_upload_capabilities where bucket=$1 and endpoint=$2 and object_key=$3`, f.cfg.Bucket, f.cfg.Endpoint, ticket.ObjectKey).Scan(&expiration, &actor, &count, &kind, &upload); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL's timestamp precision is microseconds; the SDK retains nanos.
	if delta := expiration.Sub(expected); delta <= -time.Microsecond || delta >= time.Microsecond || actor != f.userIDs[f.editor] || count != 1 || kind != "put" || upload != "" {
		t.Fatal("retained actual SDK expiration/actor/count/kind differs beyond PostgreSQL timestamp precision")
	}
	t.Logf("CMP005_ACTUAL_MINUTE_GRANT requested_seconds=60 actual_signed_seconds=%d actual_sdk_deadline=%s persisted_db_deadline=%s absolute_endpoint_matches=true", seconds, expected.UTC().Format(time.RFC3339Nano), expiration.UTC().Format(time.RFC3339Nano))
}

func TestAuditCMP005MaintenanceMinuteDeadlineOraclePinsAbsoluteSDKExpiration(t *testing.T) {
	// Pure deadline-oracle examples do not change any live/signing/DB clock.
	base := time.Date(2026, 11, 1, 1, 2, 3, 0, time.UTC)
	started := base.Add(999 * time.Millisecond)
	expires := started.Add(time.Minute)
	location := func(signing time.Time, seconds string) string {
		return "https://owned.invalid/object?" + url.Values{"x-oss-date": {signing.Format("20060102T150405Z")}, "x-oss-expires": {seconds}, "x-oss-signature-version": {"OSS4-HMAC-SHA256"}}.Encode()
	}
	for _, example := range []struct {
		name     string
		signing  time.Time
		seconds  string
		deadline time.Time
		finished time.Duration
		want     bool
	}{
		{"same_second_60", base, "60", expires, 1100 * time.Millisecond, true},
		{"crossed_second_59", base.Add(time.Second), "59", expires, 1100 * time.Millisecond, true},
		{"longer_prepare_same_absolute_deadline", base.Add(2 * time.Second), "58", expires, 2100 * time.Millisecond, true},
		{"wrong_59_endpoint", base, "59", expires, 1100 * time.Millisecond, false},
		{"wrong_60_endpoint", base.Add(time.Second), "60", expires, 1100 * time.Millisecond, false},
		{"actual_ttl_shortened", base, "59", expires.Add(-time.Second), 1100 * time.Millisecond, false},
		{"actual_ttl_extended", base, "61", expires.Add(time.Second), 1100 * time.Millisecond, false},
		{"extended_ttl_with_legal_relative_field", base.Add(time.Second), "60", expires.Add(time.Second), 1100 * time.Millisecond, false},
		{"zero_duration", base, "0", expires, 1100 * time.Millisecond, false},
	} {
		t.Run(example.name, func(t *testing.T) {
			_, err := auditOSSByteMinuteDeadline(location(example.signing, example.seconds), example.deadline, started, base.Add(example.finished))
			if (err == nil) != example.want {
				t.Fatalf("absolute SDK deadline oracle accepted=%v expected=%v: %v", err == nil, example.want, err)
			}
		})
	}
}

func auditOSSByteAwaitRealCapabilityExpiry(t *testing.T, f test037Fixture, keys ...string) {
	t.Helper()
	var expiry, now time.Time
	var grants int
	unique := map[string]bool{}
	for _, key := range keys {
		unique[key] = true
	}
	if err := f.db.QueryRow(f.ctx, `select count(*),max(max_expires_at),clock_timestamp() from public.oss_upload_capabilities where bucket=$1 and endpoint=$2 and object_key=any($3::text[])`, f.cfg.Bucket, f.cfg.Endpoint, keys).Scan(&grants, &expiry, &now); err != nil || grants != len(unique) {
		t.Fatalf("real granted deadline unavailable: grants=%d err=%v", grants, err)
	}
	deadline := expiry.Add(1100 * time.Millisecond)
	t.Logf("CMP005_REAL_GRANT_WAIT actual_sdk_deadline=%s db_clock_before=%s not_before=%s requested_seconds=60 no_clock_or_capability_changes=true", expiry.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano), deadline.UTC().Format(time.RFC3339Nano))
	if delay := deadline.Sub(now); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-f.ctx.Done():
			t.Fatal("original fixture budget expired while waiting for its actual SDK grant")
		}
	}
	var expired bool
	if err := f.db.QueryRow(f.ctx, `select clock_timestamp(),clock_timestamp()>$1::timestamptz+interval '1 second'`, expiry).Scan(&now, &expired); err != nil || !expired {
		t.Fatalf("actual database clock has not crossed SDK expiry+1s: expired=%v err=%v", expired, err)
	}
	t.Logf("CMP005_REAL_GRANT_WAIT_COMPLETE db_clock_after=%s actual_deadline_passed=true", now.UTC().Format(time.RFC3339Nano))
}

func auditOSSByteCapabilityFacts(t *testing.T, f test037Fixture) map[string]string {
	t.Helper()
	facts := auditOSSAllFacts(t, f)
	for _, table := range []string{"oss_upload_capabilities", "oss_upload_byte_bindings", "oss_upload_maintenance_actions"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	f.store.mu.Lock()
	objects := map[string]any{}
	for key, object := range f.store.objects {
		objects[key] = map[string]any{"sha256": sha256Hex(object.data), "size": len(object.data), "metadataHash": object.hash, "contentType": object.contentType}
	}
	f.store.mu.Unlock()
	raw, err := json.Marshal(objects)
	if err != nil {
		t.Fatal(err)
	}
	facts["ownedObjectBytesAndMetadata"] = string(raw)
	return facts
}

func TestAuditCMP005MaintenanceLiveActualCapabilityHoldsWithoutIntentIntegration(t *testing.T) {
	f, _, _, plan := auditOSSByteRawFixture(t)
	before := auditOSSByteCapabilityFacts(t, f)
	for _, apply := range []bool{false, true} {
		if result := auditOSSByteMaintenanceRun(t, f, f.db, plan, apply); result.Status != "RAW_CAPABILITY_LIVE_HOLD" || result.ReferenceRows != 0 || !result.ReferencesKnown {
			t.Fatalf("live actual grant was not independently held after complete reference scan: %+v", result)
		}
	}
	if !reflect.DeepEqual(before, auditOSSByteCapabilityFacts(t, f)) {
		t.Fatal("live capability HOLD changed bytes, files, quota, receipt, capability or journal")
	}
}

func TestAuditCMP005MaintenanceUnknownLegacyCapabilityHoldsWithoutIntentIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	id, _, _, source, stored := auditOSSByteLegacyFile(t, f, false)
	entry := OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: id, ExpectedDisplayEndpoint: f.cfg.displayEndpoint(), ExpectedSourceSHA: sha256Hex(source), ExpectedStoredSHA: sha256Hex(stored), ExpectedSourceSize: int64(len(source)), ExpectedStoredSize: int64(len(stored))}
	if got := auditOSSByteMaintenanceRun(t, f, f.db, auditOSSByteMaintenancePlan(t, f, entry), true); got.Status != "PROMOTED" {
		t.Fatalf("ordinary exact legacy promotion precondition: %+v", got)
	}
	var binding string
	if err := f.db.QueryRow(f.ctx, `update oss_upload_byte_bindings set created_at=clock_timestamp()-interval '10 days' where file_id=(select id from oss_files where public_id=$1) returning id::text`, id).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	plan := auditOSSByteMaintenancePlan(t, f, OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "cleanup_raw", BindingID: binding})
	plan.RetentionCutoff = time.Now().Add(-8 * 24 * time.Hour)
	before := auditOSSByteCapabilityFacts(t, f)
	for _, apply := range []bool{false, true} {
		if result := auditOSSByteMaintenanceRun(t, f, f.db, plan, apply); result.Status != "RAW_CAPABILITY_UNKNOWN_HOLD" || result.ReferenceRows != 0 || !result.ReferencesKnown {
			t.Fatalf("old retention cutoff invented an unknown SDK grant deadline: %+v", result)
		}
	}
	if !reflect.DeepEqual(before, auditOSSByteCapabilityFacts(t, f)) {
		t.Fatal("unknown legacy capability HOLD changed bytes or durable facts")
	}
}

func TestAuditCMP005MaintenanceFinalCapabilityGuardRechecksUnderOriginalKeyLockIntegration(t *testing.T) {
	f, _, _, plan := auditOSSByteRawFixture(t)
	run := ossMaintenanceRun{s: f.server, plan: plan, opts: OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned immutable snapshot"}, hasJournal: true}
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
	before := auditOSSByteCapabilityFacts(t, f)
	if err = run.executeRawCleanup(f.ctx, plan.Entries[0], token, file, proof, cfg, client); ossMaintenanceScopeStatus(err) != "RAW_CAPABILITY_LIVE_HOLD" {
		t.Fatalf("final locked executor omitted actual grant guard: %v", err)
	}
	if !reflect.DeepEqual(before, auditOSSByteCapabilityFacts(t, f)) {
		t.Fatal("final grant fence changed bytes, row proof or claimed intent")
	}
}

func TestAuditCMP005MaintenanceDoesNotExcludeUnreviewedCapabilityColumnsIntegration(t *testing.T) {
	f, _, _, plan := auditOSSByteRawFixture(t)
	if _, err := f.db.Exec(f.ctx, `alter table oss_upload_capabilities add column unreviewed_body text`); err != nil {
		t.Fatal(err)
	}
	before := auditOSSByteCapabilityFacts(t, f)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	if _, err := RunOSSByteMaintenance(ctx, f.db, f.server.cfg, plan, OSSByteMaintenanceOptions{Apply: true, BackupReference: "owned immutable snapshot"}); err == nil || err.Error() != "maintenance requires the reviewed issued-capability column inventory" {
		t.Fatalf("unreviewed table extension was hidden from the asset inventory: %v", err)
	}
	if !reflect.DeepEqual(before, auditOSSByteCapabilityFacts(t, f)) {
		t.Fatal("unreviewed capability schema changed bytes or durable facts")
	}
}
