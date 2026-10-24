package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func auditFixProjectFileFacts(t *testing.T, f test016FileFixture) map[string]string {
	t.Helper()
	facts := f.test013Fixture.facts(t)
	for table, value := range f.facts(t) {
		facts[table] = value
	}
	return facts
}
func TestAuditFixCMP003ProjectFileActivitySharesVersionAndDownloadTransaction(t *testing.T) {
	f := newTEST016FileFixture(t)
	require := func(token, method, path string, body any, want int) []byte {
		t.Helper()
		return (auditFixGRFixture{test013Fixture: f.test013Fixture}).require(t, token, method, path, body, want)
	}
	recover := func(method, path string, body any, want int, action int16, message string) []byte {
		t.Helper()
		prior := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, action, activity.ObjectFile)
		auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
		before := auditFixProjectFileFacts(t, f)
		response := require(f.editor, method, path, body, 500)
		auditFixRequireSpecificActivityError(t, response, message)
		auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectFileFacts(t, f))
		auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
		result := require(f.editor, method, path, body, want)
		if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, action, activity.ObjectFile); got != prior+1 {
			t.Fatalf("%s activity=%d prior=%d", path, got, prior)
		}
		return result
	}
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	data := test016Archive(t)
	body := test016UploadBody("atomic.jar", data)
	ticket := decodeTEST022Data[test016Upload](t, recover(http.MethodPost, base+"/uploads/presign", body, 200, activity.ActionUpload, "failed to persist OSS upload ticket activity"))
	put, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range ticket.Headers {
		put.Header.Set(k, v)
	}
	putResponse, err := f.provider.Client().Do(put)
	if err != nil {
		t.Fatal(err)
	}
	_ = putResponse.Body.Close()
	if putResponse.StatusCode != 200 {
		t.Fatalf("owned PUT status=%d", putResponse.StatusCode)
	}
	completion := ossCompleteUploadRequest{ObjectKey: ticket.ObjectKey, OriginalName: body.OriginalName, ContentType: body.ContentType, SizeBytes: body.SizeBytes, SHA256: body.SHA256}
	registered := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, recover(http.MethodPost, base+"/uploads/complete", completion, 201, activity.ActionUpload, "failed to persist upload activity"))
	f.scan(t, registered.ID, "clean", 200)
	if _, err = f.db.Exec(f.ctx, `alter table audit_events add constraint audit_reject_project_file_create check(not(aggregate_type='project_file' and action='create')) not valid`); err != nil {
		t.Fatal(err)
	}
	before := auditFixProjectFileFacts(t, f)
	response := require(f.editor, http.MethodPost, base, test016Metadata(registered.ID), 500)
	auditFixRequireSpecificActivityError(t, response, "failed to persist project file creation audit")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectFileFacts(t, f))
	if _, err = f.db.Exec(f.ctx, `alter table audit_events drop constraint audit_reject_project_file_create`); err != nil {
		t.Fatal(err)
	}
	file := decodeTEST022Data[projectFileItem](t, recover(http.MethodPost, base, test016Metadata(registered.ID), 201, activity.ActionCreate, "failed to persist project file activity"))
	f.assertFile(t, file.ID, "active", 1, 1, 0)
	download := base + "/internal/" + file.ID + "/download"
	access := decodeTEST022Data[struct {
		URL string `json:"url"`
	}](t, recover(http.MethodPost, download, nil, 200, activity.ActionDownload, "failed to persist project download activity"))
	// Real bytes from the owned OSS provider must remain the uploaded archive.
	get, err := http.NewRequestWithContext(f.ctx, http.MethodGet, access.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.provider.Client().Do(get)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || res.StatusCode != 200 || !bytes.Equal(data, actual) {
		t.Fatalf("download source bytes/status changed: status=%d err=%v", res.StatusCode, err)
	}
	before = auditFixProjectFileFacts(t, f)
	require(f.editor, http.MethodPost, "/api/v1/projects/mod/"+f.otherModCode+"/files/internal/"+file.ID+"/download", nil, 404)
	if !reflect.DeepEqual(before, auditFixProjectFileFacts(t, f)) {
		t.Fatal("cross-project download changed facts")
	}
	recover(http.MethodDelete, base+"/"+file.ID, nil, 200, activity.ActionDelete, "failed to persist project file activity")
	// Existing thirty-second notification aggregation merges removal into the
	// pending addition event; verify its union/type rather than invent a second
	// event or weaken the original publication/task assertions.
	f.assertFile(t, file.ID, "deleted", 1, 1, 0)
	var mergedKind string
	var sections []string
	if err = f.db.QueryRow(f.ctx, `select update_kind,changed_sections from project_update_events where publication_batch_id=$1`, "project-file:download_added:"+file.ID+":1").Scan(&mergedKind, &sections); err != nil || mergedKind != "content_updated" || !reflect.DeepEqual(sections, []string{"download_files"}) {
		t.Fatalf("download removal aggregation kind=%s sections=%v err=%v", mergedKind, sections, err)
	}
	before = auditFixProjectFileFacts(t, f)
	require(f.editor, http.MethodDelete, base+"/"+file.ID, nil, 404)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectFileFacts(t, f))
}
func TestAuditFixCMP003ExternalProjectDownloadRechecksVisibilityBeforeIntent(t *testing.T) {
	f := newTEST016FileFixture(t)
	hash := strings.Repeat("a", 40)
	var hide atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/project/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned-project", "versions": []string{"v1"}})
			return
		}
		if hide.Load() {
			if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
				t.Error(err)
				http.Error(w, "synthetic update failure", 500)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(test016ProviderVersion("v1", "owned-project", hash))
	}))
	t.Cleanup(provider.Close)
	f.modrinthConfig(t, provider.URL, "owned-project")
	path := "/api/v1/projects/mod/" + f.modCode + "/files/modrinth/" + hash + "/download"
	require := func(want int) []byte {
		return (auditFixGRFixture{test013Fixture: f.test013Fixture}).require(t, f.editor, http.MethodPost, path, nil, want)
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := auditFixProjectFileFacts(t, f)
	raw := require(500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist project download activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectFileFacts(t, f))
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	require(200)
	if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionDownload, activity.ObjectFile); got != 1 {
		t.Fatalf("external release intents=%d", got)
	}
	hide.Store(true)
	require(404)
	if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionDownload, activity.ObjectFile); got != 1 {
		t.Fatalf("hidden parent released another capability/intent: %d", got)
	}
}
