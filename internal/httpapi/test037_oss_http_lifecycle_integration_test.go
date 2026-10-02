package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This is an owned protocol provider, not evidence of a real cloud deployment.
// Download query signatures are independently recomputed using synthetic keys.
type test037Multipart struct {
	key    string
	object test036Object
	parts  map[int][]byte
}

type test037Store struct {
	mu                  sync.Mutex
	objects             map[string]test036Object
	uploads             map[string]*test037Multipart
	aborts              map[string]int
	sequence            int
	failCopy, failAbort atomic.Bool
}

type test037Fixture struct {
	test013Fixture
	server   *Server
	provider *httptest.Server
	store    *test037Store
	cfg      ossConfigPayload
}

func newTEST037Fixture(t *testing.T) test037Fixture {
	t.Helper()
	f := test037Fixture{test013Fixture: newTEST013Fixture(t), store: &test037Store{
		objects: make(map[string]test036Object), uploads: make(map[string]*test037Multipart), aborts: make(map[string]int),
	}}
	f.server = f.origin.Config.Handler.(*Server)
	f.cfg = defaultOSSConfig()
	f.cfg.Enabled, f.cfg.UseCName = true, true
	f.cfg.Bucket, f.cfg.Region = "test037-owned", "cn-test"
	f.cfg.AccessKeyID, f.cfg.AccessKeySecret = "test037-synthetic-key", "test037-synthetic-secret"
	f.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		key = strings.TrimPrefix(key, "test037-owned/")
		q := r.URL.Query()
		providerError := func(status int, code string) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>owned TEST037 provider result</Message></Error>", code)
		}
		if r.Method == http.MethodGet && r.Header.Get("Authorization") == "" {
			if !validTEST037DownloadSignature(r, key, f.cfg, time.Now()) {
				providerError(403, "SignatureDoesNotMatch")
				return
			}
		}
		if r.Method == http.MethodPut && r.Header.Get("x-oss-copy-source") != "" && f.store.failCopy.Load() ||
			r.Method == http.MethodDelete && q.Get("uploadId") != "" && f.store.failAbort.Load() {
			providerError(503, "ServiceUnavailable")
			return
		}
		f.store.mu.Lock()
		defer f.store.mu.Unlock()
		w.Header().Set("ETag", `"test037-owned"`)
		if r.Method == http.MethodPost && q.Has("uploads") {
			f.store.sequence++
			id := fmt.Sprintf("upload-test037-%08d", f.store.sequence)
			f.store.uploads[id] = &test037Multipart{key: key, object: test036Object{contentType: r.Header.Get("Content-Type"), hash: r.Header.Get("x-oss-meta-sha256")}, parts: make(map[int][]byte)}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprintf(w, "<InitiateMultipartUploadResult><Bucket>test037-owned</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>", key, id)
			return
		}
		if id := q.Get("uploadId"); id != "" {
			upload, found := f.store.uploads[id]
			if !found || upload.key != key {
				providerError(404, "NoSuchUpload")
				return
			}
			switch r.Method {
			case http.MethodDelete:
				delete(f.store.uploads, id)
				f.store.aborts[id]++
				w.WriteHeader(204)
				return
			case http.MethodPut:
				number, err := strconv.Atoi(q.Get("partNumber"))
				raw, readErr := io.ReadAll(io.LimitReader(r.Body, ossMultipartPartSize+1))
				if err != nil || readErr != nil || number < 1 || int64(len(raw)) > ossMultipartPartSize || int64(len(raw)) != r.ContentLength {
					providerError(400, "InvalidPart")
					return
				}
				upload.parts[number] = bytes.Clone(raw)
				return
			case http.MethodPost:
				for number := 1; number <= len(upload.parts); number++ {
					part, ok := upload.parts[number]
					if !ok {
						providerError(400, "InvalidPart")
						return
					}
					upload.object.data = append(upload.object.data, part...)
				}
				if _, exists := f.store.objects[key]; exists {
					providerError(409, "FileAlreadyExists")
					return
				}
				f.store.objects[key] = upload.object
				delete(f.store.uploads, id)
				w.Header().Set("Content-Type", "application/xml")
				_, _ = fmt.Fprintf(w, "<CompleteMultipartUploadResult><Bucket>test037-owned</Bucket><Key>%s</Key><ETag>test037-owned</ETag></CompleteMultipartUploadResult>", key)
				return
			}
		}
		switch r.Method {
		case http.MethodDelete:
			delete(f.store.objects, key)
			w.WriteHeader(204)
		case http.MethodPut:
			if source := r.Header.Get("x-oss-copy-source"); source != "" {
				source, _ = url.PathUnescape(source)
				source = strings.TrimPrefix(source, "/test037-owned/")
				object, ok := f.store.objects[source]
				if !ok {
					providerError(404, "NoSuchKey")
					return
				}
				object.data = bytes.Clone(object.data)
				f.store.objects[key] = object
				w.Header().Set("Content-Type", "application/xml")
				_, _ = io.WriteString(w, "<CopyObjectResult><LastModified>2026-10-01T00:00:00.000Z</LastModified><ETag>test037-owned</ETag></CopyObjectResult>")
				return
			}
			raw, err := io.ReadAll(io.LimitReader(r.Body, maxOSSUploadBytes+1))
			if err != nil || int64(len(raw)) != r.ContentLength || int64(len(raw)) > maxOSSUploadBytes {
				providerError(400, "InvalidArgument")
				return
			}
			if _, exists := f.store.objects[key]; exists {
				providerError(409, "FileAlreadyExists")
				return
			}
			f.store.objects[key] = test036Object{data: bytes.Clone(raw), contentType: r.Header.Get("Content-Type"), hash: r.Header.Get("x-oss-meta-sha256")}
		case http.MethodGet, http.MethodHead:
			object, ok := f.store.objects[key]
			if !ok {
				providerError(404, "NoSuchKey")
				return
			}
			w.Header().Set("Content-Type", object.contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(object.data)))
			w.Header().Set("x-oss-meta-sha256", object.hash)
			w.Header().Set("Last-Modified", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
			if r.Method == http.MethodGet {
				_, _ = w.Write(object.data)
			}
		default:
			providerError(405, "MethodNotAllowed")
		}
	}))
	t.Cleanup(f.provider.Close)
	f.cfg.Endpoint, f.cfg.PublicEndpoint = f.provider.URL, f.provider.URL
	// Legacy ESA input is normalized to object-bound private OSS signatures.
	f.cfg.DownloadURLMode = "esa_private_origin"
	sealed, err := f.server.sealSystemSetting(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)", sealed); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256", "report.create")
	}
	return f
}

func validTEST037DownloadSignature(r *http.Request, key string, cfg ossConfigPayload, now time.Time) bool {
	q := r.URL.Query()
	started, err := time.Parse("20060102T150405Z", q.Get("x-oss-date"))
	expires, expiryErr := strconv.ParseInt(q.Get("x-oss-expires"), 10, 64)
	if err != nil || expiryErr != nil || expires <= 0 || expires > 3600 || now.Before(started) || !now.Before(started.Add(time.Duration(expires)*time.Second)) || q.Get("x-oss-signature-version") != "OSS4-HMAC-SHA256" {
		return false
	}
	scope := started.Format("20060102") + "/" + cfg.Region + "/oss/aliyun_v4_request"
	if q.Get("x-oss-credential") != cfg.AccessKeyID+"/"+scope || q.Get("x-oss-additional-headers") != "" {
		return false
	}
	actual, err := hex.DecodeString(q.Get("x-oss-signature"))
	if err != nil || len(actual) != sha256.Size {
		return false
	}
	q.Del("x-oss-signature")
	escape := func(value string) string { return strings.ReplaceAll(url.QueryEscape(value), "+", "%20") }
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, escape(k))
	}
	sort.Strings(keys)
	var queryParts []string
	for _, escapedKey := range keys {
		k, _ := url.QueryUnescape(escapedKey)
		if len(q[k]) != 1 {
			return false
		}
		queryParts = append(queryParts, escapedKey+"="+escape(q[k][0]))
	}
	uri := strings.ReplaceAll(escape("/"+cfg.Bucket+"/"+key), "%2F", "/")
	canonical := r.Method + "\n" + uri + "\n" + strings.Join(queryParts, "&") + "\n\n\nUNSIGNED-PAYLOAD"
	digest := sha256.Sum256([]byte(canonical))
	message := "OSS4-HMAC-SHA256\n" + q.Get("x-oss-date") + "\n" + scope + "\n" + hex.EncodeToString(digest[:])
	signingKey := []byte("aliyun_v4" + cfg.AccessKeySecret)
	for _, part := range []string{started.Format("20060102"), cfg.Region, "oss", "aliyun_v4_request", message} {
		mac := hmac.New(sha256.New, signingKey)
		_, _ = mac.Write([]byte(part))
		signingKey = mac.Sum(nil)
	}
	return hmac.Equal(actual, signingKey)
}

func (f test037Fixture) facts(t *testing.T) map[string]string {
	t.Helper()
	result := f.test013Fixture.facts(t)
	for _, table := range []string{"oss_files", "oss_object_deletion_outbox", "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations", "oss_multipart_sessions", "oss_rehome_jobs", "report_evidence", "reports", "report_snapshots", "mod_gallery_images", "oss_download_stats", "blueprints", "content_localizations", "content_subjects"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(fact_row) order by to_jsonb(fact_row)::text),'[]'::jsonb)::text from "+pgx.Identifier{table}.Sanitize()+" fact_row").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		result[table] = raw
	}
	return result
}

func (f test037Fixture) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	if !reflect.DeepEqual(before, after) {
		for table, raw := range before {
			if raw != after[table] {
				t.Errorf("denied/failed OSS operation mutated %s", table)
			}
		}
		t.FailNow()
	}
}

type test037Ticket struct {
	Method       string                   `json:"method"`
	URL          string                   `json:"url"`
	Headers      map[string]string        `json:"headers"`
	ObjectKey    string                   `json:"objectKey"`
	Category     string                   `json:"category"`
	Source       string                   `json:"source"`
	OriginalName string                   `json:"originalName"`
	ContentType  string                   `json:"contentType"`
	SizeBytes    int64                    `json:"sizeBytes"`
	SHA256       string                   `json:"sha256"`
	Multipart    ossMultipartUploadTicket `json:"multipart"`
}

func (ticket test037Ticket) completion() map[string]any {
	return map[string]any{"objectKey": ticket.ObjectKey, "originalName": ticket.OriginalName, "contentType": ticket.ContentType, "sizeBytes": ticket.SizeBytes, "sha256": ticket.SHA256, "category": ticket.Category, "source": ticket.Source}
}

func (f test037Fixture) ticket(t *testing.T, token, route, name, contentType, source string, raw []byte) test037Ticket {
	t.Helper()
	digest := sha256.Sum256(raw)
	response := f.require(t, token, http.MethodPost, route, map[string]any{"originalName": name, "contentType": contentType, "source": source, "category": "test037", "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:])}, 200)
	ticket := decodeTEST022Data[test037Ticket](t, response)
	if ticket.ObjectKey == "" || ticket.SizeBytes != int64(len(raw)) || ticket.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("upload ticket lacks actual owned source facts")
	}
	return ticket
}

func (f test037Fixture) put(t *testing.T, ticket test037Ticket, raw []byte) {
	t.Helper()
	put := func(method, location string, headers map[string]string, body []byte) {
		r, err := http.NewRequestWithContext(f.ctx, method, location, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		response, err := f.provider.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			failure, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
			t.Fatalf("owned PUT status=%d body=%s", response.StatusCode, failure)
		}
	}
	if ticket.Method == "MULTIPART" {
		if ticket.Multipart.UploadID == "" || len(ticket.Multipart.Parts) < 2 {
			t.Fatal("actual multipart ticket missing parts")
		}
		offset := int64(0)
		for _, part := range ticket.Multipart.Parts {
			put(part.Method, part.URL, part.Headers, raw[offset:offset+part.SizeBytes])
			offset += part.SizeBytes
		}
		if offset != int64(len(raw)) {
			t.Fatal("multipart lost source bytes")
		}
		return
	}
	put(ticket.Method, ticket.URL, ticket.Headers, raw)
}

func (f test037Fixture) get(t *testing.T, location string, want int) []byte {
	t.Helper()
	r, err := http.NewRequestWithContext(f.ctx, http.MethodGet, location, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.provider.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode != want {
		t.Fatalf("owned GET status=%d want=%d err=%v body=%s", response.StatusCode, want, err, raw)
	}
	return raw
}

func TestTEST037PrivateDownloadFullHTTPRejectsCrossOwnerTamperingAndRealExpiryIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	var tickets []test037Ticket
	for index, token := range []string{f.editor, f.otherEditor} {
		raw := []byte(fmt.Sprintf("TEST037 private file %d\n", index))
		ticket := f.ticket(t, token, "/api/v1/users/me/oss/uploads/presign", fmt.Sprintf("private-%d.zip", index), "application/octet-stream", "test037-private", raw)
		f.put(t, ticket, raw)
		f.require(t, token, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
		tickets = append(tickets, ticket)
	}
	before := f.facts(t)
	f.require(t, "", http.MethodPost, "/api/v1/users/me/files/presign", map[string]any{"objectKey": tickets[0].ObjectKey}, 401)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/users/me/files/presign", map[string]any{"objectKey": tickets[0].ObjectKey}, 400)
	f.require(t, f.otherEditor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": tickets[0].ObjectKey}, 400)
	f.unchanged(t, before)
	type access struct {
		URL  string `json:"url"`
		Mode string `json:"downloadUrlMode"`
	}
	issued := decodeTEST022Data[access](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/files/presign", map[string]any{"objectKey": tickets[0].ObjectKey, "expiresMinutes": 1}, 200))
	if issued.Mode != ossDownloadModePresigned {
		t.Fatalf("legacy ESA still used mode=%s", issued.Mode)
	}
	if got := f.get(t, issued.URL, 200); string(got) != "TEST037 private file 0\n" {
		t.Fatal("authorized download returned another object's bytes")
	}
	parsed, err := url.Parse(issued.URL)
	if err != nil {
		t.Fatal(err)
	}
	started, err := time.Parse("20060102T150405Z", parsed.Query().Get("x-oss-date"))
	if err != nil {
		t.Fatal(err)
	}
	expires, err := strconv.Atoi(parsed.Query().Get("x-oss-expires"))
	if err != nil || expires != 60 {
		t.Fatalf("original one-minute expiry=%d err=%v", expires, err)
	}
	before = f.facts(t)
	for _, mutate := range []func(*url.URL){
		func(u *url.URL) { u.Path = strings.Replace(u.Path, tickets[0].ObjectKey, tickets[1].ObjectKey, 1) },
		func(u *url.URL) {
			q := u.Query()
			q.Set("response-content-disposition", "inline")
			u.RawQuery = q.Encode()
		},
		func(u *url.URL) { q := u.Query(); q.Set("x-oss-expires", "3600"); u.RawQuery = q.Encode() },
		func(u *url.URL) {
			q := u.Query()
			q.Set("x-oss-signature", strings.Repeat("0", 64))
			u.RawQuery = q.Encode()
		},
		func(u *url.URL) { u.RawQuery = "" },
	} {
		changed := *parsed
		mutate(&changed)
		if changed.String() == issued.URL {
			t.Fatal("tamper fixture did not change issued URL")
		}
		f.get(t, changed.String(), 403)
	}
	f.unchanged(t, before)
	// Wait for the actual original sixty-second TTL; no fast-forward substitute.
	timer := time.NewTimer(time.Until(started.Add(60*time.Second + 50*time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-f.ctx.Done():
		t.Fatal("original fixture budget expired before real TTL")
	}
	f.get(t, issued.URL, 403)
	f.unchanged(t, before)
	t.Log("full HTTP private issuance and independent provider verification rejected tampering and actual 60-second expiry; signed URLs remain bearer capabilities")
}

func TestTEST037ReportUploadRegistrationAndBindingFaultsRollbackFullHTTPIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("TEST037 real uploaded report evidence\n")
	ticket := f.ticket(t, f.editor, "/api/v1/reports/evidence/uploads", "test037-evidence.txt", "text/plain", "ignored-by-report-scope", raw)
	f.put(t, ticket, raw)
	before := f.facts(t)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/reports/evidence/uploads/complete", ticket.completion(), 400)
	f.unchanged(t, before)
	if _, err := f.db.Exec(f.ctx, "alter table report_evidence add constraint test037_reject_registration check(original_name<>'test037-evidence.txt') not valid"); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/reports/evidence/uploads/complete", ticket.completion(), 500)
	f.unchanged(t, before)
	if _, err := f.db.Exec(f.ctx, "alter table report_evidence drop constraint test037_reject_registration"); err != nil {
		t.Fatal(err)
	}
	type completion struct {
		ID         string `json:"id"`
		EvidenceID string `json:"evidenceId"`
		Idempotent bool   `json:"idempotent"`
	}
	first := decodeTEST022Data[completion](t, f.require(t, f.editor, http.MethodPost, "/api/v1/reports/evidence/uploads/complete", ticket.completion(), 201))
	if first.ID == "" || first.ID != first.EvidenceID {
		t.Fatal("uploaded evidence has no actual identity")
	}
	before = f.facts(t)
	repeated := decodeTEST022Data[completion](t, f.require(t, f.editor, http.MethodPost, "/api/v1/reports/evidence/uploads/complete", ticket.completion(), 200))
	if repeated.ID != first.ID || !repeated.Idempotent {
		t.Fatal("report completion did not preserve idempotency")
	}
	f.unchanged(t, before)
	var targetPublicID string
	if err := f.db.QueryRow(f.ctx, "select public_id from users where id=$1", f.userIDs[f.denied]).Scan(&targetPublicID); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"targetType": "user", "targetId": targetPublicID, "reasonCode": "harassment", "detail": "test037 evidence lifecycle", "evidenceIds": []string{first.ID}}
	if _, err := f.db.Exec(f.ctx, "alter table report_evidence add constraint test037_reject_binding check(status<>'bound') not valid"); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/reports", report, 500)
	f.unchanged(t, before)
	if _, err := f.db.Exec(f.ctx, "alter table report_evidence drop constraint test037_reject_binding"); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/reports", report, 201)
	var reports, snapshots, bound, files int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from reports),(select count(*) from report_snapshots),
		(select count(*) from report_evidence where public_id=$1 and status='bound' and report_id is not null and cleanup_after='infinity'),
		(select count(*) from oss_files where object_key=$2 and uploader_id=$3 and status='active')`, first.ID, ticket.ObjectKey, f.userIDs[f.editor]).Scan(&reports, &snapshots, &bound, &files); err != nil {
		t.Fatal(err)
	}
	if reports != 1 || snapshots != 1 || bound != 1 || files != 1 {
		t.Fatalf("report actual facts=%d/%d/%d/%d", reports, snapshots, bound, files)
	}
	before = f.facts(t)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/reports", report, 400)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/reports/evidence/"+first.ID+"/access", nil, 404)
	f.unchanged(t, before)
	if _, err := f.db.Exec(f.ctx, "update report_evidence set scan_status='clean' where public_id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/reports/evidence/"+first.ID+"/access", nil, 403)
	f.unchanged(t, before)
	access := decodeTEST022Data[struct {
		URL string `json:"url"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/reports/evidence/"+first.ID+"/access", nil, 200))
	if !bytes.Equal(f.get(t, access.URL, 200), raw) {
		t.Fatal("bound evidence download differs from actual uploaded bytes")
	}
}

func (f test037Fixture) oneMiBQuota(t *testing.T, token string) {
	t.Helper()
	if _, err := f.db.Exec(f.ctx, `delete from user_permissions up using permissions permission where up.permission_id=permission.id and up.user_id=$1
		and (permission.code like 'user.file.single_limit.%' or permission.code like 'user.file.daily_limit.%' or permission.code like 'user.file.total_limit.%')`, f.userIDs[token]); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "user.file.single_limit.1", "user.file.daily_limit.1", "user.file.total_limit.1")
}

type test037HTTPResult struct {
	status int
	raw    []byte
	err    error
}

func (f test037Fixture) concurrent(t *testing.T, token, route string, bodies []map[string]any) []test037HTTPResult {
	t.Helper()
	results := make(chan test037HTTPResult, len(bodies))
	start := make(chan struct{})
	for _, body := range bodies {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			<-start
			status, response, err := test048HTTPRequest(f.ctx, f.origin.Client(), f.origin.URL, token, http.MethodPost, route, string(raw))
			results <- test037HTTPResult{status, response, err}
		}()
	}
	close(start)
	result := make([]test037HTTPResult, 0, len(bodies))
	for range bodies {
		row := <-results
		if row.err != nil {
			t.Fatal(row.err)
		}
		result = append(result, row)
	}
	return result
}

func TestTEST037ConcurrentReservationAndUnreservedSettlementRespectRealHTTPQuotaIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	f.oneMiBQuota(t, f.editor)
	raw := bytes.Repeat([]byte("q"), 600<<10)
	digest := sha256.Sum256(raw)
	var bodies []map[string]any
	for index := range 32 {
		bodies = append(bodies, map[string]any{"originalName": fmt.Sprintf("reservation-%02d.zip", index), "contentType": "application/octet-stream", "category": "test037", "source": "test037-quota", "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:])})
	}
	accepted, denied := 0, 0
	var winning test037Ticket
	for _, row := range f.concurrent(t, f.editor, "/api/v1/users/me/oss/uploads/presign", bodies) {
		switch row.status {
		case 200:
			accepted++
			winning = decodeTEST022Data[test037Ticket](t, row.raw)
		case 403:
			denied++
			if bytes.Contains(row.raw, []byte(`"data":`)) {
				t.Fatal("quota denial has partial success data")
			}
		default:
			t.Fatalf("concurrent reservation status=%d body=%s", row.status, row.raw)
		}
	}
	if accepted != 1 || denied != 31 {
		t.Fatalf("reservation admission=%d/%d want1/31", accepted, denied)
	}
	f.put(t, winning, raw)
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", winning.completion(), 201)
	var active, stored, reservedSource, reservedStored int64
	if err := f.db.QueryRow(f.ctx, "select active_source_bytes,active_stored_bytes,reserved_source_bytes,reserved_stored_bytes from oss_user_quota_usage where user_id=$1", f.userIDs[f.editor]).Scan(&active, &stored, &reservedSource, &reservedStored); err != nil {
		t.Fatal(err)
	}
	if active != int64(len(raw)) || stored != active || reservedSource != 0 || reservedStored != 0 {
		t.Fatalf("settled quota=%d/%d/%d/%d", active, stored, reservedSource, reservedStored)
	}
	var tickets []test037Ticket
	for index := range 2 {
		ticket := f.ticket(t, f.otherEditor, "/api/v1/users/me/oss/uploads/presign", fmt.Sprintf("settlement-%d.zip", index), "application/octet-stream", "test037-quota", raw)
		f.put(t, ticket, raw)
		tickets = append(tickets, ticket)
	}
	// Valid real uploads, then expired reservations and a real permission change:
	// completion cannot rely on old admission or an in-memory caller-side check.
	if _, err := f.db.Exec(f.ctx, "update oss_user_upload_quota_reservations set created_at=now()-interval '2 minutes',expires_at=now()-interval '1 second' where user_id=$1", f.userIDs[f.otherEditor]); err != nil {
		t.Fatal(err)
	}
	f.oneMiBQuota(t, f.otherEditor)
	accepted, denied = 0, 0
	for _, row := range f.concurrent(t, f.otherEditor, "/api/v1/users/me/oss/uploads/complete", []map[string]any{tickets[0].completion(), tickets[1].completion()}) {
		if row.status == 201 {
			accepted++
		} else if row.status == 403 {
			denied++
		} else {
			t.Fatalf("concurrent settlement=%d body=%s", row.status, row.raw)
		}
	}
	if accepted != 1 || denied != 1 {
		t.Fatalf("settlement admission=%d/%d want1/1", accepted, denied)
	}
	var files int
	var daily int64
	if err := f.db.QueryRow(f.ctx, `select usage.active_source_bytes,usage.active_stored_bytes,usage.reserved_source_bytes,usage.reserved_stored_bytes,
		(select count(*) from oss_files where uploader_id=$1 and status='active'),
		(select active_source_bytes from oss_user_daily_quota_usage where user_id=$1 and usage_date=current_date)
		from oss_user_quota_usage usage where user_id=$1`, f.userIDs[f.otherEditor]).Scan(&active, &stored, &reservedSource, &reservedStored, &files, &daily); err != nil {
		t.Fatal(err)
	}
	if files != 1 || active != int64(len(raw)) || stored != active || daily != active || reservedSource != 0 || reservedStored != 0 {
		t.Fatalf("actual quota facts files=%d source/stored/daily=%d/%d/%d reserved=%d/%d", files, active, stored, daily, reservedSource, reservedStored)
	}
}

func TestTEST037MultipartActualPartsCompletionAndAbandonedRestartIntegration(t *testing.T) {
	for _, mode := range []string{"complete", "abandoned"} {
		t.Run(mode, func(t *testing.T) {
			f := newTEST037Fixture(t)
			raw := bytes.Repeat([]byte("m"), int(ossMultipartThreshold)+123)
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "multipart-"+mode+".zip", "application/octet-stream", "test037-multipart", raw)
			f.put(t, ticket, raw)
			body := ticket.completion()
			body["multipartUploadId"] = ticket.Multipart.UploadID
			before := f.facts(t)
			f.require(t, f.otherEditor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", body, 400)
			changed := ticket.completion()
			changed["multipartUploadId"] = "upload-test037-unknown"
			changed["multipartAction"] = "abort"
			f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", changed, 404)
			f.unchanged(t, before)
			if mode == "complete" {
				f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", body, 201)
				before = f.facts(t)
				f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", body, 200)
				f.unchanged(t, before)
				var status string
				var files int
				var reserved int64
				if err := f.db.QueryRow(f.ctx, `select session.status,(select count(*) from oss_files where object_key=$1 and status='active'),
				(select reserved_source_bytes from oss_user_quota_usage where user_id=$2)
				from oss_multipart_sessions session where object_key=$1`, ticket.ObjectKey, f.userIDs[f.editor]).Scan(&status, &files, &reserved); err != nil {
					t.Fatal(err)
				}
				f.store.mu.Lock()
				object, exists := f.store.objects[ticket.ObjectKey]
				_, unfinished := f.store.uploads[ticket.Multipart.UploadID]
				f.store.mu.Unlock()
				if status != "completed" || files != 1 || reserved != 0 || !exists || unfinished || !bytes.Equal(object.data, raw) {
					t.Fatalf("multipart completion facts status=%s files=%d reserved=%d exists=%v unfinished=%v", status, files, reserved, exists, unfinished)
				}
				return
			}
			// The HTTP abort first fails at the actual provider; its durable cleanup
			// session must remain claimable by a freshly constructed production worker.
			body["multipartAction"] = "abort"
			f.store.failAbort.Store(true)
			f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", body, 502)
			var status string
			if err := f.db.QueryRow(f.ctx, "select status from oss_multipart_sessions where object_key=$1", ticket.ObjectKey).Scan(&status); err != nil || status != "cleanup_pending" {
				t.Fatalf("failed actual abort status=%s err=%v", status, err)
			}
			f.store.failAbort.Store(false)
			first := NewOSSMultipartCleanupWorker(f.server.cfg, f.db)
			job, err := first.claim(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, "update oss_multipart_sessions set lease_expires_at=now()-interval '1 second' where id=$1", job.ID); err != nil {
				t.Fatal(err)
			}
			restarted := NewOSSMultipartCleanupWorker(f.server.cfg, f.db)
			current, err := restarted.claim(f.ctx)
			if err != nil || current.ID != job.ID || current.Attempts != 2 || current.LockToken == job.LockToken {
				t.Fatalf("cleanup restart=%+v err=%v", current, err)
			}
			before = f.facts(t)
			if err = first.complete(f.ctx, job); err != errOSSMultipartSessionLeaseLost {
				t.Fatalf("late cleanup completion=%v", err)
			}
			f.unchanged(t, before)
			if err = restarted.abortProviderUpload(f.ctx, current); err != nil {
				t.Fatal(err)
			}
			if err = restarted.complete(f.ctx, current); err != nil {
				t.Fatal(err)
			}
			f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", body, 200)
			before = f.facts(t)
			delete(body, "multipartAction")
			f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", body, 409)
			f.unchanged(t, before)
			var reserved int64
			var files int
			if err = f.db.QueryRow(f.ctx, `select session.status,(select reserved_source_bytes from oss_user_quota_usage where user_id=$2),
			(select count(*) from oss_files where object_key=$1) from oss_multipart_sessions session where object_key=$1`, ticket.ObjectKey, f.userIDs[f.editor]).Scan(&status, &reserved, &files); err != nil {
				t.Fatal(err)
			}
			f.store.mu.Lock()
			_, unfinished := f.store.uploads[ticket.Multipart.UploadID]
			aborts := f.store.aborts[ticket.Multipart.UploadID]
			f.store.mu.Unlock()
			if status != "aborted" || reserved != 0 || files != 0 || unfinished || aborts != 1 {
				t.Fatalf("abandoned cleanup status=%s reserved=%d files=%d unfinished=%v actualAborts=%d", status, reserved, files, unfinished, aborts)
			}
		})
	}
}

func TestTEST037CompletedDeletionKeyReincarnationAndLateWorkerFullHTTPIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("TEST037 first physical incarnation\n")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "reincarnation.zip", "application/octet-stream", "test037-private", raw)
	f.put(t, ticket, raw)
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
	var fileID int64
	if err := f.db.QueryRow(f.ctx, "select id from oss_files where object_key=$1", ticket.ObjectKey).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, "alter table oss_object_deletion_outbox add constraint test037_reject_delete check(reason<>'user-delete') not valid"); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	f.require(t, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 500)
	f.unchanged(t, before)
	if _, err := f.db.Exec(f.ctx, "alter table oss_object_deletion_outbox drop constraint test037_reject_delete"); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	first := NewOSSDeletionWorker(f.server.cfg, f.db)
	old, err := first.claim(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update oss_object_deletion_outbox set locked_at=now()-interval '10 minutes' where id=$1", old.ID); err != nil {
		t.Fatal(err)
	}
	firstRestart := NewOSSDeletionWorker(f.server.cfg, f.db)
	recovered, err := firstRestart.claim(f.ctx)
	if err != nil || recovered.ID != old.ID || recovered.Attempts != 2 || recovered.LockToken == old.LockToken {
		t.Fatalf("first deletion restart=%+v err=%v", recovered, err)
	}
	if err = firstRestart.deleteObject(f.ctx, recovered); err != nil {
		t.Fatal(err)
	}
	if err = firstRestart.complete(f.ctx, recovered); err != nil {
		t.Fatal(err)
	}
	f.store.mu.Lock()
	_, exists := f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if exists {
		t.Fatal("completed deletion did not execute actual provider DELETE")
	}
	secondBytes := []byte("TEST037 second physical incarnation\n")
	newID, err := f.server.writeGeneratedOSSObject(f.ctx, ticket.ObjectKey, "reincarnation.zip", "application/octet-stream", secondBytes, f.userIDs[f.editor], "test037-generated")
	if err != nil || newID != fileID {
		t.Fatalf("trusted regenerated file id=%d want=%d err=%v", newID, fileID, err)
	}
	before = f.facts(t)
	if err = first.deleteObject(f.ctx, old); err != errOSSDeletionLeaseLost {
		t.Fatalf("late physical DELETE from expired first-incarnation lease=%v", err)
	}
	f.unchanged(t, before)
	f.store.mu.Lock()
	object, exists := f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if !exists || !bytes.Equal(object.data, secondBytes) {
		t.Fatal("expired deletion worker damaged active regenerated bytes")
	}
	f.require(t, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	var status string
	var attempts int
	var lineage int64
	if err = f.db.QueryRow(f.ctx, "select status,attempts,oss_file_id from oss_object_deletion_outbox where id=$1", old.ID).Scan(&status, &attempts, &lineage); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 0 || lineage != fileID {
		t.Fatalf("new deletion cycle status=%s attempts=%d lineage=%d", status, attempts, lineage)
	}
	restarted := NewOSSDeletionWorker(f.server.cfg, f.db)
	current, err := restarted.claim(f.ctx)
	if err != nil || current.ID != old.ID || current.LockToken == old.LockToken {
		t.Fatalf("current deletion=%+v err=%v", current, err)
	}
	before = f.facts(t)
	if err = first.complete(f.ctx, old); err != errOSSDeletionLeaseLost {
		t.Fatalf("late previous-incarnation completion=%v", err)
	}
	f.unchanged(t, before)
	if err = restarted.deleteObject(f.ctx, current); err != nil {
		t.Fatal(err)
	}
	if err = restarted.complete(f.ctx, current); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	restarted.drain(f.ctx)
	f.unchanged(t, before)
	f.store.mu.Lock()
	_, exists = f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if exists {
		t.Fatal("second incarnation survived its own completed deletion")
	}
	var active, reserved int64
	if err = f.db.QueryRow(f.ctx, "select active_stored_bytes,reserved_stored_bytes from oss_user_quota_usage where user_id=$1", f.userIDs[f.editor]).Scan(&active, &reserved); err != nil {
		t.Fatal(err)
	}
	if active != 0 || reserved != 0 {
		t.Fatalf("deleted reincarnations retain quota=%d/%d", active, reserved)
	}
}

func TestTEST037RehomeActualCopyQueueFailureAndRestartIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	raw := encoded.Bytes()
	key := ossUserPrefix(f.cfg.Prefix, f.userIDs[f.editor]) + "/test037-rehome.png"
	fileID, err := f.server.writeGeneratedOSSObject(f.ctx, key, "rehome.png", "image/png", raw, f.userIDs[f.editor], "test037-generated")
	if err != nil {
		t.Fatal(err)
	}
	var galleryID string
	if err = f.db.QueryRow(f.ctx, "insert into mod_gallery_images(mod_id,oss_file_id,created_by) values($1,$2,$3) returning public_id", f.modID, fileID, f.userIDs[f.editor]).Scan(&galleryID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = enqueueOSSRehomeJobTx(f.ctx, tx, f.modID); err != nil {
		_ = tx.Rollback(f.ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "alter table oss_object_deletion_outbox add constraint test037_reject_rehome_source check(reason<>'mod-gallery-rehome') not valid"); err != nil {
		t.Fatal(err)
	}
	first := NewOSSRehomeWorker(f.server.cfg, f.db)
	job, err := first.claim(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.processFn(f.ctx, job.ModID); err == nil {
		t.Fatal("actual Copy + destination registration ignored source-deletion queue failure")
	}
	var persisted string
	if err = f.db.QueryRow(f.ctx, "select object_key from oss_files where id=$1", fileID).Scan(&persisted); err != nil || persisted != key {
		t.Fatalf("failed rehome changed published reference=%s err=%v", persisted, err)
	}
	state, err := first.recordFailure(f.ctx, job, fmt.Errorf("test037 source-deletion queue unavailable"))
	if err != nil || state != "queued" {
		t.Fatalf("failed copy recoverability=%s err=%v", state, err)
	}
	if _, err = f.db.Exec(f.ctx, "alter table oss_object_deletion_outbox drop constraint test037_reject_rehome_source"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update oss_rehome_jobs set next_attempt_at=now() where id=$1", job.ID); err != nil {
		t.Fatal(err)
	}
	restarted := NewOSSRehomeWorker(f.server.cfg, f.db)
	current, err := restarted.claim(f.ctx)
	if err != nil || current.ID != job.ID || current.Attempts != 2 || current.LockToken == job.LockToken {
		t.Fatalf("actual copy restart=%+v err=%v", current, err)
	}
	before := f.facts(t)
	if _, err = first.complete(f.ctx, job); err != errOSSRehomeLeaseLost {
		t.Fatalf("late rehome completion=%v", err)
	}
	f.unchanged(t, before)
	if err = restarted.processFn(f.ctx, current.ModID); err != nil {
		t.Fatal(err)
	}
	if state, err = restarted.complete(f.ctx, current); err != nil || state != "completed" {
		t.Fatalf("actual copy completion=%s err=%v", state, err)
	}
	if err = f.db.QueryRow(f.ctx, "select object_key from oss_files where id=$1", fileID).Scan(&persisted); err != nil || persisted == key {
		t.Fatalf("new actual gallery reference=%s err=%v", persisted, err)
	}
	deletion := NewOSSDeletionWorker(f.server.cfg, f.db)
	deletion.drain(f.ctx)
	f.store.mu.Lock()
	oldObject, oldExists := f.store.objects[key]
	newObject, newExists := f.store.objects[persisted]
	f.store.mu.Unlock()
	_ = oldObject
	if oldExists || !newExists || !bytes.Equal(newObject.data, raw) {
		t.Fatalf("actual copy/source cleanup old=%v new=%v", oldExists, newExists)
	}
	before = f.facts(t)
	restarted.drain(f.ctx)
	deletion.drain(f.ctx)
	f.unchanged(t, before)
	var copies, cleanup int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from oss_files where id=$1 and object_key=$2 and status='active'),
		(select count(*) from oss_object_deletion_outbox where object_key=$3 and status='completed')`, fileID, persisted, key).Scan(&copies, &cleanup); err != nil {
		t.Fatal(err)
	}
	if copies != 1 || cleanup != 1 {
		t.Fatalf("actual rehome durable facts=%d/%d", copies, cleanup)
	}
}

func TestTEST037EveryGIFBudgetHasValidBoundaryAndInvalidLaterFrames(t *testing.T) {
	for _, fixture := range []struct {
		name                  string
		width, height, frames int
		delays                []int
		valid                 bool
	}{
		{"frame_limit", 1, 1, 120, nil, true}, {"frame_overflow", 1, 1, 121, nil, false},
		{"duration_limit", 2, 2, 2, []int{1500, 1500}, true}, {"duration_overflow", 2, 2, 2, []int{1500, 1501}, false},
		{"pixel_limit", 1000, 1000, 64, nil, true}, {"pixel_overflow", 1000, 1000, 65, nil, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw := encodeSEC030GIF(t, fixture.width, fixture.height, fixture.frames, fixture.delays)
			_, err := validateRasterImageBytes(raw, "image/gif")
			if (err == nil) != fixture.valid {
				t.Fatalf("full GIF boundary valid=%v err=%v", fixture.valid, err)
			}
		})
	}
	valid := encodeSEC030GIF(t, 2, 2, 2, []int{1, 2})
	for _, raw := range [][]byte{valid[:len(valid)-2], append(bytes.Clone(valid), 0), valid[:12]} {
		if _, err := validateRasterImageBytes(raw, "image/gif"); err == nil {
			t.Fatal("malformed later frame/trailer/header was accepted")
		}
	}
}

func TestTEST037GIFLaterFramesReachActualUploadValidationAndCleanupFullHTTPIntegration(t *testing.T) {
	for _, mode := range []string{"valid", "truncated", "frames", "duration"} {
		t.Run(mode, func(t *testing.T) {
			f := newTEST037Fixture(t)
			_, blueprintID, err := f.server.createPendingBlueprint(f.ctx, f.userIDs[f.editor], "test037-cover.nbt", time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			raw := encodeSEC030GIF(t, 2, 2, 2, []int{1, 2})
			switch mode {
			case "truncated":
				raw = raw[:len(raw)-2]
			case "frames":
				raw = encodeSEC030GIF(t, 1, 1, 121, nil)
			case "duration":
				raw = encodeSEC030GIF(t, 2, 2, 2, []int{1500, 1501})
			}
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "cover-"+mode+".gif", "image/gif", "blueprint_cover:"+blueprintID, raw)
			f.put(t, ticket, raw)
			before := f.facts(t)
			if mode == "valid" {
				f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
				var cover string
				var clean int
				if err = f.db.QueryRow(f.ctx, `select blueprint.cover_object_key,(select count(*) from oss_files where object_key=$2 and scan_status='clean' and status='active')
				from blueprints blueprint where public_id=$1`, blueprintID, ticket.ObjectKey).Scan(&cover, &clean); err != nil {
					t.Fatal(err)
				}
				if cover != ticket.ObjectKey || clean != 1 {
					t.Fatalf("valid GIF cover=%s clean=%d", cover, clean)
				}
				return
			}
			f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 422)
			after := f.facts(t)
			for table, expected := range before {
				if table != "oss_object_deletion_outbox" && after[table] != expected {
					t.Errorf("invalid GIF changed %s", table)
				}
			}
			if t.Failed() {
				t.FailNow()
			}
			var intents int
			if err = f.db.QueryRow(f.ctx, "select count(*) from oss_object_deletion_outbox where object_key=$1 and status='pending'", ticket.ObjectKey).Scan(&intents); err != nil || intents != 1 {
				t.Fatalf("invalid GIF cleanup intents=%d err=%v", intents, err)
			}
			NewOSSDeletionWorker(f.server.cfg, f.db).drain(f.ctx)
			f.store.mu.Lock()
			_, exists := f.store.objects[ticket.ObjectKey]
			f.store.mu.Unlock()
			if exists {
				t.Fatal("invalid later-frame upload bytes survived actual cleanup DELETE")
			}
		})
	}
}
