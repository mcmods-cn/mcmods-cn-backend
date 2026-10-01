package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

type test016FileFixture struct {
	test013Fixture
	server   *Server
	provider *httptest.Server
	requests *atomic.Int64
}

func newTEST016FileFixture(t *testing.T) test016FileFixture {
	t.Helper()
	f := test016FileFixture{test013Fixture: newTEST013Fixture(t), requests: new(atomic.Int64)}
	f.server = f.origin.Config.Handler.(*Server)
	type object struct {
		data              []byte
		hash, contentType string
	}
	objects := map[string]object{}
	var mu sync.Mutex
	f.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			if _, exists := objects[r.URL.Path]; exists {
				w.WriteHeader(http.StatusConflict)
				return
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil || int64(len(data)) != r.ContentLength {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			objects[r.URL.Path] = object{data, r.Header.Get("x-oss-meta-sha256"), r.Header.Get("Content-Type")}
			w.Header().Set("ETag", `"test016-owned"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		entry, exists := objects[r.URL.Path]
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodHead && r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", entry.contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(entry.data)))
		w.Header().Set("x-oss-meta-sha256", entry.hash)
		w.Header().Set("ETag", `"test016-owned"`)
		w.Header().Set("Last-Modified", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			_, _ = w.Write(entry.data)
		}
	}))
	t.Cleanup(f.provider.Close)
	cfg := defaultOSSConfig()
	cfg.Enabled, cfg.UseCName = true, true
	cfg.Bucket, cfg.Region = "test016-owned", "cn-test"
	cfg.Endpoint, cfg.PublicEndpoint = f.provider.URL, f.provider.URL
	cfg.AccessKeyID, cfg.AccessKeySecret = "test016-synthetic-key", "test016-synthetic-secret"
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.download.upload."+f.modCode)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.download.upload."+f.otherModCode)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "oss.write")
	return f
}

func test016Archive(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	entry, err := w.Create("test016.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(entry, "TEST016 owned immutable artifact"); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func test016UploadBody(name string, data []byte) ossDirectUploadRequest {
	digest := sha256.Sum256(data)
	return ossDirectUploadRequest{OriginalName: name, ContentType: "application/zip", SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
}

type test016Upload struct {
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	ObjectKey      string            `json:"objectKey"`
	UploadRequired bool              `json:"uploadRequired"`
}

func (f test016FileFixture) put(t *testing.T, token, base, name string, data []byte) ossCompleteUploadRequest {
	t.Helper()
	body := test016UploadBody(name, data)
	raw := f.require(t, token, http.MethodPost, base+"/uploads/presign", body, http.StatusOK)
	var response struct {
		Data test016Upload `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	upload := response.Data
	if !upload.UploadRequired || upload.Method != http.MethodPut || upload.ObjectKey == "" || !strings.HasPrefix(upload.URL, f.provider.URL+"/") {
		t.Fatalf("not an owned real PUT: %s", raw)
	}
	request, err := http.NewRequestWithContext(f.ctx, upload.Method, upload.URL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range upload.Headers {
		request.Header.Set(key, value)
	}
	res, err := f.provider.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("owned PUT status=%d", res.StatusCode)
	}
	return ossCompleteUploadRequest{ObjectKey: upload.ObjectKey, OriginalName: name, ContentType: body.ContentType, SizeBytes: body.SizeBytes, SHA256: body.SHA256}
}

func (f test016FileFixture) facts(t *testing.T) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"project_files", "oss_files", "oss_upload_logs", "oss_scan_logs", "oss_download_stats", "oss_object_deletion_outbox", "public_routes", "nats_outbox", "project_update_events", "project_update_notification_tasks", "notifications", "audit_events"} {
		var data string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(row_value) order by to_jsonb(row_value)::text),'[]'::jsonb)::text from `+table+` row_value`).Scan(&data); err != nil {
			t.Fatal(err)
		}
		result[table] = data
	}
	return result
}

func (f test016FileFixture) register(t *testing.T, token, base, name string) (string, ossCompleteUploadRequest) {
	t.Helper()
	upload := f.put(t, token, base, name, test016Archive(t))
	raw := f.require(t, token, http.MethodPost, base+"/uploads/complete", upload, http.StatusCreated)
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Data.ID == "" {
		t.Fatalf("registration lacks public ID: %v %s", err, raw)
	}
	return response.Data.ID, upload
}

func test016Metadata(ossID string) createProjectFileRequest {
	return createProjectFileRequest{OSSFileID: ossID, DisplayName: "TEST016 release", VersionName: "1.0", ReleaseChannel: "release", GameVersions: []string{"1.21.1"}, Loaders: []string{"neoforge"}}
}

func (f test016FileFixture) attach(t *testing.T, token, base, ossID string) string {
	t.Helper()
	raw := f.require(t, token, http.MethodPost, base, test016Metadata(ossID), http.StatusCreated)
	var response struct {
		Data projectFileItem `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Data.ID == "" {
		t.Fatalf("file lacks ID: %v %s", err, raw)
	}
	return response.Data.ID
}

func (f test016FileFixture) scan(t *testing.T, ossID, status string, want int) {
	t.Helper()
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/oss/files/"+ossID+"/scan", map[string]string{"status": status, "note": "TEST016 owned manual scan transition"}, want)
}

func (f test016FileFixture) assertFile(t *testing.T, id, status string, generation, added, removed int) {
	t.Helper()
	var gotStatus string
	var gotGeneration, gotAdded, gotRemoved, routes, tasks int
	if err := f.db.QueryRow(f.ctx, `select status,publication_generation,
		(select count(*) from project_update_events where publication_batch_id like 'project-file:download_added:'||$1||':%'),
		(select count(*) from project_update_events where publication_batch_id like 'project-file:download_removed:'||$1||':%'),
		(select count(*) from public_routes where public_id=$1),
		(select count(*) from project_update_notification_tasks task join project_update_events event on event.id=task.event_id where publication_batch_id like 'project-file:%:'||$1||':%')
		from project_files where public_id=$1`, id).Scan(&gotStatus, &gotGeneration, &gotAdded, &gotRemoved, &routes, &tasks); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotGeneration != generation || gotAdded != added || gotRemoved != removed || routes != 0 || tasks != added+removed {
		t.Fatalf("file durable publication mismatch: %s gen%d add%d remove%d routes%d tasks%d", gotStatus, gotGeneration, gotAdded, gotRemoved, routes, tasks)
	}
}

func (f test016FileFixture) assertList(t *testing.T, token, base string, ids ...string) {
	t.Helper()
	raw := f.require(t, token, http.MethodGet, base+"?source=internal&limit=1", nil, http.StatusOK)
	var response struct {
		Data struct {
			Items      []projectFileItem `json:"items"`
			HasMore    bool              `json:"hasMore"`
			NextCursor string            `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Items) != len(ids) {
		t.Fatalf("public file list=%s want=%v", raw, ids)
	}
	for i, id := range ids {
		if response.Data.Items[i].ID != id {
			t.Fatalf("public file wrong ID: %s", raw)
		}
	}
	if response.Data.HasMore || response.Data.NextCursor != "" || len(raw) > 16<<10 {
		t.Fatalf("file list unbounded/cursor wrong: %s", raw)
	}
}

func TestTEST016ProjectFileScanPublicationDeletionAndPermissionHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	otherBase := "/api/v1/projects/mod/" + f.otherModCode + "/files"
	ossID, _ := f.register(t, f.editor, base, "release.jar")
	otherOSS, _ := f.register(t, f.otherEditor, otherBase, "foreign.jar")
	for _, reject := range []struct {
		token, base string
		body        createProjectFileRequest
		status      int
	}{
		{"", base, test016Metadata(ossID), http.StatusUnauthorized},
		{f.denied, base, test016Metadata(ossID), http.StatusForbidden},
		{f.otherEditor, base, test016Metadata(ossID), http.StatusForbidden},
		{f.editor, base, test016Metadata(otherOSS), http.StatusBadRequest},
	} {
		before := f.facts(t)
		f.require(t, reject.token, http.MethodPost, reject.base, reject.body, reject.status)
		if !reflect.DeepEqual(before, f.facts(t)) {
			t.Fatal("denied metadata mutated durable facts")
		}
	}
	unknown := test016Metadata(ossID)
	unknown.GameVersions = []string{"0.TEST016-invalid"}
	before := f.facts(t)
	f.require(t, f.editor, http.MethodPost, base, unknown, http.StatusBadRequest)
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("unknown version mutated facts")
	}
	id := f.attach(t, f.editor, base, ossID)
	f.assertFile(t, id, "processing", 0, 0, 0)
	f.assertList(t, "", base)
	download := base + "/internal/" + id + "/download"
	f.require(t, "", http.MethodPost, download, nil, http.StatusNotFound)
	f.require(t, f.denied, http.MethodPatch, "/api/v1/admin/oss/files/"+ossID+"/scan", map[string]string{"status": "clean"}, http.StatusForbidden)
	f.scan(t, ossID, "rejected", http.StatusOK)
	f.assertFile(t, id, "rejected", 0, 0, 0)
	f.assertList(t, "", base)
	f.scan(t, ossID, "clean", http.StatusOK)
	f.assertFile(t, id, "active", 1, 1, 0)
	f.assertList(t, "", base, id)
	f.scan(t, ossID, "clean", http.StatusOK)
	f.assertFile(t, id, "active", 1, 1, 0)
	f.scan(t, ossID, "pending", http.StatusOK)
	f.assertFile(t, id, "processing", 1, 1, 1)
	f.assertList(t, "", base)
	f.require(t, "", http.MethodPost, download, nil, http.StatusNotFound)
	f.scan(t, ossID, "clean", http.StatusOK)
	// Same actor within the existing thirty-second merge window coalesces into
	// the first durable update/task, rather than fabricating another delivery.
	f.assertFile(t, id, "active", 2, 1, 1)
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, base, test016Metadata(ossID), http.StatusConflict)
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("duplicate attachment mutated facts")
	}
	before = f.facts(t)
	f.require(t, f.otherEditor, http.MethodDelete, otherBase+"/"+id, nil, http.StatusNotFound)
	f.require(t, f.otherEditor, http.MethodDelete, base+"/"+id, nil, http.StatusForbidden)
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("cross-project deletion mutated facts")
	}
	f.require(t, f.editor, http.MethodDelete, base+"/"+id, nil, http.StatusOK)
	f.assertFile(t, id, "deleted", 2, 1, 1)
	f.assertList(t, "", base)
	before = f.facts(t)
	f.require(t, f.editor, http.MethodDelete, base+"/"+id, nil, http.StatusNotFound)
	f.require(t, "", http.MethodPost, download, nil, http.StatusNotFound)
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("repeat deletion/download changed facts")
	}
	f.scan(t, ossID, "clean", http.StatusOK)
	f.assertFile(t, id, "deleted", 2, 1, 1)
	var activeOSS int
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_files where public_id=$1 and status='active'`, ossID).Scan(&activeOSS); err != nil || activeOSS != 1 {
		t.Fatalf("soft-deleting project file unexpectedly deleted immutable OSS artifact: %d %v", activeOSS, err)
	}
}

func TestTEST016ProjectDownloadCountersFailAtomicallyBeforeSuccessHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	ossID, upload := f.register(t, f.editor, base, "download.jar")
	id := f.attach(t, f.editor, base, ossID)
	f.scan(t, ossID, "clean", http.StatusOK)
	download := base + "/internal/" + id + "/download"
	if _, err := f.db.Exec(f.ctx, `alter table project_files add constraint test016_counter_reject check(download_count=0)`); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	f.require(t, "", http.MethodPost, download, nil, http.StatusInternalServerError)
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("failed download count persisted partial OSS/project/audit facts")
	}
	if _, err := f.db.Exec(f.ctx, `alter table project_files drop constraint test016_counter_reject`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table oss_download_stats add constraint test016_statistics_reject check(downloads=0)`); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, "", http.MethodPost, download, nil, http.StatusInternalServerError)
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("late OSS statistics failure left the earlier project counter increment")
	}
	if _, err := f.db.Exec(f.ctx, `alter table oss_download_stats drop constraint test016_statistics_reject`); err != nil {
		t.Fatal(err)
	}
	raw := f.require(t, "", http.MethodPost, download, nil, http.StatusOK)
	var response struct {
		Data struct {
			URL       string    `json:"url"`
			ExpiresAt time.Time `json:"expiresAt"`
			Filename  string    `json:"filename"`
			Mode      string    `json:"downloadUrlMode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(response.Data.URL)
	if err != nil || parsed.Query().Get("x-oss-signature") == "" || response.Data.Mode != ossDownloadModePresigned || response.Data.Filename != "download.jar" || time.Until(response.Data.ExpiresAt) <= 0 || time.Until(response.Data.ExpiresAt) > time.Hour {
		t.Fatalf("download signature/expiry/filename mismatch: %s %v", raw, err)
	}
	res, err := f.provider.Client().Get(response.Data.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	actual, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || res.StatusCode != 200 || !bytes.Equal(actual, test016Archive(t)) {
		t.Fatal("signed owned GET did not return the actual immutable artifact")
	}
	var count, stats int
	if err = f.db.QueryRow(f.ctx, `select download_count,(select downloads from oss_download_stats where object_key=$2) from project_files where public_id=$1`, id, upload.ObjectKey).Scan(&count, &stats); err != nil || count != 1 || stats != 1 {
		t.Fatalf("download counts diverged: %d/%d %v", count, stats, err)
	}
	type outcome struct {
		status int
		err    error
	}
	results := make(chan outcome, 16)
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+download, nil)
			if err != nil {
				results <- outcome{err: err}
				return
			}
			response, err := f.origin.Client().Do(request)
			if err != nil {
				results <- outcome{err: err}
				return
			}
			_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
			_ = response.Body.Close()
			results <- outcome{response.StatusCode, readErr}
		}()
	}
	group.Wait()
	close(results)
	for result := range results {
		if result.err != nil || result.status != 200 {
			t.Fatalf("concurrent download failed: %+v", result)
		}
	}
	if err = f.db.QueryRow(f.ctx, `select download_count,(select downloads from oss_download_stats where object_key=$2) from project_files where public_id=$1`, id, upload.ObjectKey).Scan(&count, &stats); err != nil || count != 17 || stats != 17 {
		t.Fatalf("concurrent counters lost/duplicated increments: %d/%d %v", count, stats, err)
	}
}

func TestTEST016PendingProjectFilesRemainPrivateAndEmitNoPublicUpdatesHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.otherModID); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/projects/mod/" + f.otherModCode + "/files"
	ossID, _ := f.register(t, f.otherEditor, base, "pending-project.jar")
	id := f.attach(t, f.otherEditor, base, ossID)
	f.assertFile(t, id, "processing", 0, 0, 0)
	f.require(t, "", http.MethodGet, base+"?source=internal", nil, http.StatusNotFound)
	f.require(t, f.editor, http.MethodGet, base+"?source=internal", nil, http.StatusNotFound)
	f.scan(t, ossID, "clean", http.StatusOK)
	f.assertFile(t, id, "active", 1, 0, 0)
	f.assertList(t, f.otherEditor, base, id)
	before := f.facts(t)
	for _, token := range []string{"", f.otherEditor, f.editor} {
		f.require(t, token, http.MethodPost, base+"/internal/"+id+"/download", nil, http.StatusNotFound)
	}
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("unpublished project download/private request mutated public facts")
	}
	f.require(t, f.otherEditor, http.MethodDelete, base+"/"+id, nil, http.StatusOK)
	f.assertFile(t, id, "deleted", 1, 0, 0)
}

func TestTEST016OSSDefaultConfigurationOwnsItsExtensionSlice(t *testing.T) {
	original := append([]string(nil), defaultOSSAllowedExtensions...)
	defer copy(defaultOSSAllowedExtensions, original)
	first, second := defaultOSSConfig(), defaultOSSConfig()
	first.AllowedExtensions[0] = ".test016-mutated"
	if !reflect.DeepEqual(second.AllowedExtensions, original) || !reflect.DeepEqual(defaultOSSAllowedExtensions, original) {
		t.Fatal("per-request configuration shares the global extension backing array")
	}
}

func TestTEST016SharedLogoDeletionHandlesSlowTransientProvider(t *testing.T) {
	testOPS008SharedLogoLifecycle(t, 750*time.Millisecond)
}

func TestTEST016ProjectPublicationStatusIsRevalidatedInsideFileCreationHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	ossID, _ := f.register(t, f.editor, base, "publication-race.jar")
	f.scan(t, ossID, "clean", http.StatusOK)
	blocker, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(f.ctx, `select id from mods where id=$1 for update`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.Exec(f.ctx, `select id from oss_files where public_id=$1 for update`, ossID); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(test016Metadata(ossID))
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+base, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+f.editor)
	request.Header.Set("Content-Type", "application/json")
	type outcome struct {
		status int
		raw    []byte
		err    error
	}
	result := make(chan outcome, 1)
	go func() {
		response, err := f.origin.Client().Do(request)
		if err != nil {
			result <- outcome{err: err}
			return
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		result <- outcome{response.StatusCode, data, err}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting int
		if err = f.db.QueryRow(f.ctx, `select count(*) from pg_stat_activity where datname=current_database() and wait_event_type='Lock' and (query like '%from oss_files%' or query like '%from mods%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual HTTP mutation did not reach its owned row lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = blocker.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	response := <-result
	if response.err != nil || response.status != http.StatusCreated {
		t.Fatalf("pending-project authorized upload unexpectedly failed: %+v", response)
	}
	var decoded struct {
		Data projectFileItem `json:"data"`
	}
	if err = json.Unmarshal(response.raw, &decoded); err != nil {
		t.Fatal(err)
	}
	f.assertFile(t, decoded.Data.ID, "active", 1, 0, 0)
	f.require(t, "", http.MethodGet, base+"?source=internal", nil, http.StatusNotFound)
}

func TestTEST016ProjectDownloadDoesNotBorrowASecondPoolConnectionHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	ossID, _ := f.register(t, f.editor, base, "bounded-pool.jar")
	id := f.attach(t, f.editor, base, ossID)
	f.scan(t, ossID, "clean", http.StatusOK)
	var held []pgx.Tx
	defer func() {
		for _, tx := range held {
			if err := tx.Rollback(context.Background()); err != nil {
				t.Error(err)
			}
		}
	}()
	// Hold unrelated read-only transactions, leaving precisely one connection
	// for the actual download. Its own transaction must not borrow another.
	for i := int32(0); i < f.db.Stat().MaxConns()-1; i++ {
		tx, err := f.db.BeginTx(f.ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, tx)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.origin.URL+base+"/internal/"+id+"/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.origin.Client().Do(request)
	if err != nil {
		t.Fatalf("download borrowed a second connection while its transaction held the only free slot: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		t.Fatalf("single-slot download status=%d body=%s", res.StatusCode, raw)
	}
}

func TestTEST016ProjectFileLatePublicationAndDeletionFailuresRollBackHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	ossID, _ := f.register(t, f.editor, base, "atomic-publication.jar")
	id := f.attach(t, f.editor, base, ossID)
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox add constraint test016_publication_reject check(event_type<>'project.updated')`); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	f.scan(t, ossID, "clean", http.StatusInternalServerError)
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("failed publication intent left scan/file/event/task/outbox/audit facts")
	}
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox drop constraint test016_publication_reject`); err != nil {
		t.Fatal(err)
	}
	f.scan(t, ossID, "clean", http.StatusOK)
	f.assertFile(t, id, "active", 1, 1, 0)
	if _, err := f.db.Exec(f.ctx, `alter table audit_events add constraint test016_delete_audit_reject check(aggregate_type<>'project_file' or action<>'delete')`); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, f.editor, http.MethodDelete, base+"/"+id, nil, http.StatusInternalServerError)
	if !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatal("failed late deletion audit left soft-delete/publication facts")
	}
	if _, err := f.db.Exec(f.ctx, `alter table audit_events drop constraint test016_delete_audit_reject`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodDelete, base+"/"+id, nil, http.StatusOK)
	f.assertFile(t, id, "deleted", 1, 1, 0)
}

func TestTEST016ProjectUploadsAcceptOnlyTheirActualArtifactFormatsHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	data := test016Archive(t)
	codes := map[string]string{"mod": f.modCode}
	for _, item := range []struct{ kind, extension string }{{"mod", ".jar"}, {"plugin", ".jar"}, {"modpack", ".mrpack"}, {"modpack", ".zip"}, {"map", ".zip"}, {"resource_pack", ".zip"}, {"shader_pack", ".zip"}, {"datapack", ".zip"}, {"addon", ".zip"}, {"addon", ".jar"}} {
		t.Run(item.kind, func(t *testing.T) {
			code, exists := codes[item.kind]
			if !exists && item.kind == "modpack" {
				if err := f.db.QueryRow(f.ctx, `insert into modpacks(slug,primary_name,review_status,submitted_by) values('test016-pack','TEST016 Pack','approved',$1) returning public_id`, f.userIDs[f.editor]).Scan(&code); err != nil {
					t.Fatal(err)
				}
			} else if !exists && item.kind != "mod" {
				if err := f.db.QueryRow(f.ctx, `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by) values($1,$2,$2,'approved',$3) returning public_id`, item.kind, "test016-"+item.kind, f.userIDs[f.editor]).Scan(&code); err != nil {
					t.Fatal(err)
				}
			}
			codes[item.kind] = code
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.download.upload."+code)
			base := "/api/v1/projects/" + item.kind + "/" + code + "/files"
			f.require(t, "", http.MethodPost, base+"/uploads/presign", test016UploadBody("x"+item.extension, data), http.StatusUnauthorized)
			f.require(t, f.denied, http.MethodPost, base+"/uploads/presign", test016UploadBody("x"+item.extension, data), http.StatusForbidden)
			f.require(t, f.editor, http.MethodPost, base+"/uploads/presign", test016UploadBody("x.exe", data), http.StatusBadRequest)
			upload := f.put(t, f.editor, base, "test016"+item.extension, data)
			raw := f.require(t, f.editor, http.MethodPost, base+"/uploads/complete", upload, http.StatusCreated)
			var response struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				t.Fatal(err)
			}
			var uploader int64
			var category, scan string
			if err := f.db.QueryRow(f.ctx, `select uploader_id,category,scan_status from oss_files where public_id=$1`, response.Data.ID).Scan(&uploader, &category, &scan); err != nil {
				t.Fatal(err)
			}
			if uploader != f.userIDs[f.editor] || category != ossProjectReleaseCategory(item.kind, code) || scan != "pending" {
				t.Fatalf("artifact registration has wrong scope/actor/scan: %d %s %s", uploader, category, scan)
			}
			metadata := test016Metadata(response.Data.ID)
			if item.kind == "plugin" {
				metadata.Loaders = []string{"paper"}
			} else if !projectFileRequiresLoader(item.kind) {
				metadata.Loaders = []string{}
			}
			if item.kind == "plugin" {
				invalid := metadata
				invalid.Loaders = []string{"neoforge"}
				before := f.facts(t)
				f.require(t, f.editor, http.MethodPost, base, invalid, http.StatusBadRequest)
				if !reflect.DeepEqual(before, f.facts(t)) {
					t.Fatal("wrong project-type loader changed metadata facts")
				}
			}
			raw = f.require(t, f.editor, http.MethodPost, base, metadata, http.StatusCreated)
			var attached struct {
				Data projectFileItem `json:"data"`
			}
			if err := json.Unmarshal(raw, &attached); err != nil {
				t.Fatal(err)
			}
			f.assertFile(t, attached.Data.ID, "processing", 0, 0, 0)
		})
	}
}

func TestTEST016ScanRevalidatesParentPublicationUnderConcurrentStatusChangeHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	ossID, _ := f.register(t, f.editor, base, "scan-publication-race.jar")
	id := f.attach(t, f.editor, base, ossID)
	blocker, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(f.ctx, `select id from mods where id=$1 for update`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.Exec(f.ctx, `select id from project_files where public_id=$1 for update`, id); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodPatch, f.origin.URL+"/api/v1/admin/oss/files/"+ossID+"/scan", strings.NewReader(`{"status":"clean"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+f.reviewer)
	request.Header.Set("Content-Type", "application/json")
	type outcome struct {
		status int
		raw    []byte
		err    error
	}
	result := make(chan outcome, 1)
	go func() {
		response, err := f.origin.Client().Do(request)
		if err != nil {
			result <- outcome{err: err}
			return
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		result <- outcome{response.StatusCode, data, err}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting int
		if err = f.db.QueryRow(f.ctx, `select count(*) from pg_stat_activity where datname=current_database() and wait_event_type='Lock' and query like '%from project_files%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual scan did not reach its owned row lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = blocker.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	response := <-result
	if response.err != nil || response.status != 200 {
		t.Fatalf("scan mutation failed: %+v", response)
	}
	f.assertFile(t, id, "active", 1, 0, 0)
}

func TestTEST016PublicInternalFileCursorTraversesWithoutCrossScopeOrDuplicateHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	want := map[string]bool{}
	for i := 0; i < 3; i++ {
		ossID, _ := f.register(t, f.editor, base, strconv.Itoa(i)+".jar")
		id := f.attach(t, f.editor, base, ossID)
		f.scan(t, ossID, "clean", http.StatusOK)
		want[id] = true
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 3; page++ {
		path := base + "?source=internal&limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		raw := f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
		var response struct {
			Data struct {
				Items      []projectFileItem `json:"items"`
				HasMore    bool              `json:"hasMore"`
				NextCursor string            `json:"nextCursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Data.Items) != 1 || !want[response.Data.Items[0].ID] || seen[response.Data.Items[0].ID] || len(raw) > 16<<10 {
			t.Fatalf("page has missing/duplicate/foreign/unbounded item: %s", raw)
		}
		seen[response.Data.Items[0].ID] = true
		if response.Data.HasMore != (page < 2) || (response.Data.NextCursor != "") != (page < 2) {
			t.Fatalf("cursor boundary incorrect: %s", raw)
		}
		cursor = response.Data.NextCursor
		if cursor != "" {
			f.require(t, "", http.MethodGet, "/api/v1/projects/mod/"+f.otherModCode+"/files?source=internal&limit=1&cursor="+url.QueryEscape(cursor), nil, http.StatusBadRequest)
			f.require(t, "", http.MethodGet, base+"?source=internal&limit=2&cursor="+url.QueryEscape(cursor), nil, http.StatusBadRequest)
		}
	}
	if len(seen) != 3 {
		t.Fatal("full HTTP keyset traversal lost an active file")
	}
}

func TestTEST016PendingProjectObjectCannotBeClaimedByAnotherAuthorizedUploaderHTTPIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.download.upload."+f.modCode)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	upload := f.put(t, f.editor, base, "owned.jar", test016Archive(t))
	before := f.facts(t)
	requests := f.requests.Load()
	f.require(t, f.otherEditor, http.MethodPost, base+"/uploads/complete", upload, http.StatusBadRequest)
	if !reflect.DeepEqual(before, f.facts(t)) || f.requests.Load() != requests {
		t.Fatal("foreign completion changed durable facts or touched the provider")
	}
	f.require(t, f.editor, http.MethodPost, base+"/uploads/complete", upload, http.StatusCreated)
}

// A malformed provider must not leave a CPU loop and an uncloseable HTTP server
// in the parent test process. The same production pager runs in a bounded child.
func TestTEST016ModrinthEmptyOrUnusableVersionDoesNotSpin(t *testing.T) {
	if endpoint := os.Getenv("MCMODS_TEST016_OWNED_EMPTY_PROVIDER"); endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
			t.Fatal("child requires the parent's owned literal loopback provider")
		}
		api := &Server{cache: querycache.New(config.RedisConfig{})}
		defer api.cache.Close()
		cfg := defaultModImportConfig()
		cfg.Modrinth.BaseURL, cfg.Modrinth.Token = endpoint, ""
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		page, err := api.loadModrinthProjectFilePage(ctx, projectFileContext{ProjectType: "mod", ProjectID: "mod000001", ModrinthProjectID: "owned"}, projectFilePageRequest{Source: "modrinth", Limit: 1, Scope: "owned"}, cfg)
		if err != nil || len(page.Items) != 0 || page.HasMore || page.NextCursor != "" {
			t.Fatalf("empty version did not terminate cleanly: %+v %v", page, err)
		}
		return
	}
	for _, mode := range []string{"empty", "invalid-url", "missing-hash"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/project/owned" {
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned", "versions": []string{"v1"}})
					return
				}
				files := []map[string]any{}
				if mode != "empty" {
					file := map[string]any{"filename": "v1.jar", "url": "https://cdn.example.invalid/v1.jar", "hashes": map[string]string{"sha1": "owned-hash"}}
					if mode == "invalid-url" {
						file["url"] = "http://127.0.0.1/private"
					}
					if mode == "missing-hash" {
						file["hashes"] = map[string]string{}
					}
					files = append(files, file)
				}
				_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "v1", "project_id": "owned", "files": files}})
			}))
			defer provider.Close()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestTEST016ModrinthEmptyOrUnusableVersionDoesNotSpin$", "-test.v")
			command.Env = append(os.Environ(), "MCMODS_TEST016_OWNED_EMPTY_PROVIDER="+provider.URL)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("owned provider %s pager did not finish within the fixed child budget: providerCalls=%d deadline=%v err=%v\n%s", mode, calls.Load(), ctx.Err(), err, output)
			}
			t.Logf("owned %s pager child completed: %s", mode, output)
		})
	}
}
