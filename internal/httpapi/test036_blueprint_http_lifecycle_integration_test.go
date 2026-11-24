package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type test036Object struct {
	data              []byte
	contentType, hash string
}
type test036CopyRequest struct {
	Source, Destination, ForbidOverwrite string
	Bytes                                int
	SHA256                               string
	ContentLength                        int64
	BodyBytes                            int
}
type test036Fixture struct {
	test013Fixture
	server           *Server
	provider         *httptest.Server
	mu               *sync.Mutex
	objects          map[string]test036Object
	failGET, failPUT *atomic.Bool
	copyRequests     *[]test036CopyRequest
}

func TestTEST036LargeValidNormalizedArtifactStillConvertsAtOriginalBudgetsFullHTTPIntegration(t *testing.T) {
	f := newTEST036Fixture(t)
	document := blueprintDocument{SchemaVersion: blueprintSchemaVersion, Name: "TEST036 large valid artifact", SourceFormat: "nbt", DataVersion: 3955, Size: [3]int{250, 100, 10}}
	document.Blocks = make([]blueprintBlock, maxBlueprintNonAirBlockCount)
	state := blueprintBlockState{ID: "minecraft:" + strings.Repeat("a", 90)}
	for index := range document.Blocks {
		document.Blocks[index] = blueprintBlock{Position: [3]int{index % 250, (index / 250) % 100, index / (250 * 100)}, State: state}
	}
	normalized, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized) <= maxBlueprintSourceBytes || len(normalized) > maxBlueprintNormalizedBytes {
		t.Fatalf("fixture must cross only source read budget: normalized bytes=%d", len(normalized))
	}
	raw, _, err := encodeBlueprint(document, "nbt")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > maxBlueprintSourceBytes {
		t.Fatalf("fixture source exceeds unchanged source budget: %d", len(raw))
	}
	// Prove the source meets all decoder/dimension/volume/non-air limits before
	// asking the real worker to produce and read the larger normalized artifact.
	sourceDocument, err := decodeBlueprint(raw, "nbt", document.Name)
	if err != nil || len(sourceDocument.Blocks) != maxBlueprintNonAirBlockCount {
		t.Fatalf("large source is not legal: blocks=%d err=%v", len(sourceDocument.Blocks), err)
	}
	upload := f.upload(t, f.editor, "test036-large-valid.nbt", raw)
	if err = f.worker("large-normalize").processJob(f.ctx, upload.jobID); err != nil {
		t.Fatal(err)
	}
	var normalizedKey string
	if err = f.db.QueryRow(f.ctx, "select normalized_object_key from blueprints where public_id=$1 and status='ready'", upload.publicID).Scan(&normalizedKey); err != nil {
		t.Fatal(err)
	}
	stored := f.object(t, normalizedKey)
	if len(stored) <= maxBlueprintSourceBytes || len(stored) > maxBlueprintNormalizedBytes {
		t.Fatalf("worker did not actually persist the large valid normalized artifact: %d", len(stored))
	}
	response := f.require(t, f.editor, http.MethodPost, "/api/v1/blueprints/"+upload.publicID+"/convert", map[string]string{"format": "schem"}, 202)
	jobPublicID := decodeTEST022Data[map[string]string](t, response)["jobId"]
	var jobID int64
	if err = f.db.QueryRow(f.ctx, "select id from blueprint_jobs where public_id=$1 and operation='convert' and status='queued'", jobPublicID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err = f.worker("large-convert").processJob(f.ctx, jobID); err != nil {
		t.Fatalf("legal normalized artifact could not convert: %v", err)
	}
	var key string
	if err = f.db.QueryRow(f.ctx, "select object_key from blueprint_variants where blueprint_id=(select id from blueprints where public_id=$1) and format='schem' and status='ready'", upload.publicID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	converted, err := decodeBlueprint(f.object(t, key), "schem", document.Name)
	if err != nil || converted.Size != document.Size || len(converted.Blocks) != maxBlueprintNonAirBlockCount {
		t.Fatalf("large conversion fidelity: size=%v blocks=%d err=%v", converted.Size, len(converted.Blocks), err)
	}
	for _, block := range converted.Blocks {
		if !reflect.DeepEqual(block.State, state) {
			t.Fatal("large conversion changed palette state")
		}
	}
	t.Logf("unchanged budgets: source=%d normalized=%d blocks=%d", len(raw), len(stored), len(converted.Blocks))
}

func newTEST036Fixture(t *testing.T) test036Fixture {
	t.Helper()
	f := test036Fixture{test013Fixture: newTEST013Fixture(t), mu: new(sync.Mutex), objects: make(map[string]test036Object), failGET: new(atomic.Bool), failPUT: new(atomic.Bool), copyRequests: &[]test036CopyRequest{}}
	f.server = f.origin.Config.Handler.(*Server)
	f.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet && f.failGET.Load()) || (r.Method == http.MethodPut && f.failPUT.Load()) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(503)
			_, _ = io.WriteString(w, "<Error><Code>ServiceUnavailable</Code><Message>owned TEST036 transient fault</Message></Error>")
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		// The SDK uses path-style bucket URLs for this owned loopback endpoint.
		objectPath := strings.TrimPrefix(r.URL.Path, "/test036-owned/")
		if objectPath != r.URL.Path {
			objectPath = "/" + objectPath
		}
		if r.Method == http.MethodDelete {
			delete(f.objects, objectPath)
			w.WriteHeader(204)
			return
		}
		if r.Method == http.MethodPut {
			if encodedSource := r.Header.Get("x-oss-copy-source"); encodedSource != "" {
				// CopyObject has no upload body. Preserve exact actual source bytes
				// and metadata under the private destination requested by production;
				// never turn a zero-length copy request into a new empty upload.
				source, err := url.PathUnescape(encodedSource)
				body, bodyErr := io.ReadAll(io.LimitReader(r.Body, 1))
				request := test036CopyRequest{Source: source, Destination: objectPath, ForbidOverwrite: r.Header.Get("x-oss-forbid-overwrite"), ContentLength: r.ContentLength, BodyBytes: len(body)}
				*f.copyRequests = append(*f.copyRequests, request)
				providerError := func(status int, code string) {
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(status)
					_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>owned TEST036 copy boundary</Message></Error>", code)
				}
				if err != nil || bodyErr != nil || len(body) != 0 || !strings.HasPrefix(source, "/test036-owned/") || request.ForbidOverwrite != "true" || !isOSSSealedObjectKey(objectPath) {
					providerError(400, "InvalidArgument")
					return
				}
				sourcePath := "/" + strings.TrimPrefix(source, "/test036-owned/")
				attempt := path.Base(path.Dir(objectPath))
				exactPrivateTarget := path.Join(path.Dir(sourcePath), ossSealedUploadComponent, attempt, path.Base(sourcePath))
				if sourcePath == objectPath || len(attempt) != 36 || objectPath != exactPrivateTarget {
					providerError(400, "InvalidArgument")
					return
				}
				original, exists := f.objects[sourcePath]
				if !exists {
					providerError(404, "NoSuchKey")
					return
				}
				if _, exists = f.objects[objectPath]; exists {
					providerError(409, "FileAlreadyExists")
					return
				}
				original.data = bytes.Clone(original.data)
				f.objects[objectPath] = original
				last := len(*f.copyRequests) - 1
				(*f.copyRequests)[last].Bytes = len(original.data)
				(*f.copyRequests)[last].SHA256 = sha256Hex(original.data)
				w.Header().Set("Content-Type", "application/xml")
				w.Header().Set("ETag", `"test036-owned"`)
				_, _ = io.WriteString(w, "<CopyObjectResult><LastModified>2026-09-30T00:00:00.000Z</LastModified><ETag>test036-owned</ETag></CopyObjectResult>")
				return
			}
			raw, err := io.ReadAll(io.LimitReader(r.Body, maxBlueprintNormalizedBytes+1))
			if err != nil || int64(len(raw)) != r.ContentLength || len(raw) > maxBlueprintNormalizedBytes {
				w.WriteHeader(400)
				return
			}
			if _, exists := f.objects[objectPath]; exists {
				w.WriteHeader(409)
				return
			}
			f.objects[objectPath] = test036Object{data: bytes.Clone(raw), contentType: r.Header.Get("Content-Type"), hash: r.Header.Get("x-oss-meta-sha256")}
			w.Header().Set("ETag", `"test036-owned"`)
			w.WriteHeader(200)
			return
		}
		object, exists := f.objects[objectPath]
		if !exists {
			w.WriteHeader(404)
			return
		}
		if r.Method != http.MethodHead && r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", object.contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(object.data)))
		w.Header().Set("x-oss-meta-sha256", object.hash)
		w.Header().Set("ETag", `"test036-owned"`)
		w.Header().Set("Last-Modified", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			_, _ = w.Write(object.data)
		}
	}))
	t.Cleanup(f.provider.Close)
	t.Cleanup(func() {
		f.mu.Lock()
		requests := append([]test036CopyRequest(nil), (*f.copyRequests)...)
		f.mu.Unlock()
		for i, request := range requests {
			t.Logf("TEST036_PROVIDER_COPY index=%d method=PUT source=%q destination=%q forbid_overwrite=%q declared_content_length=%d actual_body_bytes=%d copied_bytes=%d actual_sha256=%s", i, request.Source, request.Destination, request.ForbidOverwrite, request.ContentLength, request.BodyBytes, request.Bytes, request.SHA256)
		}
	})
	cfg := defaultOSSConfig()
	cfg.Enabled, cfg.UseCName = true, true
	cfg.Bucket, cfg.Region = "test036-owned", "cn-test"
	cfg.Endpoint, cfg.PublicEndpoint = f.provider.URL, f.provider.URL
	cfg.AccessKeyID, cfg.AccessKeySecret = "test036-synthetic-key", "test036-synthetic-secret"
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)", sealed); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256")
	}
	review := defaultReviewConfig()
	review.BlueprintCreate, review.BlueprintEdit = true, true
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update system_settings set value=$2::jsonb where key=$1", reviewConfigSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	return f
}

func test036Document() blueprintDocument {
	return blueprintDocument{SchemaVersion: blueprintSchemaVersion, Name: "TEST036 golden", DataVersion: 3955, SourceFormat: "nbt", Size: [3]int{3, 2, 2}, Blocks: []blueprintBlock{
		{Position: [3]int{0, 0, 0}, State: blueprintBlockState{ID: "minecraft:stone"}},
		{Position: [3]int{2, 1, 1}, State: blueprintBlockState{ID: "minecraft:oak_log", Properties: map[string]string{"axis": "y"}}},
	}}
}

func (f test036Fixture) worker(name string) *BlueprintWorker {
	return &BlueprintWorker{cfg: f.server.cfg, db: f.db, queue: f.server.queue, workerID: "test036-" + name}
}
func (f test036Fixture) facts(t *testing.T) map[string]string {
	t.Helper()
	result := f.test013Fixture.facts(t)
	for _, table := range []string{"blueprints", "blueprint_variants", "blueprint_materials", "blueprint_mods", "blueprint_jobs", "blueprint_job_artifacts",
		"oss_files", "oss_object_deletion_outbox", "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations", "content_localizations", "content_subjects"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(fact_row) order by to_jsonb(fact_row)::text),'[]'::jsonb)::text from "+pgx.Identifier{table}.Sanitize()+" fact_row").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		result[table] = raw
	}
	return result
}
func (f test036Fixture) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	if !reflect.DeepEqual(before, after) {
		for table, raw := range before {
			if raw != after[table] {
				t.Errorf("failed/denied blueprint operation mutated %s", table)
			}
		}
		t.FailNow()
	}
}

type test036UploadResult struct {
	publicID, jobPublicID, format string
	jobID                         int64
	original                      []byte
}

func (f test036Fixture) upload(t *testing.T, token, name string, raw []byte) test036UploadResult {
	t.Helper()
	digest := sha256.Sum256(raw)
	body := ossDirectUploadRequest{OriginalName: name, ContentType: "application/octet-stream", SizeBytes: int64(len(raw)), SHA256: hex.EncodeToString(digest[:]), Category: "blueprints", Source: "blueprint"}
	responseRaw := f.require(t, token, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", body, 200)
	var response struct {
		Data struct {
			test016Upload
			Category    string `json:"category"`
			Source      string `json:"source"`
			BlueprintID string `json:"blueprintId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseRaw, &response); err != nil {
		t.Fatal(err)
	}
	presign := response.Data
	if !presign.UploadRequired || presign.Method != http.MethodPut || !strings.HasPrefix(presign.URL, f.provider.URL+"/") || !validCatalogPublicID(presign.BlueprintID) {
		t.Fatalf("not an actual new owned blueprint upload: %s", responseRaw)
	}
	request, err := http.NewRequestWithContext(f.ctx, presign.Method, presign.URL, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range presign.Headers {
		request.Header.Set(key, value)
	}
	uploaded, err := f.provider.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, uploaded.Body)
	_ = uploaded.Body.Close()
	if uploaded.StatusCode != 200 || readErr != nil {
		t.Fatalf("actual upload status=%d err=%v", uploaded.StatusCode, readErr)
	}
	complete := ossCompleteUploadRequest{ObjectKey: presign.ObjectKey, OriginalName: name, ContentType: body.ContentType, SizeBytes: body.SizeBytes, SHA256: body.SHA256, Category: presign.Category, Source: presign.Source}
	responseRaw = f.require(t, token, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", complete, http.StatusCreated)
	var completed struct {
		Data struct {
			BlueprintID string                             `json:"blueprintId"`
			Blueprint   struct{ ID, Status, JobID string } `json:"blueprint"`
		} `json:"data"`
	}
	if err = json.Unmarshal(responseRaw, &completed); err != nil {
		t.Fatal(err)
	}
	result := test036UploadResult{publicID: completed.Data.BlueprintID, jobPublicID: completed.Data.Blueprint.JobID, format: blueprintFormatFromName(name), original: bytes.Clone(raw)}
	if result.publicID != presign.BlueprintID || completed.Data.Blueprint.ID != result.publicID || completed.Data.Blueprint.Status != "queued" || !validCatalogPublicID(result.jobPublicID) {
		t.Fatalf("completion bypassed durable job: %s", responseRaw)
	}
	if err = f.db.QueryRow(f.ctx, "select id from blueprint_jobs where public_id=$1 and status='queued' and operation='normalize' and created_by=$2", result.jobPublicID, f.userIDs[token]).Scan(&result.jobID); err != nil {
		t.Fatal(err)
	}
	var intents int
	if err = f.db.QueryRow(f.ctx, "select count(*) from nats_outbox where event_type='blueprint.conversion.requested' and aggregate_type='blueprint_job' and aggregate_id=$1 and (payload->>'jobId')::bigint=$2", result.jobPublicID, result.jobID).Scan(&intents); err != nil || intents != 1 {
		t.Fatalf("job durable intent=%d err=%v", intents, err)
	}
	return result
}

func (f test036Fixture) approve(t *testing.T, publicID string) {
	t.Helper()
	var requestID string
	if err := f.db.QueryRow(f.ctx, "select revision.public_id from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id where request.aggregate_type='blueprint' and request.aggregate_key=$1 and request.status='pending'", publicID).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+requestID, map[string]string{"status": "approved", "note": "TEST036 independent review"}, 200)
	var published bool
	var status string
	if err := f.db.QueryRow(f.ctx, "select review_status,published_revision_id is not null from blueprints where public_id=$1", publicID).Scan(&status, &published); err != nil || status != "approved" || !published {
		t.Fatalf("not actually published status=%s published=%t err=%v", status, published, err)
	}
}

func (f test036Fixture) object(t *testing.T, key string) []byte {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	object, exists := f.objects["/"+strings.TrimPrefix(key, "/")]
	if !exists {
		t.Fatalf("missing real owned object %s", key)
	}
	return bytes.Clone(object.data)
}

func TestTEST036GoldenUploadNormalizeReviewConvertAndDownloadFullHTTPIntegration(t *testing.T) {
	for _, format := range []string{"nbt", "schem", "litematic"} {
		t.Run(format, func(t *testing.T) {
			f := newTEST036Fixture(t)
			document := test036Document()
			raw, _, err := encodeBlueprint(document, format)
			if err != nil {
				t.Fatal(err)
			}
			upload := f.upload(t, f.editor, "test036-golden."+format, raw)
			path := "/api/v1/blueprints/" + upload.publicID
			before := f.facts(t)
			f.require(t, "", http.MethodGet, path, nil, 404)
			f.require(t, f.otherEditor, http.MethodGet, path, nil, 404)
			f.require(t, f.otherEditor, http.MethodGet, path+"/content", nil, 403)
			f.require(t, f.otherEditor, http.MethodPut, path, blueprintUpdateRequest{Title: "foreign metadata"}, 403)
			f.unchanged(t, before)
			if err = f.worker("normalize").processJob(f.ctx, upload.jobID); err != nil {
				t.Fatal(err)
			}
			var status, reviewStatus, normalizedKey, coverKey string
			var count, materials int
			if err = f.db.QueryRow(f.ctx, `select status,review_status,normalized_object_key,cover_object_key,block_count,
   (select count(*) from blueprint_materials material where material.blueprint_id=blueprints.id)
   from blueprints where public_id=$1`, upload.publicID).Scan(&status, &reviewStatus, &normalizedKey, &coverKey, &count, &materials); err != nil {
				t.Fatal(err)
			}
			if status != "ready" || reviewStatus != "pending" || count != 2 || materials != 2 || normalizedKey == "" || coverKey == "" {
				t.Fatalf("normalization status=%s review=%s blocks/materials=%d/%d keys=%s/%s", status, reviewStatus, count, materials, normalizedKey, coverKey)
			}
			normalized := f.require(t, f.editor, http.MethodGet, path+"/render", nil, 200)
			var decoded blueprintDocument
			if err = json.Unmarshal(normalized, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Size != document.Size || decoded.DataVersion != document.DataVersion || !reflect.DeepEqual(decoded.Blocks, document.Blocks) {
				t.Fatalf("normalized bytes lost golden blocks: %#v", decoded)
			}
			f.require(t, "", http.MethodGet, path+"/render", nil, 404)
			f.require(t, "", http.MethodGet, path+"/cover", nil, http.StatusNoContent)
			f.approve(t, upload.publicID)
			detail := f.require(t, "", http.MethodGet, path, nil, 200)
			if !bytes.Contains(detail, []byte(`"blockCount":2`)) || !bytes.Contains(detail, []byte(`"canEdit":false`)) {
				t.Fatalf("approved public detail=%s", detail)
			}
			f.require(t, "", http.MethodGet, path+"/render", nil, 200)
			before = f.facts(t)
			f.require(t, "", http.MethodPost, path+"/convert", map[string]string{"format": "nbt"}, 401)
			f.require(t, f.otherEditor, http.MethodPost, path+"/convert", map[string]string{"format": "unsupported"}, 400)
			f.unchanged(t, before)
			target := "schem"
			if format == "schem" {
				target = "nbt"
			}
			conversionRaw := f.require(t, f.otherEditor, http.MethodPost, path+"/convert", map[string]string{"format": target}, 202)
			queued := decodeTEST022Data[map[string]string](t, conversionRaw)
			var jobID int64
			if err = f.db.QueryRow(f.ctx, "select id from blueprint_jobs where public_id=$1 and status='queued' and operation='convert' and created_by=$2", queued["jobId"], f.userIDs[f.otherEditor]).Scan(&jobID); err != nil {
				t.Fatal(err)
			}
			if err = f.worker("convert").processJob(f.ctx, jobID); err != nil {
				t.Fatal(err)
			}
			var variantID, variantKey string
			if err = f.db.QueryRow(f.ctx, "select public_id,object_key from blueprint_variants where blueprint_id=(select id from blueprints where public_id=$1) and format=$2 and not original and status='ready'", upload.publicID, target).Scan(&variantID, &variantKey); err != nil {
				t.Fatal(err)
			}
			converted, err := decodeBlueprint(f.object(t, variantKey), target, "roundtrip")
			if err != nil || converted.Size != document.Size || !reflect.DeepEqual(converted.Blocks, document.Blocks) {
				t.Fatalf("conversion lost bytes: document=%#v err=%v", converted, err)
			}
			download := f.require(t, "", http.MethodPost, path+"/variants/"+variantID+"/download", nil, 200)
			access := decodeTEST022Data[map[string]any](t, download)
			accessURL, _ := access["url"].(string)
			if !strings.HasPrefix(accessURL, f.provider.URL+"/") {
				t.Fatalf("not owned download URL: %s", download)
			}
			response, err := f.provider.Client().Get(accessURL)
			if err != nil {
				t.Fatal(err)
			}
			bytesDownloaded, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if response.StatusCode != 200 || readErr != nil || !bytes.Equal(bytesDownloaded, f.object(t, variantKey)) {
				t.Fatalf("actual variant GET status=%d err=%v", response.StatusCode, readErr)
			}
			f.require(t, f.otherEditor, http.MethodPost, path+"/convert", map[string]string{"format": target}, 200)
			if err = f.worker("redelivery").processJob(f.ctx, jobID); err != nil {
				t.Fatal(err)
			}
			var completed, intents int
			if err = f.db.QueryRow(f.ctx, `select (select count(*) from blueprint_jobs where id=$1 and status='completed' and attempts=1),
   (select count(*) from nats_outbox where payload->>'templateKey'='blueprint_format_success' and (payload->>'recipientId')::bigint=$2)`, jobID, f.userIDs[f.otherEditor]).Scan(&completed, &intents); err != nil || completed != 1 || intents != 1 {
				t.Fatalf("redelivery completed=%d intents=%d err=%v", completed, intents, err)
			}
		})
	}
}

func TestWithdrawnBlueprintConversionStopsBeforeCreatingArtifacts(t *testing.T) {
	f := newTEST036Fixture(t)
	var id int64
	var publicID string
	err := f.db.QueryRow(f.ctx, `insert into blueprints(owner_id,title,source_format,status,review_status)
		values($1,'Withdrawn conversion','nbt','ready','approved') returning id,public_id`, f.userIDs[f.editor]).Scan(&id, &publicID)
	if err != nil {
		t.Fatal(err)
	}
	raw := f.require(t, f.otherEditor, http.MethodPost, "/api/v1/blueprints/"+publicID+"/convert", map[string]string{"format": "schem"}, http.StatusAccepted)
	result := decodeTEST022Data[struct {
		JobID string `json:"jobId"`
	}](t, raw)
	var jobID int64
	if err = f.db.QueryRow(f.ctx, "select id from blueprint_jobs where public_id=$1", result.JobID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update blueprints set status='deleted' where id=$1", id); err != nil {
		t.Fatal(err)
	}
	if err = f.worker("withdrawn-convert").processJob(f.ctx, jobID); !errors.Is(err, errBlueprintJobWithdrawn) {
		t.Fatalf("withdrawn conversion: %v", err)
	}
	var state string
	var attempts, artifacts, notifications int
	err = f.db.QueryRow(f.ctx, `select j.status,j.attempts,
		(select count(*) from blueprint_job_artifacts where job_id=j.id),
		(select count(*) from nats_outbox o where o.aggregate_type='blueprint' and o.aggregate_id=$2 and o.event_type like 'blueprint.notification.%')
		from blueprint_jobs j where j.id=$1`, jobID, publicID).Scan(&state, &attempts, &artifacts, &notifications)
	if err != nil || state != "failed" || attempts != 1 || artifacts != 0 || notifications != 0 {
		t.Fatalf("withdrawn job status=%q attempts=%d artifacts=%d notifications=%d error=%v", state, attempts, artifacts, notifications, err)
	}
}

func TestBlueprintFailureNotificationUsesCurrentVisibility(t *testing.T) {
	f := newTEST036Fixture(t)
	for _, path := range []string{"worker", "recovery"} {
		for _, visibility := range []string{"published", "withdrawn"} {
			t.Run(path+"/"+visibility, func(t *testing.T) {
				var id int64
				var publicID string
				err := f.db.QueryRow(f.ctx, `insert into blueprints(owner_id,title,source_format,status,review_status)
					values($1,'Conversion notice test','nbt','ready','approved') returning id,public_id`, f.userIDs[f.editor]).Scan(&id, &publicID)
				if err != nil {
					t.Fatal(err)
				}
				raw := f.require(t, f.otherEditor, http.MethodPost, "/api/v1/blueprints/"+publicID+"/convert",
					map[string]string{"format": "schem"}, http.StatusAccepted)
				queued := decodeTEST022Data[map[string]string](t, raw)
				var jobID int64
				if err = f.db.QueryRow(f.ctx, "update blueprint_jobs set max_attempts=1 where public_id=$1 returning id", queued["jobId"]).Scan(&jobID); err != nil {
					t.Fatal(err)
				}
				worker := f.worker("notice-" + path)
				job, claimed, err := worker.claimBlueprintJob(f.ctx, jobID)
				if err != nil || !claimed {
					t.Fatalf("claim %d: claimed=%t err=%v", jobID, claimed, err)
				}
				if visibility == "withdrawn" {
					if _, err = f.db.Exec(f.ctx, "update blueprints set status='deleted' where id=$1", id); err != nil {
						t.Fatal(err)
					}
				}
				if path == "worker" {
					terminal, failErr := worker.failBlueprintJob(f.ctx, job, errors.New("synthetic conversion failure"))
					if failErr != nil || !terminal {
						t.Fatalf("failed job: terminal=%t err=%v", terminal, failErr)
					}
				} else {
					if _, err = f.db.Exec(f.ctx, "update blueprint_jobs set lease_expires_at=now()-interval '1 second' where id=$1", jobID); err != nil {
						t.Fatal(err)
					}
					requeued, exhausted, recoverErr := worker.recoverStaleBlueprintJobs(f.ctx)
					if recoverErr != nil || requeued != 0 || exhausted != 1 {
						t.Fatalf("recover: requeued=%d exhausted=%d err=%v", requeued, exhausted, recoverErr)
					}
				}
				var status string
				var notices int
				err = f.db.QueryRow(f.ctx, `select j.status,(select count(*) from nats_outbox
					where aggregate_type='blueprint' and aggregate_id=$2 and event_type='blueprint.notification.blueprint_conversion_failure'
					and (payload->>'recipientId')::bigint=$3) from blueprint_jobs j where j.id=$1`,
					jobID, publicID, f.userIDs[f.otherEditor]).Scan(&status, &notices)
				expected := 1
				if visibility == "withdrawn" {
					expected = 0
				}
				if err != nil || status != "failed" || notices != expected {
					t.Fatalf("status=%q notices=%d want=%d err=%v", status, notices, expected, err)
				}
			})
		}
	}
}

func TestBlueprintOwnerCanRetryFailedNormalizationOnceOverHTTP(t *testing.T) {
	f := newTEST036Fixture(t)
	var id int64
	var publicID string
	err := f.db.QueryRow(f.ctx, `insert into blueprints(owner_id,title,source_format,status,review_status,original_object_key,last_error)
		values($1,'Retry fixture','nbt','failed','pending','synthetic/retry.nbt','earlier attempt') returning id,public_id`, f.userIDs[f.editor]).Scan(&id, &publicID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/v1/blueprints/" + publicID + "/retry"
	f.require(t, f.otherEditor, http.MethodPost, endpoint, nil, http.StatusForbidden)
	f.require(t, f.editor, http.MethodPost, endpoint, nil, http.StatusAccepted)
	f.require(t, f.editor, http.MethodPost, endpoint, nil, http.StatusConflict)
	var status string
	var jobs, events int
	err = f.db.QueryRow(f.ctx, `select b.status,
		(select count(*) from blueprint_jobs where blueprint_id=b.id),
		(select count(*) from nats_outbox o join blueprint_jobs j on o.aggregate_id=j.public_id
		 where o.aggregate_type='blueprint_job' and o.event_type='blueprint.conversion.requested' and j.blueprint_id=b.id)
		from blueprints b where b.id=$1`, id).Scan(&status, &jobs, &events)
	if err != nil || status != "queued" || jobs != 1 || events != 1 {
		t.Fatalf("retry status=%q jobs=%d events=%d error=%v", status, jobs, events, err)
	}
}

func TestBlueprintDetailScopesProcessingNotesAndStorageMetadata(t *testing.T) {
	f := newTEST036Fixture(t)
	var publicID string
	const note = "synthetic converter error"
	if err := f.db.QueryRow(f.ctx, `insert into blueprints(owner_id,title,source_format,status,review_status,original_object_key,last_error)
		values($1,'Detail projection','nbt','failed','approved','synthetic/original.nbt',$2) returning public_id`, f.userIDs[f.editor], note).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	var ownerPublicID string
	if err := f.db.QueryRow(f.ctx, "select public_id from users where id=$1", f.userIDs[f.editor]).Scan(&ownerPublicID); err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []struct {
		name, token, expectedNote string
	}{
		{"visitor", "", ""}, {"other editor", f.otherEditor, ""}, {"owner", f.editor, note},
	} {
		t.Run(viewer.name, func(t *testing.T) {
			data := decodeTEST022Data[map[string]any](t, f.require(t, viewer.token, http.MethodGet, "/api/v1/blueprints/"+publicID, nil, http.StatusOK))
			if data["lastError"] != viewer.expectedNote || data["id"] != publicID || data["status"] != "failed" {
				t.Fatalf("detail status=%v id=%v note=%v", data["status"], data["id"], data["lastError"])
			}
			uploader, ok := data["uploader"].(map[string]any)
			if !ok || uploader["id"] != ownerPublicID {
				t.Fatalf("uploader id=%v, want profile id %s", uploader["id"], ownerPublicID)
			}
			for _, key := range []string{"originalObjectKey", "originalFileId"} {
				if _, found := data[key]; found {
					t.Fatalf("detail includes internal storage field %s", key)
				}
			}
		})
	}
}

func TestTEST036UnknownNBTStateCannotSilentlyPublishEmptyBlueprintFullHTTPIntegration(t *testing.T) {
	f := newTEST036Fixture(t)
	raw, _, err := encodeGzipNBT(map[string]any{
		"size": []int32{1, 1, 1}, "DataVersion": int32(3955),
		"palette": []map[string]any{{"Name": "minecraft:stone"}},
		"blocks":  []map[string]any{{"pos": []int32{0, 0, 0}, "state": int32(1)}},
	}, "TEST036 invalid palette")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "test036-invalid-palette.nbt", raw)
	worker := f.worker("invalid-palette")
	for attempt := 1; attempt <= 3; attempt++ {
		if err = worker.processJob(f.ctx, upload.jobID); err == nil {
			t.Fatal("invalid palette state was silently dropped and normalization succeeded")
		}
	}
	var status, key string
	var attempts int
	var published bool
	if err = f.db.QueryRow(f.ctx, `select blueprint.status,blueprint.normalized_object_key,blueprint.published_revision_id is not null,job.attempts
  from blueprints blueprint join blueprint_jobs job on job.blueprint_id=blueprint.id where job.id=$1`, upload.jobID).Scan(&status, &key, &published, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || key != "" || published || attempts != 3 {
		t.Fatalf("invalid source status=%s key=%s published=%t attempts=%d", status, key, published, attempts)
	}
	f.require(t, "", http.MethodGet, "/api/v1/blueprints/"+upload.publicID, nil, 404)
}

func TestTEST036CompletionNotificationFailureMustNotLoseRecoverableJobIntegration(t *testing.T) {
	f := newTEST036Fixture(t)
	raw, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "test036-notification.nbt", raw)
	if _, err = f.db.Exec(f.ctx, `alter table nats_outbox add constraint test036_reject_completion
  check(coalesce(payload->>'templateKey','')<>'blueprint_conversion_success') not valid`); err != nil {
		t.Fatal(err)
	}
	if err = f.worker("notification-failure").processJob(f.ctx, upload.jobID); err == nil {
		t.Fatal("job reported completed success despite losing its durable completion notification")
	}
	var status string
	var normalized string
	if err = f.db.QueryRow(f.ctx, "select status,normalized_object_key from blueprints where public_id=$1", upload.publicID).Scan(&status, &normalized); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || normalized != "" {
		t.Fatalf("failed notification left unrecoverable publication status=%s key=%s", status, normalized)
	}
	if _, err = f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test036_reject_completion"); err != nil {
		t.Fatal(err)
	}
	if err = f.worker("notification-restart").processJob(f.ctx, upload.jobID); err != nil {
		t.Fatal(err)
	}
	var intents, completed int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from nats_outbox where payload->>'templateKey'='blueprint_conversion_success'),
  (select count(*) from blueprint_jobs where id=$1 and status='completed' and attempts=2)`, upload.jobID).Scan(&intents, &completed); err != nil || intents != 1 || completed != 1 {
		t.Fatalf("restart intents=%d completed=%d err=%v", intents, completed, err)
	}
}

func TestTEST036BlueprintReviewNotificationFailureIsAtomicFullHTTPIntegration(t *testing.T) {
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			f := newTEST036Fixture(t)
			raw, _, err := encodeBlueprint(test036Document(), "nbt")
			if err != nil {
				t.Fatal(err)
			}
			upload := f.upload(t, f.editor, "test036-review-"+decision+".nbt", raw)
			if err = f.worker("review-source").processJob(f.ctx, upload.jobID); err != nil {
				t.Fatal(err)
			}
			var requestID string
			if err = f.db.QueryRow(f.ctx, "select revision.public_id from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id where request.aggregate_type='blueprint' and request.aggregate_key=$1 and request.status='pending'", upload.publicID).Scan(&requestID); err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, "alter table nats_outbox add constraint test036_reject_review check(coalesce(payload->>'templateKey','') not in ('review_approved','review_rejected')) not valid"); err != nil {
				t.Fatal(err)
			}
			before := f.facts(t)
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+requestID, map[string]string{"status": decision, "note": "atomic review notification"}, 500)
			f.unchanged(t, before)
			if _, err = f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test036_reject_review"); err != nil {
				t.Fatal(err)
			}
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+requestID, map[string]string{"status": decision, "note": "retry after storage recovery"}, 200)
		})
	}
}

func TestTEST036AdministratorMetadataEditRetainsOwnersBoundCoverFullHTTPIntegration(t *testing.T) {
	f := newTEST036Fixture(t)
	raw, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "test036-admin-cover.nbt", raw)
	if err = f.worker("admin-cover").processJob(f.ctx, upload.jobID); err != nil {
		t.Fatal(err)
	}
	f.approve(t, upload.publicID)
	var fileID, ownerID int64
	var key string
	if err = f.db.QueryRow(f.ctx, "select cover_file_id,cover_object_key,owner_id from blueprints where public_id=$1", upload.publicID).Scan(&fileID, &key, &ownerID); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	f.require(t, f.otherEditor, http.MethodPut, "/api/v1/blueprints/"+upload.publicID, blueprintUpdateRequest{Title: "not an administrator"}, 403)
	f.unchanged(t, before)
	// An actual DB permission binding and live session, not injected admin claims.
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "admin.*")
	response := f.require(t, f.denied, http.MethodPut, "/api/v1/blueprints/"+upload.publicID, blueprintUpdateRequest{Title: "Administrator retained owner cover", Description: "metadata only"}, 200)
	result := decodeTEST022Data[struct {
		Updated        bool   `json:"updated"`
		ReviewRequired bool   `json:"reviewRequired"`
		RevisionID     string `json:"revisionId"`
	}](t, response)
	if !result.Updated || result.ReviewRequired || !validCatalogPublicID(result.RevisionID) {
		t.Fatalf("actual administrator edit did not publish: %s", response)
	}
	var newFileID, newOwnerID int64
	var newKey, title string
	if err = f.db.QueryRow(f.ctx, "select cover_file_id,cover_object_key,owner_id,title from blueprints where public_id=$1", upload.publicID).Scan(&newFileID, &newKey, &newOwnerID, &title); err != nil {
		t.Fatal(err)
	}
	if newFileID != fileID || newKey != key || newOwnerID != ownerID || title != "Administrator retained owner cover" {
		t.Fatalf("metadata changed owner binding: file=%d/%d key=%s/%s owner=%d/%d title=%s", newFileID, fileID, newKey, key, newOwnerID, ownerID, title)
	}
	after := f.facts(t)
	for _, table := range []string{"oss_files", "blueprint_job_artifacts", "oss_object_deletion_outbox", "blueprint_variants"} {
		if before[table] != after[table] {
			t.Errorf("metadata-only administrator edit mutated %s", table)
		}
	}
	f.require(t, "", http.MethodGet, "/api/v1/blueprints/"+upload.publicID+"/cover", nil, 200)
}

func TestTEST036ExpiredLeaseRestartCompensatesActualUploadedArtifactIntegration(t *testing.T) {
	f := newTEST036Fixture(t)
	document := test036Document()
	raw, _, err := encodeBlueprint(document, "nbt")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "test036-restart-source.nbt", raw)
	first := f.worker("crashed-after-upload")
	job, claimed, err := first.claimBlueprintJob(f.ctx, upload.jobID)
	if err != nil || !claimed {
		t.Fatalf("actual job claim: claimed=%v err=%v", claimed, err)
	}
	normalized, err := encodeNormalizedBlueprintJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.server.ossConfigFromSettings(f.ctx)
	oldKey := blueprintArtifactObjectKey(cfg.Prefix, ossBlueprintReleaseCategory(upload.publicID, "normalized"), job.id, job.attempts, blueprintArtifactNormalized, "json")
	artifact, err := first.writePendingBlueprintArtifact(f.ctx, job.id, job.attempts, job.runToken, blueprintArtifactNormalized, ossBlueprintReleaseCategory(upload.publicID, "normalized"), "json", "blueprint.json", "application/json", normalized, job.createdBy, "blueprint_normalized")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.object(t, oldKey), normalized) {
		t.Fatal("crash fixture never actually uploaded its pending artifact")
	}
	// Expire only this owned lease; recovery uses the unchanged original clock,
	// 2-minute lease policy, run-token fence, and production recovery transaction.
	if _, err = f.db.Exec(f.ctx, "update blueprint_jobs set lease_expires_at=now()-interval '1 second' where id=$1", job.id); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "alter table nats_outbox add constraint test036_reject_recovery check(event_type<>'blueprint.conversion.recovered') not valid"); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	restart := f.worker("fresh-recovery-worker")
	if _, _, err = restart.recoverStaleBlueprintJobs(f.ctx); err == nil {
		t.Fatal("recovery ignored durable intent failure")
	}
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test036_reject_recovery"); err != nil {
		t.Fatal(err)
	}
	recovered, exhausted, err := restart.recoverStaleBlueprintJobs(f.ctx)
	if err != nil || recovered != 1 || exhausted != 0 {
		t.Fatalf("restart recovery=%d/%d err=%v", recovered, exhausted, err)
	}
	var artifactStatus, fileStatus, jobStatus string
	var deletionIntents int
	if err = f.db.QueryRow(f.ctx, `select artifact.status,file.status,job.status,
  (select count(*) from oss_object_deletion_outbox where object_key=$2 and status='pending')
  from blueprint_job_artifacts artifact join oss_files file on file.id=artifact.file_id join blueprint_jobs job on job.id=artifact.job_id
  where artifact.id=$1`, artifact.id, oldKey).Scan(&artifactStatus, &fileStatus, &jobStatus, &deletionIntents); err != nil {
		t.Fatal(err)
	}
	if artifactStatus != "abandoned" || fileStatus != "deleted" || jobStatus != "queued" || deletionIntents != 1 {
		t.Fatalf("restart compensation=%s/%s/%s deletes=%d", artifactStatus, fileStatus, jobStatus, deletionIntents)
	}
	before = f.facts(t)
	if err = first.completeBlueprintJob(f.ctx, job.id, job.runToken); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("stale owner completion was not fenced: %v", err)
	}
	if err = first.renewBlueprintJobLease(f.ctx, job.id, job.runToken); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("stale owner heartbeat was not fenced: %v", err)
	}
	if err = first.normalizeBlueprint(f.ctx, job.id, job.attempts, job.runToken, job.blueprintID, job.createdBy); !errors.Is(err, errBlueprintJobLeaseLost) {
		t.Fatalf("stale owner resumed normalization instead of being fenced: %v", err)
	}
	f.unchanged(t, before)
	if err = restart.processJob(f.ctx, job.id); err != nil {
		t.Fatal(err)
	}
	var newKey string
	var attempts int
	if err = f.db.QueryRow(f.ctx, "select blueprint.normalized_object_key,job.attempts from blueprints blueprint join blueprint_jobs job on job.blueprint_id=blueprint.id where job.id=$1 and job.status='completed' and blueprint.status='ready'", job.id).Scan(&newKey, &attempts); err != nil {
		t.Fatal(err)
	}
	if newKey == oldKey || attempts != 2 {
		t.Fatalf("restart reused immutable old attempt key=%s attempts=%d", newKey, attempts)
	}
	NewOSSDeletionWorker(f.server.cfg, f.db).drain(f.ctx)
	f.mu.Lock()
	_, oldExists := f.objects["/"+oldKey]
	f.mu.Unlock()
	if oldExists {
		t.Fatal("fresh deletion worker did not delete actual abandoned object")
	}
	var completedDeletes int
	if err = f.db.QueryRow(f.ctx, "select count(*) from oss_object_deletion_outbox where object_key=$1 and status='completed'", oldKey).Scan(&completedDeletes); err != nil || completedDeletes != 1 {
		t.Fatalf("delete acknowledgement=%d err=%v", completedDeletes, err)
	}
	if len(f.object(t, newKey)) == 0 {
		t.Fatal("cleanup deleted new attempt bytes")
	}
	if recovered, exhausted, err = restart.recoverStaleBlueprintJobs(f.ctx); err != nil || recovered != 0 || exhausted != 0 {
		t.Fatalf("second recovery not idempotent=%d/%d err=%v", recovered, exhausted, err)
	}
}

func TestBlueprintConversionUsesTheVariantPublishedWhileAdmissionWaits(t *testing.T) {
	f := newTEST036Fixture(t)
	raw, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "publishing.nbt", raw)
	if err := f.worker("normalize").processJob(f.ctx, upload.jobID); err != nil {
		t.Fatal(err)
	}
	var variantID string
	if err := f.db.QueryRow(f.ctx, `update blueprint_variants set status='processing'
		where blueprint_id=(select id from blueprints where public_id=$1) and format='nbt' and original returning public_id`,
		upload.publicID).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	// This fixture owns its database. A site's default can be stronger than
	// read committed; the admission transaction needs a fresh snapshot after waiting.
	databaseName := pgx.Identifier{f.db.Config().ConnConfig.Database}.Sanitize()
	if _, err := f.db.Exec(f.ctx, "alter database "+databaseName+" set default_transaction_isolation='repeatable read'"); err != nil {
		t.Fatal(err)
	}
	f.db.Reset()
	var defaultIsolation string
	if err := f.db.QueryRow(f.ctx, "show default_transaction_isolation").Scan(&defaultIsolation); err != nil || defaultIsolation != "repeatable read" {
		t.Fatalf("fixture isolation=%q err=%v", defaultIsolation, err)
	}
	publisher, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Rollback(f.ctx)
	if _, err := publisher.Exec(f.ctx, `select id from blueprints where public_id=$1 for no key update`, upload.publicID); err != nil {
		t.Fatal(err)
	}
	// Hold the existing admission budget as well so that a caller which
	// reads the variant before the publication commits also has to wait.
	if _, err := publisher.Exec(f.ctx, `select pg_advisory_xact_lock(hashtextextended('blueprint-conversion-global',0))`); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Exec(f.ctx, `update blueprint_variants set status='ready' where public_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	result := auditCompletionAsyncJSON(f.test013Fixture, f.editor, http.MethodPost, "/api/v1/blueprints/"+upload.publicID+"/convert", map[string]string{"format": "nbt"})
	auditCompletionAwaitLocks(t, f.test013Fixture, 1)
	if err := publisher.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	response := <-result
	if response.err != nil || response.status != http.StatusOK {
		t.Fatalf("expected the published variant, status=%d err=%v body=%s", response.status, response.err, response.raw)
	}
	data := decodeTEST022Data[struct {
		VariantID     string
		AlreadyExists bool
	}](t, response.raw)
	if !data.AlreadyExists || data.VariantID != variantID {
		t.Fatalf("published variant %q, received %+v", variantID, data)
	}
	var extraJobs int
	if err := f.db.QueryRow(f.ctx, `select count(*) from blueprint_jobs where operation='convert'
		and blueprint_id=(select id from blueprints where public_id=$1)`, upload.publicID).Scan(&extraJobs); err != nil || extraJobs != 0 {
		t.Fatalf("extra conversion jobs=%d, err=%v", extraJobs, err)
	}
}

func TestTEST036PublicConversionConcurrentHTTPAdmissionAndDeduplication(t *testing.T) {
	for _, mode := range []string{"per-user", "global", "same-operation"} {
		t.Run(mode, func(t *testing.T) {
			f := newTEST036Fixture(t)
			attempts, wantAccepted := 5, maxBlueprintActiveJobsPerUser
			if mode == "global" {
				attempts, wantAccepted = maxBlueprintActiveConversionsGlobal+1, maxBlueprintActiveConversionsGlobal
			}
			if mode == "same-operation" {
				attempts, wantAccepted = 16, 1
			}
			targets := make([]string, attempts)
			tokens := make([]string, attempts)
			for index := range attempts {
				tokens[index] = f.otherEditor
				if mode == "global" {
					_, _, tokens[index] = createTEST044User(t, f.ctx, f.db, f.server.cfg, fmt.Sprintf("test036-public-%02d", index), "UTC")
				}
				if mode == "same-operation" && index > 0 {
					targets[index] = targets[0]
					continue
				}
				document := test036Document()
				document.DataVersion += index
				raw, _, err := encodeBlueprint(document, "nbt")
				if err != nil {
					t.Fatal(err)
				}
				upload := f.upload(t, f.editor, fmt.Sprintf("test036-public-%02d.nbt", index), raw)
				if err = f.worker(fmt.Sprintf("public-normalize-%d", index)).processJob(f.ctx, upload.jobID); err != nil {
					t.Fatal(err)
				}
				f.approve(t, upload.publicID)
				targets[index] = upload.publicID
			}
			type outcome struct {
				status int
				body   []byte
				retry  string
				err    error
			}
			results := make(chan outcome, attempts)
			start := make(chan struct{})
			var ready sync.WaitGroup
			ready.Add(attempts)
			for index := range attempts {
				go func(index int) {
					ready.Done()
					<-start
					request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+"/api/v1/blueprints/"+targets[index]+"/convert", strings.NewReader(`{"format":"schem"}`))
					if err != nil {
						results <- outcome{err: err}
						return
					}
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Authorization", "Bearer "+tokens[index])
					response, err := f.origin.Client().Do(request)
					if err != nil {
						results <- outcome{err: err}
						return
					}
					raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
					_ = response.Body.Close()
					results <- outcome{status: response.StatusCode, body: raw, retry: response.Header.Get("Retry-After"), err: err}
				}(index)
			}
			ready.Wait()
			close(start)
			accepted, rejected := 0, 0
			for range attempts {
				result := <-results
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.status == 202 {
					accepted++
					continue
				}
				wantStatus, wantCode, wantRetry := 429, "BLUEPRINT_JOB_CONCURRENCY_LIMIT", "60"
				if mode == "global" {
					wantCode = "BLUEPRINT_GLOBAL_CONCURRENCY_LIMIT"
				}
				if mode == "same-operation" {
					wantStatus, wantCode, wantRetry = 409, "BLUEPRINT_CONVERSION_ALREADY_QUEUED", "30"
				}
				if result.status != wantStatus || !bytes.Contains(result.body, []byte(wantCode)) || result.retry != wantRetry {
					t.Fatalf("%s admission status=%d retry=%s body=%s", mode, result.status, result.retry, result.body)
				}
				rejected++
			}
			if accepted != wantAccepted || rejected != attempts-wantAccepted {
				t.Fatalf("%s actual HTTP admissions=%d/%d want=%d/%d", mode, accepted, rejected, wantAccepted, attempts-wantAccepted)
			}
			var jobs, intents int
			if err := f.db.QueryRow(f.ctx, `select (select count(*) from blueprint_jobs where operation='convert' and status='queued'),
   (select count(*) from nats_outbox event join blueprint_jobs job on event.aggregate_id=job.public_id
    where event.event_type='blueprint.conversion.requested' and job.operation='convert')`).Scan(&jobs, &intents); err != nil {
				t.Fatal(err)
			}
			if jobs != wantAccepted || intents != wantAccepted {
				t.Fatalf("%s durable jobs/intents=%d/%d want %d", mode, jobs, intents, wantAccepted)
			}
		})
	}
}

func TestTEST036EntityPayloadPreservedAndLossyConversionFailsWithoutDamagingSourceFullHTTPIntegration(t *testing.T) {
	f := newTEST036Fixture(t)
	root := map[string]any{
		"size": []int32{1, 1, 1}, "DataVersion": int32(3955),
		"palette":  []map[string]any{{"Name": "minecraft:chest"}},
		"blocks":   []map[string]any{{"pos": []int32{0, 0, 0}, "state": int32(0), "nbt": map[string]any{"id": "minecraft:chest", "Items": []map[string]any{{"id": "minecraft:diamond", "Count": int8(1)}}}}},
		"entities": []map[string]any{{"pos": []float64{0.5, 0.0, 0.5}, "blockPos": []int32{0, 0, 0}, "nbt": map[string]any{"id": "minecraft:armor_stand", "CustomName": "TEST036 entity"}}},
	}
	raw, _, err := encodeGzipNBT(root, "TEST036 source with entities")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := decodeBlueprint(raw, "nbt", "entities")
	if err != nil || len(expected.BlockEntities) != 1 || len(expected.Entities) != 1 {
		t.Fatalf("invalid fidelity fixture: blockEntities=%d entities=%d err=%v", len(expected.BlockEntities), len(expected.Entities), err)
	}
	upload := f.upload(t, f.editor, "test036-entities.nbt", raw)
	if err = f.worker("entity-normalize").processJob(f.ctx, upload.jobID); err != nil {
		t.Fatal(err)
	}
	normalized := f.require(t, f.editor, http.MethodGet, "/api/v1/blueprints/"+upload.publicID+"/render", nil, 200)
	var actual blueprintDocument
	if err = json.Unmarshal(normalized, &actual); err != nil {
		t.Fatal(err)
	}
	// JSON-number normalization is intentional; compare complete JSON trees so
	// NBT integer widths do not cause a false loss-of-data assertion.
	expectedJSON, err := json.Marshal(map[string]any{"blocks": expected.Blocks, "blockEntities": expected.BlockEntities, "entities": expected.Entities})
	if err != nil {
		t.Fatal(err)
	}
	actualJSON, err := json.Marshal(map[string]any{"blocks": actual.Blocks, "blockEntities": actual.BlockEntities, "entities": actual.Entities})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expectedJSON, actualJSON) {
		t.Fatalf("normalization lost entity payload: expected=%s actual=%s", expectedJSON, actualJSON)
	}
	f.approve(t, upload.publicID)
	response := f.require(t, f.otherEditor, http.MethodPost, "/api/v1/blueprints/"+upload.publicID+"/convert", map[string]string{"format": "schem"}, 202)
	var jobID int64
	if err = f.db.QueryRow(f.ctx, "select id from blueprint_jobs where public_id=$1", decodeTEST022Data[map[string]string](t, response)["jobId"]).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err = f.worker("lossless-refusal").processJob(f.ctx, jobID); !errors.Is(err, errBlueprintEntityDataWouldBeLost) {
		t.Fatalf("lossy conversion did not fail explicitly: %v", err)
	}
	var sourceStatus, jobStatus, lastError string
	var attempts, generated, notices int
	if err = f.db.QueryRow(f.ctx, `select blueprint.status,job.status,job.last_error,job.attempts,
  (select count(*) from blueprint_variants where blueprint_id=blueprint.id and not original),
  (select count(*) from nats_outbox where payload->>'templateKey'='blueprint_conversion_failure' and (payload->>'recipientId')::bigint=$2)
  from blueprints blueprint join blueprint_jobs job on job.blueprint_id=blueprint.id where job.id=$1`, jobID, f.userIDs[f.otherEditor]).Scan(&sourceStatus, &jobStatus, &lastError, &attempts, &generated, &notices); err != nil {
		t.Fatal(err)
	}
	if (sourceStatus != "ready" && sourceStatus != "partial") || jobStatus != "failed" || attempts != 1 || generated != 0 || notices != 1 || !strings.Contains(strings.ToLower(lastError), "entity") {
		t.Fatalf("terminal fidelity facts source/job=%s/%s attempts=%d variants=%d notices=%d reason=%s", sourceStatus, jobStatus, attempts, generated, notices, lastError)
	}
	f.require(t, "", http.MethodGet, "/api/v1/blueprints/"+upload.publicID, nil, 200)
	if !bytes.Equal(normalized, f.require(t, "", http.MethodGet, "/api/v1/blueprints/"+upload.publicID+"/render", nil, 200)) {
		t.Fatal("optional conversion failure changed published normalized source")
	}
}

func TestTEST036MaliciousPaletteAndGeometryCannotPublishFullHTTPIntegration(t *testing.T) {
	cases := []struct {
		name               string
		size               []int32
		paletteName, state any
		position           []int32
	}{
		{"negative-state", []int32{1, 1, 1}, "minecraft:stone", int32(-1), []int32{0, 0, 0}},
		{"wrong-state-type", []int32{1, 1, 1}, "minecraft:stone", "not an integer", []int32{0, 0, 0}},
		{"missing-state-name", []int32{1, 1, 1}, "", int32(0), []int32{0, 0, 0}},
		{"wrong-name-type", []int32{1, 1, 1}, int32(1), int32(0), []int32{0, 0, 0}},
		{"outside-position", []int32{1, 1, 1}, "minecraft:stone", int32(0), []int32{1, 0, 0}},
		{"dimension-budget", []int32{4097, 1, 1}, "minecraft:stone", int32(0), []int32{0, 0, 0}},
		{"volume-budget", []int32{128, 128, 129}, "minecraft:stone", int32(0), []int32{0, 0, 0}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			f := newTEST036Fixture(t)
			raw, _, err := encodeGzipNBT(map[string]any{"size": testCase.size, "palette": []map[string]any{{"Name": testCase.paletteName}}, "blocks": []map[string]any{{"pos": testCase.position, "state": testCase.state}}}, "TEST036 malicious source")
			if err != nil {
				t.Fatal(err)
			}
			upload := f.upload(t, f.editor, "test036-"+testCase.name+".nbt", raw)
			if err = f.worker("malicious-"+testCase.name).processJob(f.ctx, upload.jobID); err == nil {
				t.Fatal("malformed palette/geometry reached a successful normalized state")
			}
			var status string
			var generated, materials int
			if err = f.db.QueryRow(f.ctx, `select status,
   (select count(*) from blueprint_job_artifacts where blueprint_id=blueprints.id),
   (select count(*) from blueprint_materials where blueprint_id=blueprints.id)
   from blueprints where public_id=$1`, upload.publicID).Scan(&status, &generated, &materials); err != nil {
				t.Fatal(err)
			}
			if status != "queued" || generated != 0 || materials != 0 {
				t.Fatalf("malicious source left partial success: status=%s artifacts=%d materials=%d", status, generated, materials)
			}
			f.require(t, "", http.MethodGet, "/api/v1/blueprints/"+upload.publicID, nil, 404)
		})
	}
}

func TestTEST036TerminalFailureNotificationIsAtomicAndRecoverableIntegration(t *testing.T) {
	f := newTEST036Fixture(t)
	raw, _, err := encodeGzipNBT(map[string]any{
		"size": []int32{1, 1, 1}, "palette": []map[string]any{{"Name": "minecraft:chest"}},
		"blocks": []map[string]any{{"pos": []int32{0, 0, 0}, "state": int32(0), "nbt": map[string]any{"id": "minecraft:chest"}}},
	}, "TEST036 failure notice source")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "test036-failure-notice.nbt", raw)
	if err = f.worker("failure-notice-source").processJob(f.ctx, upload.jobID); err != nil {
		t.Fatal(err)
	}
	f.approve(t, upload.publicID)
	response := f.require(t, f.otherEditor, http.MethodPost, "/api/v1/blueprints/"+upload.publicID+"/convert", map[string]string{"format": "schem"}, 202)
	var jobID int64
	if err = f.db.QueryRow(f.ctx, "select id from blueprint_jobs where public_id=$1", decodeTEST022Data[map[string]string](t, response)["jobId"]).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "alter table nats_outbox add constraint test036_reject_terminal_notice check(coalesce(payload->>'templateKey','')<>'blueprint_conversion_failure') not valid"); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	if err = f.worker("failed-notice-store").processJob(f.ctx, jobID); err == nil {
		t.Fatal("terminal work failure returned success")
	}
	var status string
	var attempts, notices int
	if err = f.db.QueryRow(f.ctx, `select job.status,job.attempts,
  (select count(*) from nats_outbox where payload->>'templateKey'='blueprint_conversion_failure')
  from blueprint_jobs job where job.id=$1`, jobID).Scan(&status, &attempts, &notices); err != nil {
		t.Fatal(err)
	}
	if status != "processing" || attempts != 1 || notices != 0 {
		t.Fatalf("failure committed without recoverable notice: job=%s attempts=%d notices=%d", status, attempts, notices)
	}
	after := f.facts(t)
	for _, table := range []string{"blueprints", "blueprint_variants", "blueprint_job_artifacts", "oss_files"} {
		if before[table] != after[table] {
			t.Errorf("failed optional conversion/notice store mutated %s", table)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	if _, err = f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test036_reject_terminal_notice"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update blueprint_jobs set lease_expires_at=now()-interval '1 second' where id=$1", jobID); err != nil {
		t.Fatal(err)
	}
	restart := f.worker("terminal-notice-restart")
	recovered, exhausted, err := restart.recoverStaleBlueprintJobs(f.ctx)
	if err != nil || recovered != 1 || exhausted != 0 {
		t.Fatalf("failed notice recovery=%d/%d err=%v", recovered, exhausted, err)
	}
	if err = restart.processJob(f.ctx, jobID); !errors.Is(err, errBlueprintEntityDataWouldBeLost) {
		t.Fatalf("restarted fidelity refusal=%v", err)
	}
	if err = f.db.QueryRow(f.ctx, `select job.status,job.attempts,
  (select count(*) from nats_outbox where payload->>'templateKey'='blueprint_conversion_failure')
  from blueprint_jobs job where job.id=$1`, jobID).Scan(&status, &attempts, &notices); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempts != 2 || notices != 1 {
		t.Fatalf("restarted terminal job/notice=%s/%d/%d", status, attempts, notices)
	}
	if f.facts(t)["blueprints"] != before["blueprints"] {
		t.Fatal("terminal notice recovery changed the published source")
	}
	f.require(t, "", http.MethodGet, "/api/v1/blueprints/"+upload.publicID, nil, 200)
}
