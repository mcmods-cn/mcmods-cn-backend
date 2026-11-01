package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestReportEvidenceFileAndMetadataCommitTogetherAndRetryIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify report evidence registration")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// The real maintenance API intentionally addresses public tables. This
	// new OID/owner/nonce guarded database keeps those tables in our scope.
	pool := openAccessOwnedMigratedFixture(t, ctx, databaseURL)
	var err error

	evidenceBody := []byte("BUG-080 transient registration evidence\n")
	digest := sha256.Sum256(evidenceBody)
	sha := hex.EncodeToString(digest[:])
	objectKey := "mcmods/moderation/report-evidence/800/bug080-evidence.txt"
	legacyObjectKey := "mcmods/moderation/report-evidence/800/bug080-legacy-orphan.txt"
	probeObjectKey := "mcmods/moderation/report-evidence/800/bug080-unverified-probe.txt"
	rawInputs := []string{objectKey, legacyObjectKey, probeObjectKey}
	provider, objects := newBUG080OwnedEvidenceProvider(t, evidenceBody, rawInputs...)
	defer provider.Close()

	// Install the exact migrated schema, then inject only the specific owned
	// metadata failure. A missing/incorrect fixture column must never count as
	// reaching this business boundary.
	if _, err = pool.Exec(ctx, `insert into users(id,username,email,password_hash) values(800,'bug080-owner','bug080-owner@example.invalid','synthetic-only');
	 create sequence bug080_reject_once;
	 create function reject_first_bug080_evidence() returns trigger language plpgsql as $$ begin if nextval('bug080_reject_once')=1 then raise exception 'BUG-080 transient evidence registration failure' using errcode='XX001'; end if; return new; end $$;
	 create trigger reject_first_bug080_evidence before insert on report_evidence for each row execute function reject_first_bug080_evidence()`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool, cfg: config.Load()}
	ossConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: provider.URL, PublicEndpoint: "https://public.example.test",
		Bucket: "bug080-bucket", AccessKeyID: "bug080-key", AccessKeySecret: "bug080-secret", UseCName: true, Prefix: "mcmods",
	}
	sealed, err := server.sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"objectKey": objectKey, "originalName": "bug080-evidence.txt", "contentType": "text/plain",
		"sizeBytes": len(evidenceBody), "sha256": sha,
	}
	var initiallyCalled bool
	if err = pool.QueryRow(ctx, `select is_called from bug080_reject_once`).Scan(&initiallyCalled); err != nil || initiallyCalled {
		t.Fatalf("fault sequence was not fresh: called=%t err=%v", initiallyCalled, err)
	}
	var bindingTotal, deletionTotal int
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_upload_byte_bindings),(select count(*) from oss_object_deletion_outbox)`).Scan(&bindingTotal, &deletionTotal); err != nil || bindingTotal != 0 || deletionTotal != 0 {
		t.Fatalf("fresh owned lifecycle baseline bindings=%d deletions=%d err=%v", bindingTotal, deletionTotal, err)
	}
	objects.requireExactSet(t, rawInputs, nil, evidenceBody)
	zeroEffectTables := []string{"oss_files", "report_evidence", "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations", "user_activity_events", "nats_outbox", "blueprints", "blueprint_jobs", "public_routes"}
	beforeFailure := accessFixtureBusinessRows(t, ctx, pool, zeroEffectTables...)
	first := invokeBUG080ReportEvidenceComplete(t, ctx, server, body)
	if first.Code != http.StatusInternalServerError || !strings.Contains(first.Body.String(), "登记举报附件失败") {
		t.Fatalf("first completion must reach specific evidence metadata fault: status=%d body=%s", first.Code, first.Body.String())
	}
	var firstError struct{ Code, Error string }
	if err = json.Unmarshal(first.Body.Bytes(), &firstError); err != nil || firstError.Code != "HTTP_500" || firstError.Error != "登记举报附件失败" {
		t.Fatalf("specific metadata error contract: %+v err=%v", firstError, err)
	}
	var injected int64
	var called bool
	if err = pool.QueryRow(ctx, `select last_value,is_called from bug080_reject_once`).Scan(&injected, &called); err != nil || !called || injected != 1 {
		t.Fatalf("expected specific injected fault executions=1, got%d err=%v", injected, err)
	}
	var filesAfterFailure, evidenceAfterFailure int
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_files),(select count(*) from report_evidence)`).
		Scan(&filesAfterFailure, &evidenceAfterFailure); err != nil {
		t.Fatal(err)
	}
	if filesAfterFailure != 0 || evidenceAfterFailure != 0 {
		t.Fatalf("failed atomic registration left files/evidence = %d/%d", filesAfterFailure, evidenceAfterFailure)
	}

	if after := accessFixtureBusinessRows(t, ctx, pool, zeroEffectTables...); after != beforeFailure {
		t.Fatal("specific evidence metadata fault changed business, quota, activity or outbox rows")
	}
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_upload_byte_bindings),(select count(*) from oss_object_deletion_outbox)`).Scan(&bindingTotal, &deletionTotal); err != nil || bindingTotal != 1 || deletionTotal != 1 {
		t.Fatalf("metadata fault created an unexpected lifecycle set: bindings=%d deletions=%d err=%v", bindingTotal, deletionTotal, err)
	}
	var abandonedBindingID, abandonedKey string
	var compensation int
	if err = pool.QueryRow(ctx, `select min(binding.id::text),min(binding.sealed_source_key),count(*)
	 from oss_upload_byte_bindings binding join oss_object_deletion_outbox deletion
	 on deletion.bucket=binding.bucket and deletion.endpoint=binding.endpoint and deletion.object_key=binding.sealed_source_key
	 where binding.upload_object_key=$1 and binding.owner_id=800 and binding.status='cleanup_pending' and binding.file_id is null
	 and binding.bucket=$2 and binding.endpoint=$3 and binding.file_endpoint=$4 and binding.region=$5 and binding.use_cname=$9
	 and binding.logical_category='moderation/report-evidence/800' and binding.source='report_evidence'
	 and binding.declared_sha256=$6 and binding.declared_size_bytes=$7 and binding.run_token<>'' and binding.lease_expires_at>clock_timestamp()
	 and binding.sealed_source_key=$8||binding.id::text||'/bug080-evidence.txt' and binding.stored_object_key=binding.sealed_source_key
	 and deletion.reason='upload-byte-binding-not-adopted' and deletion.region=binding.region and deletion.use_cname=binding.use_cname
	 and deletion.status='pending' and deletion.oss_file_id is null and deletion.raw_upload_binding_id is null
	 and deletion.attempts=0 and deletion.locked_at is null and deletion.locked_by='' and deletion.raw_hold_reason=''`,
		objectKey, ossConfig.Bucket, ossConfig.Endpoint, ossConfig.displayEndpoint(), ossConfig.Region, sha, len(evidenceBody), path.Dir(objectKey)+"/_mcmods_sealed_v1/", ossConfig.UseCName).
		Scan(&abandonedBindingID, &abandonedKey, &compensation); err != nil || compensation != 1 {
		t.Fatalf("failed registration candidate compensation=%d err=%v", compensation, err)
	}
	objects.requireBytes(t, abandonedKey, objectKey, evidenceBody)
	objects.requireExactSet(t, rawInputs, map[string]string{abandonedKey: objectKey}, evidenceBody)
	abandonedBeforeRetry := snapshotBUG080AbandonedLifecycle(t, ctx, pool, abandonedBindingID, abandonedKey)

	second := invokeBUG080ReportEvidenceComplete(t, ctx, server, body)
	if second.Code != http.StatusCreated {
		t.Fatalf("retry completion status = %d body=%s", second.Code, second.Body.String())
	}
	var response struct {
		Data struct {
			ID         string `json:"id"`
			EvidenceID string `json:"evidenceId"`
		} `json:"data"`
	}
	if err = json.Unmarshal(second.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var filesAfterRetry, evidenceAfterRetry int
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_files),(select count(*) from report_evidence)`).
		Scan(&filesAfterRetry, &evidenceAfterRetry); err != nil {
		t.Fatal(err)
	}
	if filesAfterRetry != 1 || evidenceAfterRetry != 1 || response.Data.EvidenceID == "" || response.Data.ID != response.Data.EvidenceID {
		t.Fatalf("retry registration = files/evidence %d/%d response=%+v body=%s", filesAfterRetry, evidenceAfterRetry, response, second.Body.String())
	}

	var completedKey string
	var actualProofs int
	if err = pool.QueryRow(ctx, `select file.object_key,count(binding.id)
	 from report_evidence evidence join oss_files file on file.object_key=evidence.object_key
	 join oss_upload_byte_bindings binding on binding.file_id=file.id
	 where evidence.public_id=$1 and file.uploader_id=800 and file.status='active' and file.source_hash_verified
	 and file.category='moderation/report-evidence/800' and file.source='report_evidence' and binding.logical_category=file.category and binding.source=file.source
	 and file.bucket=$2 and file.endpoint=$3 and file.sha256=$4 and file.stored_sha256=$4 and file.size_bytes=$5 and file.source_size_bytes=$5
	 and binding.owner_id=800 and binding.bucket=$2 and binding.endpoint=$6 and binding.file_endpoint=$3
	 and binding.upload_object_key=$7 and binding.status='committed' and binding.sealed_source_key=file.object_key and binding.stored_object_key=file.object_key
	 and binding.source_verified_sha256=$4 and binding.stored_verified_sha256=$4 and binding.source_verified_size_bytes=$5 and binding.stored_verified_size_bytes=$5
	 group by file.object_key`, response.Data.EvidenceID, ossConfig.Bucket, ossConfig.displayEndpoint(), sha, len(evidenceBody), ossConfig.Endpoint, objectKey).Scan(&completedKey, &actualProofs); err != nil || actualProofs != 1 {
		t.Fatalf("completed report/file/sealed-byte alias proof=%d err=%v", actualProofs, err)
	}
	objects.requireBytes(t, completedKey, objectKey, evidenceBody)
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_upload_byte_bindings),(select count(*) from oss_object_deletion_outbox)`).Scan(&bindingTotal, &deletionTotal); err != nil || bindingTotal != 2 || deletionTotal != 1 {
		t.Fatalf("successful retry created an unexpected lifecycle set: bindings=%d deletions=%d err=%v", bindingTotal, deletionTotal, err)
	}
	if completedKey == abandonedKey || snapshotBUG080AbandonedLifecycle(t, ctx, pool, abandonedBindingID, abandonedKey) != abandonedBeforeRetry {
		t.Fatal("successful retry changed or adopted its previous abandoned candidate/intent")
	}
	objects.requireExactSet(t, rawInputs, map[string]string{abandonedKey: objectKey, completedKey: objectKey}, evidenceBody)

	replayTables := append(append([]string(nil), zeroEffectTables...), "oss_upload_byte_bindings", "oss_object_deletion_outbox")
	beforeReplay := accessFixtureBusinessRows(t, ctx, pool, replayTables...)
	beforeReplayAssets := objects.bytesSnapshot(t)
	third := invokeBUG080ReportEvidenceComplete(t, ctx, server, body)
	if third.Code != http.StatusOK || !strings.Contains(third.Body.String(), `"idempotent":true`) || !strings.Contains(third.Body.String(), response.Data.EvidenceID) {
		t.Fatalf("idempotent completion status = %d body=%s", third.Code, third.Body.String())
	}
	mismatchedRetry := make(map[string]any, len(body))
	for key, value := range body {
		mismatchedRetry[key] = value
	}
	mismatchedRetry["sha256"] = strings.Repeat("f", 64)
	mismatchResponse := invokeBUG080ReportEvidenceComplete(t, ctx, server, mismatchedRetry)
	if mismatchResponse.Code != http.StatusBadRequest || strings.Contains(mismatchResponse.Body.String(), `"idempotent":true`) {
		t.Fatalf("mismatched idempotency status = %d body=%s", mismatchResponse.Code, mismatchResponse.Body.String())
	}
	if after := accessFixtureBusinessRows(t, ctx, pool, replayTables...); after != beforeReplay || objects.bytesSnapshot(t) != beforeReplayAssets {
		t.Fatal("idempotent/mismatched retry changed business, receipt, quota, outbox or actual assets")
	}

	if err = pool.QueryRow(ctx, `select last_value,is_called from bug080_reject_once`).Scan(&injected, &called); err != nil || !called || injected != 2 {
		t.Fatalf("replay/mismatch reran metadata registration: count=%d called=%t err=%v", injected, called, err)
	}
	t.Run("unverified_legacy_probe_is_not_adopted", func(t *testing.T) {
		oldID, _, before := insertBUG080LegacyFile(t, ctx, server, ossConfig, probeObjectKey, evidenceBody, sha)
		probe := invokeBUG080ReportEvidenceComplete(t, ctx, server, map[string]any{"objectKey": probeObjectKey, "originalName": "bug080-unverified-probe.txt", "contentType": "text/plain", "sizeBytes": len(evidenceBody), "sha256": sha})
		if probe.Code != http.StatusCreated || strings.Contains(probe.Body.String(), `"idempotent":true`) {
			t.Fatalf("unverified legacy direct completion must create a proved file, status=%d body=%s", probe.Code, probe.Body.String())
		}
		var after string
		var newID int64
		if err := pool.QueryRow(ctx, `select to_jsonb(file)::text from oss_files file where id=$1`, oldID).Scan(&after); err != nil || after != before {
			t.Fatalf("unverified old row was incorrectly adopted/changed: %v", err)
		}
		if err := pool.QueryRow(ctx, `select file_id from oss_upload_byte_bindings where upload_object_key=$1 and status='committed' and owner_id=800`, probeObjectKey).Scan(&newID); err != nil || newID == oldID {
			t.Fatalf("unverified probe adopted old ID or has no actual alias: old=%d new=%d err=%v", oldID, newID, err)
		}
	})
	t.Run("legacy_orphan_uses_actual_byte_proof_promotion", func(t *testing.T) {
		legacyID, legacyPublicID, before := insertBUG080LegacyFile(t, ctx, server, ossConfig, legacyObjectKey, evidenceBody, sha)
		legacyKey := promoteBUG080LegacyFile(t, ctx, server, ossConfig, legacyID, legacyPublicID, legacyObjectKey, before, evidenceBody, sha)
		objects.requireBytes(t, legacyKey, legacyObjectKey, evidenceBody)
		legacyBody := map[string]any{"objectKey": legacyObjectKey, "originalName": "bug080-legacy-orphan.txt", "contentType": "text/plain", "sizeBytes": len(evidenceBody), "sha256": sha}
		legacyResponse := invokeBUG080ReportEvidenceComplete(t, ctx, server, legacyBody)
		if legacyResponse.Code != http.StatusOK || !strings.Contains(legacyResponse.Body.String(), `"idempotent":true`) {
			t.Fatalf("legacy orphan recovery status=%d body=%s", legacyResponse.Code, legacyResponse.Body.String())
		}
		var legacyEvidence int
		if err := pool.QueryRow(ctx, `select count(*) from report_evidence evidence join oss_files file on file.object_key=evidence.object_key
		 where file.id=$1 and file.public_id=$2 and evidence.uploader_id=800 and evidence.sha256=$3 and evidence.byte_size=$4 and evidence.status='temporary'`, legacyID, legacyPublicID, sha, len(evidenceBody)).Scan(&legacyEvidence); err != nil || legacyEvidence != 1 {
			t.Fatalf("legacy orphan exact original file evidence rows=%d err=%v", legacyEvidence, err)
		}
		again := invokeBUG080ReportEvidenceComplete(t, ctx, server, legacyBody)
		if again.Code != http.StatusOK || !strings.Contains(again.Body.String(), `"idempotent":true`) {
			t.Fatalf("promoted legacy repeated retry=%d body=%s", again.Code, again.Body.String())
		}
	})
}

func invokeBUG080ReportEvidenceComplete(t *testing.T, ctx context.Context, server *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/reports/evidence/uploads/complete" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 800}))
		server.completeOSSDirectUploadWithScope(w, r, ossReportEvidenceScope)
	}))
	defer origin.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.URL+"/api/v1/reports/evidence/uploads/complete", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	actual, err := origin.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer actual.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(actual.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	for key, values := range actual.Header {
		response.Header()[key] = values
	}
	response.WriteHeader(actual.StatusCode)
	_, _ = response.Write(responseBody)
	t.Logf("BUG080_ACTUAL_HTTP method=POST path=/api/v1/reports/evidence/uploads/complete status=%d bytes=%d controlled_claims_user=800", actual.StatusCode, len(responseBody))
	return response
}

type bug080OwnedEvidenceObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
	copies  map[string]string
	gets    map[string]int
}

func newBUG080OwnedEvidenceProvider(t *testing.T, body []byte, inputs ...string) (*httptest.Server, *bug080OwnedEvidenceObjects) {
	t.Helper()
	store := &bug080OwnedEvidenceObjects{objects: map[string][]byte{}, copies: map[string]string{}, gets: map[string]int{}}
	allowedInputs := map[string]bool{}
	for _, key := range inputs {
		store.objects[key] = bytes.Clone(body)
		allowedInputs[key] = true
	}
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store.mu.Lock()
		defer store.mu.Unlock()
		key := strings.TrimPrefix(r.URL.Path, "/")
		providerError := func(status int, code string) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>owned BUG080 fixture</Message></Error>", code)
		}
		if r.Method == http.MethodPut {
			actualBody, readErr := io.ReadAll(io.LimitReader(r.Body, 1))
			source, decodeErr := url.PathUnescape(r.Header.Get("x-oss-copy-source"))
			sourceKey := strings.TrimPrefix(source, "/bug080-bucket/")
			prefix := path.Dir(sourceKey) + "/_mcmods_sealed_v1/"
			tail := strings.Split(strings.TrimPrefix(key, prefix), "/")
			validTarget := strings.HasPrefix(key, prefix) && len(tail) == 2 && uuid.MatchString(tail[0]) && tail[1] == path.Base(sourceKey)
			forbid := r.Header.Get("x-oss-forbid-overwrite") == "true"
			t.Logf("BUG080_COPY actual_content_length=%d actual_body_bytes=%d exact_source=%t sealed_target=%t forbid_overwrite=%t", r.ContentLength, len(actualBody), allowedInputs[sourceKey], validTarget, forbid)
			if readErr != nil || len(actualBody) != 0 || r.ContentLength > 0 || decodeErr != nil ||
				!strings.HasPrefix(source, "/bug080-bucket/") || !allowedInputs[sourceKey] || !validTarget || !forbid {
				providerError(http.StatusBadRequest, "InvalidArgument")
				return
			}
			if _, exists := store.objects[key]; exists {
				providerError(http.StatusConflict, "FileAlreadyExists")
				return
			}
			store.objects[key] = bytes.Clone(store.objects[sourceKey])
			store.copies[key] = sourceKey
			w.Header().Set("Content-Type", "application/xml")
			w.Header().Set("ETag", `"bug080"`)
			_, _ = io.WriteString(w, `<CopyObjectResult><LastModified>2026-11-01T00:00:00.000Z</LastModified><ETag>bug080</ETag></CopyObjectResult>`)
			return
		}
		if r.Method != http.MethodHead && r.Method != http.MethodGet {
			providerError(http.StatusMethodNotAllowed, "MethodNotAllowed")
			return
		}
		data, exists := store.objects[key]
		if !exists {
			providerError(http.StatusNotFound, "NoSuchKey")
			return
		}
		digest := sha256.Sum256(data)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Header().Set("x-oss-meta-sha256", hex.EncodeToString(digest[:]))
		w.Header().Set("ETag", `"bug080"`)
		if r.Method == http.MethodGet {
			store.gets[key]++
			_, _ = w.Write(data)
		}
	}))
	return provider, store
}

func (store *bug080OwnedEvidenceObjects) requireBytes(t *testing.T, key, original string, want []byte) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if key == original || store.copies[key] != original || store.gets[key] < 1 ||
		!bytes.Equal(store.objects[key], want) || !bytes.Equal(store.objects[original], want) {
		t.Fatalf("sealed candidate actual COPY/GET/bytes or immutable original do not match: key=%s source=%s gets=%d", key, original, store.gets[key])
	}
}

func (store *bug080OwnedEvidenceObjects) bytesSnapshot(t *testing.T) string {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	data, err := json.Marshal(store.objects)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (store *bug080OwnedEvidenceObjects) requireExactSet(t *testing.T, rawInputs []string, candidates map[string]string, want []byte) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	expected := map[string]bool{}
	rawExpected := map[string]bool{}
	for _, key := range rawInputs {
		if expected[key] {
			t.Fatal("duplicate declared raw input")
		}
		expected[key] = true
		rawExpected[key] = true
		if _, copied := store.copies[key]; copied {
			t.Fatal("a raw input was replaced by a copy")
		}
	}
	for key, source := range candidates {
		if expected[key] || !rawExpected[source] || store.copies[key] != source || store.gets[key] < 1 {
			t.Fatalf("candidate lacks exact distinct source/COPY/GET evidence: key=%s source=%s", key, source)
		}
		expected[key] = true
	}
	if len(store.objects) != len(expected) || len(store.copies) != len(candidates) {
		t.Fatalf("unexpected provider object/copy set: objects=%d want=%d copies=%d want=%d", len(store.objects), len(expected), len(store.copies), len(candidates))
	}
	for key, data := range store.objects {
		if !expected[key] || !bytes.Equal(data, want) {
			t.Fatalf("unexpected or changed provider object bytes: %s", key)
		}
	}
	t.Logf("BUG080_EXACT_OBJECT_SET raw=%d sealed=%d actual_objects=%d actual_copies=%d", len(rawInputs), len(candidates), len(store.objects), len(store.copies))
}

func snapshotBUG080AbandonedLifecycle(t *testing.T, ctx context.Context, pool accessFixtureSQL, bindingID, key string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `select jsonb_build_object(
	 'binding',(select to_jsonb(binding) from oss_upload_byte_bindings binding where id=$1::uuid),
	 'deletions',(select jsonb_agg(to_jsonb(deletion) order by id) from oss_object_deletion_outbox deletion where object_key=$2))::text`, bindingID, key).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func insertBUG080LegacyFile(t *testing.T, ctx context.Context, server *Server, cfg ossConfigPayload, key string, body []byte, sha string) (int64, string, string) {
	t.Helper()
	var id int64
	var publicID, before string
	if err := server.db.QueryRow(ctx, `insert into oss_files(
	 bucket,endpoint,region,object_key,category,source,original_name,source_original_name,content_type,
	 size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
	 values($1,$2,$3,$4,'moderation/report-evidence/800','report_evidence',$5,$5,'text/plain; charset=utf-8',$6,$6,$7,800,'active','pending')
	 returning id,public_id`, cfg.Bucket, cfg.displayEndpoint(), cfg.Region, key, path.Base(key), len(body), sha).Scan(&id, &publicID); err != nil {
		t.Fatal(err)
	}
	if err := server.db.QueryRow(ctx, `select to_jsonb(file)::text from oss_files file where id=$1 and not source_hash_verified and stored_sha256=''`, id).Scan(&before); err != nil {
		t.Fatalf("unverified legacy input was not retained: %v", err)
	}
	return id, publicID, before
}

func promoteBUG080LegacyFile(t *testing.T, ctx context.Context, server *Server, cfg ossConfigPayload, id int64, publicID, originalKey, preimage string, body []byte, sha string) string {
	t.Helper()
	var databaseName string
	if err := server.db.QueryRow(ctx, `select current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	entry := OSSByteMaintenanceEntry{ActionID: newExportID(), Operation: "promote", FilePublicID: publicID, OriginalSourceKey: originalKey,
		ExpectedDisplayEndpoint: cfg.displayEndpoint(), ExpectedSourceSHA: sha, ExpectedStoredSHA: sha, ExpectedSourceSize: int64(len(body)), ExpectedStoredSize: int64(len(body))}
	plan := OSSByteMaintenancePlan{Version: 1, Database: databaseName, MaxDurationSeconds: 10, MaxFiles: 1, MaxReadBytes: 1 << 20,
		MaxReferenceRows: 100000, MaxReferenceCols: 2048, MaxReferenceBytes: 4 << 20, ReferenceWritesQuiesced: true,
		ReferenceWriteTables: []string{"oss_files"}, Entries: []OSSByteMaintenanceEntry{entry}}
	// There are no other application/Worker connections in this new database.
	// All test HTTP writes are synchronous and stopped during this maintenance.
	var otherClients int
	if err := server.db.QueryRow(ctx, `select count(*) from pg_stat_activity where datname=current_database() and backend_type='client backend' and pid<>pg_backend_pid()`).Scan(&otherClients); err != nil || otherClients != 0 {
		t.Fatalf("actual owned database is not quiescent: other clients=%d err=%v", otherClients, err)
	}
	scope, _, err := inspectOSSByteMaintenanceWriteScope(ctx, server.db, plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.ReferenceSchemaSHA = scope.SchemaSHA
	for _, table := range scope.PotentialTables {
		if table != "oss_files" {
			plan.QuiescedReferenceTables = append(plan.QuiescedReferenceTables, table)
		}
	}
	backup := map[string]any{"database": databaseName, "fileId": publicID, "filePreimage": json.RawMessage(preimage),
		"rawSourceKey": originalKey, "rawSourceBytesBase64": base64.StdEncoding.EncodeToString(body),
		"accountingAndBusinessRows": accessFixtureBusinessRows(t, ctx, server.db, "oss_files", "report_evidence", "oss_user_quota_usage", "oss_user_daily_quota_usage")}
	raw, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "legacy-before-promotion.json")
	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("write actual owned backup: %v/%v", err, closeErr)
	}
	directory, err := os.Open(filepath.Dir(backupPath))
	if err != nil {
		t.Fatal(err)
	}
	err, closeErr = directory.Sync(), directory.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("persist owned backup directory: %v/%v", err, closeErr)
	}
	info, err := os.Stat(backupPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("owned backup is not private mode0600: %v", err)
	}
	readback, err := os.ReadFile(backupPath)
	if err != nil || !bytes.Equal(readback, raw) {
		t.Fatalf("owned backup readback mismatch: %v", err)
	}
	backupSHA := sha256.Sum256(raw)
	backupReference := fmt.Sprintf("owned synthetic file+raw+accounting backup %s sha256=%x", backupPath, backupSHA)
	t.Logf("BUG080_ACTUAL_BACKUP bytes=%d sha256=%x actual_quiescence=true worker_count=0", len(raw), backupSHA)
	t.Logf("BUG080_ACTUAL_BACKUP_JSON %s", raw)
	before := accessFixtureBusinessRows(t, ctx, server.db, "oss_files", "report_evidence", "oss_upload_byte_bindings", "oss_upload_maintenance_actions", "oss_user_quota_usage", "oss_user_daily_quota_usage")
	dry, err := RunOSSByteMaintenance(ctx, server.db, server.cfg, plan, OSSByteMaintenanceOptions{})
	if err != nil || len(dry.Results) != 1 || dry.Results[0].Status != "READY_PINNED_PROMOTION" || !dry.Results[0].ReferencesKnown || dry.Results[0].SourceObservedSHA != sha || dry.Results[0].StoredObservedSHA != sha {
		t.Fatalf("actual legacy diagnosis did not prove eligibility; retain explicit HOLD instead of fabricating proof: %+v err=%v", dry, err)
	}
	if after := accessFixtureBusinessRows(t, ctx, server.db, "oss_files", "report_evidence", "oss_upload_byte_bindings", "oss_upload_maintenance_actions", "oss_user_quota_usage", "oss_user_daily_quota_usage"); after != before {
		t.Fatal("maintenance dry-run changed business, receipt, journal or accounting rows")
	}
	accountingBefore := accessFixtureBusinessRows(t, ctx, server.db, "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations")
	var filesBefore, filesAfter int
	if err = server.db.QueryRow(ctx, `select count(*) from oss_files`).Scan(&filesBefore); err != nil {
		t.Fatal(err)
	}
	actual, err := RunOSSByteMaintenance(ctx, server.db, server.cfg, plan, OSSByteMaintenanceOptions{Apply: true, BackupReference: backupReference})
	if err != nil || len(actual.Results) != 1 || actual.Results[0].Status != "PROMOTED" {
		t.Fatalf("actual bounded legacy promotion: %+v err=%v", actual, err)
	}
	if err = server.db.QueryRow(ctx, `select count(*) from oss_files`).Scan(&filesAfter); err != nil || filesAfter != filesBefore {
		t.Fatalf("legacy promotion duplicated/deleted files: before=%d after=%d err=%v", filesBefore, filesAfter, err)
	}
	if after := accessFixtureBusinessRows(t, ctx, server.db, "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations"); after != accountingBefore {
		t.Fatal("legacy promotion changed exact source/stored quota accounting")
	}
	var storedKey string
	var proofs, metadata int
	if err = server.db.QueryRow(ctx, `select file.object_key,count(binding.id),(select count(*) from report_evidence where object_key=file.object_key)
	 from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id
	 where file.id=$1 and file.public_id=$2 and file.source_hash_verified and file.bucket=$3 and file.endpoint=$4
	 and file.uploader_id=800 and file.category='moderation/report-evidence/800' and file.source='report_evidence' and binding.logical_category=file.category and binding.source=file.source
	 and file.sha256=$5 and file.stored_sha256=$5 and file.size_bytes=$6 and file.source_size_bytes=$6
	 and binding.owner_id=800 and binding.bucket=$3 and binding.endpoint=$7 and binding.file_endpoint=$4
	 and binding.status='committed' and binding.upload_object_key=$8 and binding.sealed_source_key=file.object_key and binding.stored_object_key=file.object_key
	 group by file.id,file.object_key`, id, publicID, cfg.Bucket, cfg.displayEndpoint(), sha, len(body), cfg.Endpoint, originalKey).Scan(&storedKey, &proofs, &metadata); err != nil || proofs != 1 || metadata != 0 || storedKey == originalKey {
		t.Fatalf("promotion did not preserve original ID/alias and missing-metadata input: proofs=%d metadata=%d err=%v", proofs, metadata, err)
	}
	return storedKey
}
