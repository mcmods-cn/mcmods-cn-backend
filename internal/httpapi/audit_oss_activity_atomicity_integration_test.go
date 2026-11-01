package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	xwebp "golang.org/x/image/webp"
	"image/png"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

// Use the existing credential-redacting HTTP assertion, including on an
// expected-failure pre-fix response that may contain a signed access URL.
func auditOSSRequire(t *testing.T, f test037Fixture, token, method, path string, body any, want int) []byte {
	return (auditFixGRFixture{test013Fixture: f.test013Fixture}).require(t, token, method, path, body, want)
}

func TestAuditCMP003OSSCompletionSharesRegistrationAndQuotaTransactionHTTPIntegration(t *testing.T) {
	for _, scope := range []string{"user", "admin"} {
		t.Run(scope, func(t *testing.T) {
			f := newTEST037Fixture(t)
			base := "/api/v1/users/me/oss/uploads/"
			if scope == "admin" {
				base = "/api/v1/admin/oss/uploads/"
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
			}
			bytes := []byte("owned atomic upload source " + scope)
			ticket := f.ticket(t, f.editor, base+"presign", "atomic-"+scope+".zip", "application/octet-stream", "test037-atomic", bytes)
			f.put(t, ticket, bytes)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before := f.facts(t)
			raw := auditOSSRequire(t, f, f.editor, http.MethodPost, base+"complete", ticket.completion(), 500)
			if !strings.Contains(string(raw), "failed to persist upload activity") {
				t.Fatalf("completion failed at an unrelated boundary: %s", raw)
			}
			auditOSSAssertFailureOnlyCompensatesOwnCandidates(t, f, before)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			var prior int
			if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&prior); err != nil {
				t.Fatal(err)
			}
			result := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, base+"complete", ticket.completion(), 201))
			var files, intents int
			var verified bool
			if err := f.db.QueryRow(f.ctx, `select count(*),bool_and(file.source_hash_verified) from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where binding.upload_object_key=$1 and binding.status='committed' and file.status='active'`, ticket.ObjectKey).Scan(&files, &verified); err != nil || files != 1 || !verified {
				t.Fatalf("registered files=%d verified=%v err=%v", files, verified, err)
			}
			if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&intents); err != nil || intents != prior+1 {
				t.Fatalf("recovery intents=%d prior=%d err=%v", intents, prior, err)
			}
			// A completed-file replay remains readable during an activity outage. It is
			// not another upload, and must preserve the original source/reference rows.
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before = f.facts(t)
			repeated := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, base+"complete", ticket.completion(), 200))
			if repeated["id"] != result["id"] {
				t.Fatal("repeat completion changed immutable public identity")
			}
			f.unchanged(t, before)
		})
	}
}

func TestAuditCMP003OSSScanSharesStatusReferencesAndAuditTransactionHTTPIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
	bytes := []byte("owned shared scan fixture")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "shared-scan.zip", "application/octet-stream", "test037-atomic", bytes)
	f.put(t, ticket, bytes)
	file := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	if _, err := f.db.Exec(f.ctx, `insert into mod_gallery_images(mod_id,oss_file_id,created_by) select mod_id,file.id,$2 from (values($3::bigint),($4::bigint)) refs(mod_id) cross join oss_files file where file.public_id=$1`, file.ID, f.userIDs[f.editor], f.modID, f.otherModID); err != nil {
		t.Fatal(err)
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := f.facts(t)
	raw := auditOSSRequire(t, f, f.editor, http.MethodPatch, "/api/v1/admin/oss/files/"+file.ID+"/scan", map[string]string{"status": "rejected", "note": "Owned reversible scan decision"}, 500)
	if !strings.Contains(string(raw), "failed to persist scan activity") {
		t.Fatalf("scan failed at unrelated boundary: %s", raw)
	}
	f.unchanged(t, before)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	var prior int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionEdit, activity.ObjectFile).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	auditOSSRequire(t, f, f.editor, http.MethodPatch, "/api/v1/admin/oss/files/"+file.ID+"/scan", map[string]string{"status": "rejected", "note": "Owned reversible scan decision"}, 200)
	var status, scan string
	var intents, audits, deletions int
	var references int
	if err := f.db.QueryRow(f.ctx, `select count(*) from mod_gallery_images where oss_file_id=(select id from oss_files where public_id=$1)`, file.ID).Scan(&references); err != nil || references != 2 {
		t.Fatalf("shared references=%d err=%v", references, err)
	}
	if err := f.db.QueryRow(f.ctx, `select status,scan_status from oss_files where public_id=$1`, file.ID).Scan(&status, &scan); err != nil || status != "quarantined" || scan != "rejected" {
		t.Fatalf("scan state=%s/%s err=%v", status, scan, err)
	}
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3),(select count(*) from audit_events where aggregate_type='oss_file' and aggregate_key=$4 and action='scan_status_update'),(select count(*) from oss_object_deletion_outbox where object_key=$5)`, f.userIDs[f.editor], activity.ActionEdit, activity.ObjectFile, file.ID, ticket.ObjectKey).Scan(&intents, &audits, &deletions); err != nil || intents != prior+1 || audits != 1 || deletions != 0 {
		t.Fatalf("scan intents=%d prior=%d audits=%d deletion=%d err=%v", intents, prior, audits, deletions, err)
	}
}

func auditOSSAllFacts(t *testing.T, f test037Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"blueprint_jobs", "blueprint_variants", "activity_event_outbox", "system_settings"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(row) order by to_jsonb(row)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` row`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}

func TestAuditCMP003OSSBlueprintCompletionIncludesNormalizeOutboxHTTPIntegration(t *testing.T) {
	for _, failure := range []string{"activity", "normalize_outbox"} {
		t.Run(failure, func(t *testing.T) {
			f := newTEST037Fixture(t)
			raw, _, err := encodeBlueprint(test036Document(), "nbt")
			if err != nil {
				t.Fatal(err)
			}
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "atomic-blueprint.nbt", "application/octet-stream", "blueprint", raw)
			f.put(t, ticket, raw)
			if failure == "activity" {
				auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			} else {
				if _, err := f.db.Exec(f.ctx, `create function audit_oss_normalize_outbox_failure() returns trigger language plpgsql as $$ begin raise exception 'AUDIT_OSS_NORMALIZE_OUTBOX_UNAVAILABLE' using errcode='XX001'; end $$; create trigger audit_oss_normalize_outbox_failure before insert on nats_outbox for each row execute function audit_oss_normalize_outbox_failure()`); err != nil {
					t.Fatal(err)
				}
			}
			before := auditOSSAllFacts(t, f)
			response := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 500)
			message := "failed to persist upload activity"
			if failure == "normalize_outbox" {
				message = "failed to register blueprint processing task"
			}
			if !strings.Contains(string(response), message) {
				t.Fatalf("wrong failure boundary: %s", response)
			}
			auditOSSAssertFailureOnlyCompensatesOwnCandidates(t, f, before)
			if failure == "activity" {
				auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			} else {
				if _, err := f.db.Exec(f.ctx, `drop trigger audit_oss_normalize_outbox_failure on nats_outbox; drop function audit_oss_normalize_outbox_failure()`); err != nil {
					t.Fatal(err)
				}
			}
			response = auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
			if !strings.Contains(string(response), "queued") {
				t.Fatal("valid blueprint did not queue normalization")
			}
			var files, jobs, variants, outbox int
			if err := f.db.QueryRow(f.ctx, `select (select count(*) from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where binding.upload_object_key=$1 and binding.status='committed' and file.status='active'),(select count(*) from blueprint_jobs where operation='normalize' and status='queued'),(select count(*) from blueprint_variants),(select count(*) from nats_outbox where event_type='blueprint.conversion.requested')`, ticket.ObjectKey).Scan(&files, &jobs, &variants, &outbox); err != nil || files != 1 || jobs != 1 || variants != 1 || outbox != 1 {
				t.Fatalf("recovered files/jobs/variants/outbox=%d/%d/%d/%d err=%v", files, jobs, variants, outbox, err)
			}
			before = auditOSSAllFacts(t, f)
			auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 200)
			if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(before, after) {
				t.Fatal("completed blueprint replay changed durable facts")
			}
		})
	}
}

func TestAuditCMP003OSSConfigurationActivitySharesSettingsTransactionHTTPIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
	payload := f.cfg
	payload.Prefix = "owned-atomic-config"
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := auditOSSAllFacts(t, f)
	response := auditOSSRequire(t, f, f.editor, http.MethodPut, "/api/v1/admin/config/oss", payload, 500)
	if !strings.Contains(string(response), "failed to persist OSS configuration activity") {
		t.Fatal("configuration rejected at unrelated boundary")
	}
	if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(before, after) {
		t.Fatal("failed configuration intent changed settings or business facts")
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	var prior, count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionEdit, activity.ObjectFile).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	auditOSSRequire(t, f, f.editor, http.MethodPut, "/api/v1/admin/config/oss", payload, 200)
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionEdit, activity.ObjectFile).Scan(&count); err != nil || count != prior+1 {
		t.Fatalf("configuration intent=%d prior=%d err=%v", count, prior, err)
	}
}

func TestAuditCMP003OSSConcurrentCompletionHasOneIntentAndHashesOutsidePoolHTTPIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("owned concurrent upload")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "concurrent.zip", "application/octet-stream", "test037-atomic", raw)
	f.put(t, ticket, raw)
	config := f.db.Config()
	config.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	f.server.db = pool
	defer func() { f.server.db = f.db; pool.Close() }()
	original := f.provider.Config.Handler
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	f.provider.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/"+ticket.ObjectKey) {
			ready <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		original.ServeHTTP(w, r)
	})
	var prior int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	first := auditAccessStartHTTP(f.test013Fixture, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion())
	second := auditAccessStartHTTP(f.test013Fixture, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion())
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-f.ctx.Done():
			close(release)
			t.Fatal("both actual source-hash GETs did not reach the controlled barrier")
		}
	}
	if pool.Stat().AcquiredConns() != 0 {
		close(release)
		t.Fatalf("hash validation held transaction connections=%d", pool.Stat().AcquiredConns())
	}
	close(release)
	statuses := map[int]int{}
	for _, done := range []<-chan auditAccessHTTPResult{first, second} {
		select {
		case result := <-done:
			if result.err != nil {
				t.Fatal(result.err)
			}
			statuses[result.status]++
		case <-f.ctx.Done():
			t.Fatal("concurrent completions did not finish")
		}
	}
	if statuses[201] != 1 || statuses[200] != 1 {
		t.Fatalf("concurrent completion statuses=%v", statuses)
	}
	var files, intents int
	var bytes int64
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where binding.upload_object_key=$1 and binding.status='committed'),(select count(*) from activity_event_outbox where user_id=$2 and action_id=$3 and object_type_id=$4),(select active_stored_bytes from oss_user_quota_usage where user_id=$2)`, ticket.ObjectKey, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&files, &intents, &bytes); err != nil || files != 1 || intents != prior+1 || bytes != int64(len(raw)) {
		t.Fatalf("concurrent facts files=%d intents=%d prior=%d stored=%d err=%v", files, intents, prior, bytes, err)
	}
}

func TestAuditCMP003OSSLargeOrdinaryUploadsKeepOwnLimitHTTPIntegration(t *testing.T) {
	var archive bytes.Buffer
	archive.Grow((33 << 20) + 1024)
	writer := zip.NewWriter(&archive)
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "payload.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write(make([]byte, 33<<20)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw := archive.Bytes()
	for _, scope := range []string{"user", "admin"} {
		t.Run(scope, func(t *testing.T) {
			f := newTEST037Fixture(t)
			base := "/api/v1/users/me/oss/uploads/"
			if scope == "admin" {
				base = "/api/v1/admin/oss/uploads/"
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
			}
			ticket := f.ticket(t, f.editor, base+"presign", "ordinary33MiB.zip", "application/zip", "test037-atomic", raw)
			f.put(t, ticket, raw)
			body := ticket.completion()
			if ticket.Method == "MULTIPART" {
				body["multipartUploadId"] = ticket.Multipart.UploadID
			}
			result := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, base+"complete", body, 201))
			if result["sizeBytes"] != float64(len(raw)) {
				t.Fatal("ordinary file was truncated or hit an unrelated blueprint budget")
			}
		})
	}
	f := newTEST037Fixture(t)
	digest := sha256.Sum256(raw)
	response := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", map[string]any{"originalName": "actual-blueprint.nbt", "contentType": "application/octet-stream", "source": "blueprint", "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:])}, 413)
	if !bytes.Contains(response, []byte("BLUEPRINT_SOURCE_SIZE_LIMIT")) {
		t.Fatal("actual blueprint must retain its specific source-size limit")
	}
}

func TestAuditCMP003OSSConvertedUploadActivityFailureRetainsRetryableSourceHTTPIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	source, err := os.ReadFile("testdata/audit-20261024/oss-atomic-source.png")
	if err != nil {
		t.Fatal(err)
	}
	converted, err := os.ReadFile("testdata/audit-20261024/oss-atomic-source.webp")
	if err != nil {
		t.Fatal(err)
	}
	a, err := png.Decode(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	b, err := xwebp.Decode(bytes.NewReader(converted))
	if err != nil {
		t.Fatal(err)
	}
	if a.Bounds() != b.Bounds() {
		t.Fatal("conversion golden lost dimensions")
	}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				t.Fatal("conversion golden lost pixel/alpha values")
			}
		}
	}
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "atomic-image.png", "image/png", "markdown", source)
	f.put(t, ticket, source)
	conversionFacts := auditOSSInstallBindingWebPConversion(t, f, ticket, converted)

	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := auditOSSAllFacts(t, f)
	response := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 500)
	if !bytes.Contains(response, []byte("failed to persist upload activity")) {
		t.Fatal("converted upload did not reach its activity-intent boundary")
	}
	auditOSSAssertFailureOnlyCompensatesOwnCandidates(t, f, before)
	destination, _ := conversionFacts()
	f.store.mu.Lock()
	_, sourcePresent := f.store.objects[ticket.ObjectKey]
	stored := bytes.Clone(f.store.objects[destination].data)
	f.store.mu.Unlock()
	if !sourcePresent || !bytes.Equal(stored, converted) {
		t.Fatal("failed registration destroyed retryable source or converted bytes")
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	var prior int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	completed := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	sourceHash := sha256.Sum256(source)
	storedHash := sha256.Sum256(converted)
	if completed["sourceSha256"] != hex.EncodeToString(sourceHash[:]) || completed["storedSha256"] != hex.EncodeToString(storedHash[:]) || completed["sourceSha256"] == completed["storedSha256"] {
		t.Fatal("source and converted actual-byte hashes were not kept distinct")
	}
	var sourceBytes, storedBytes int64
	var intents int
	_, processes := conversionFacts()
	if err := f.db.QueryRow(f.ctx, `select active_source_bytes,active_stored_bytes,(select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3) from oss_user_quota_usage where user_id=$1`, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&sourceBytes, &storedBytes, &intents); err != nil || sourceBytes != int64(len(source)) || storedBytes != int64(len(converted)) || intents != prior+1 || processes != 2 {
		t.Fatalf("converted recovery bytes=%d/%d intents=%d prior=%d processes=%d err=%v", sourceBytes, storedBytes, intents, prior, processes, err)
	}
	repeated := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 200))
	if repeated["sourceSha256"] != completed["sourceSha256"] || repeated["storedSha256"] != completed["storedSha256"] {
		t.Fatal("idempotent response lost source/stored hash contract")
	}
}
