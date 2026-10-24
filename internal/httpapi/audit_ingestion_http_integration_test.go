package httpapi

// Regression fixture adapted from the portable V2 audit evidence. Uses a fresh
// ephemeral schema and synthetic OSS; security controls stay active. This is
// HTTP + PostgreSQL + directly invoked business Worker, not NATS or browser E2E.
import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type auditFixGRFixture struct {
	test013Fixture
	server        *Server
	provider      *httptest.Server
	objects       map[string]test036Object
	objectMu      *sync.Mutex
	versionID     int64
	versionPublic string
}

func newAuditFixGRFixture(t *testing.T, durations ...time.Duration) auditFixGRFixture {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 for isolated PostgreSQL regression")
	}
	timeout := 150 * time.Second
	if len(durations) > 0 {
		timeout = durations[0]
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	cfg := config.Load()
	fixture, err := database.NewIsolatedTestSchema(ctx, cfg.DB.ConnString(), 1)
	if err != nil {
		t.Fatal(err)
	}
	db := fixture.Pool
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := fixture.Close(c); err != nil {
			t.Error(err)
		}
	})
	// Unique synthetic identity IDs avoid cache collisions with earlier pg_temp fixtures.
	if _, err = db.Exec(ctx, "select setval(pg_get_serial_sequence('users','id'),$1,false)", time.Now().UnixNano()%500000000+1000000000); err != nil {
		t.Fatal(err)
	}
	server := NewServer(ctx, cfg, db, nil, nil, nil, nil)
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := server.Shutdown(c); e != nil {
			t.Error(e)
		}
		server.cache.Close()
	})
	origin := httptest.NewServer(server)
	t.Cleanup(origin.Close)
	f := auditFixGRFixture{test013Fixture: test013Fixture{ctx: ctx, db: db, origin: origin, userIDs: map[string]int64{}}, server: server, objects: map[string]test036Object{}, objectMu: &sync.Mutex{}}
	for i, p := range []*string{&f.editor, &f.otherEditor, &f.denied, &f.reviewer} {
		id, _, token := createTEST044User(t, ctx, db, cfg, fmt.Sprintf("audit-gr-%d-%s", i, strconv.FormatInt(time.Now().UnixNano(), 36)), "UTC")
		*p = token
		f.userIDs[token] = id
	}
	if _, err = db.Exec(ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=any($1::bigint[])`, []int64{f.userIDs[f.editor], f.userIDs[f.otherEditor], f.userIDs[f.denied], f.userIDs[f.reviewer]}); err != nil {
		t.Fatal(err)
	}
	f.modCode = randomCatalogPublicID()
	f.otherModCode = randomCatalogPublicID()
	for i, m := range []struct {
		code, token string
		id          *int64
	}{{f.modCode, f.editor, &f.modID}, {f.otherModCode, f.otherEditor, &f.otherModID}} {
		if err = db.QueryRow(ctx, "insert into mods(project_code,slug,primary_name,review_status,submitted_by) values($1,$2,$3,'approved',$4) returning id", m.code, "audit-gr-"+m.code, fmt.Sprintf("Audit game import %d", i), f.userIDs[m.token]).Scan(m.id); err != nil {
			t.Fatal(err)
		}
		grantTEST044Permissions(t, ctx, db, f.userIDs[m.token], "project.edit."+m.code, "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256")
	}
	if _, err = db.Exec(ctx, "insert into mod_identifiers(mod_id,identifier,is_primary) values($1,'example',true)", f.modID); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, ctx, db, f.userIDs[f.reviewer], "content.review")
	if err = db.QueryRow(ctx, "insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status,created_by,updated_by) values($1,'Audit controlled legacy format',ARRAY['1.20.1'],ARRAY['forge'],'active',$2,$2) returning id,public_id", f.modID, f.userIDs[f.editor]).Scan(&f.versionID, &f.versionPublic); err != nil {
		t.Fatal(err)
	}
	f.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Protocol fixture only; all bodies and credentials are synthetic.
		key := strings.TrimPrefix(r.URL.Path, "/")
		key = strings.TrimPrefix(key, "audit-owned/")
		f.objectMu.Lock()
		defer f.objectMu.Unlock()
		if r.Header.Get("Origin") == "http://localhost:3000" {
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		}
		w.Header().Set("ETag", "\"audit-owned\"")
		switch r.Method {
		case http.MethodPut:
			if src := r.Header.Get("x-oss-copy-source"); src != "" {
				src, _ = url.PathUnescape(src)
				src = strings.TrimPrefix(src, "/audit-owned/")
				obj, ok := f.objects[src]
				if !ok {
					http.Error(w, "missing", 404)
					return
				}
				f.objects[key] = obj
				w.Header().Set("Content-Type", "application/xml")
				io.WriteString(w, "<CopyObjectResult><LastModified>2026-10-01T00:00:00.000Z</LastModified><ETag>audit-owned</ETag></CopyObjectResult>")
				return
			}
			raw, e := io.ReadAll(io.LimitReader(r.Body, 8<<20))
			if e != nil || len(raw) > 4<<20 {
				http.Error(w, "fixture bound", 413)
				return
			}
			f.objects[key] = test036Object{data: raw, hash: r.Header.Get("x-oss-meta-sha256"), contentType: r.Header.Get("Content-Type")}
		case http.MethodGet, http.MethodHead:
			obj, ok := f.objects[key]
			if !ok {
				http.Error(w, "missing", 404)
				return
			}
			w.Header().Set("Content-Type", obj.contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(obj.data)))
			w.Header().Set("x-oss-meta-sha256", obj.hash)
			w.Header().Set("Last-Modified", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
			if r.Method == "GET" {
				w.Write(obj.data)
			}
		case http.MethodDelete:
			delete(f.objects, key)
			w.WriteHeader(204)
		case http.MethodOptions:
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Access-Control-Allow-Methods", "PUT,GET,HEAD,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
			w.WriteHeader(204)
		default:
			http.Error(w, "method", 405)
		}
	}))
	t.Cleanup(f.provider.Close)
	oss := defaultOSSConfig()
	oss.Enabled = true
	oss.UseCName = true
	oss.Bucket = "audit-owned"
	oss.Region = "cn-test"
	oss.Endpoint = f.provider.URL
	oss.PublicEndpoint = f.provider.URL
	oss.AccessKeyID = "audit-gr-synthetic"
	oss.AccessKeySecret = "synthetic-fixture-secret"
	sealed, err := server.sealSystemSetting(oss)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)", sealed); err != nil {
		t.Fatal(err)
	}
	t.Logf("GR_ENV owned_regular_schema=true synthetic_rows=true redis_enabled=%v antiabuse_enabled=%v settings_changed_only_in_fixture=true nats_transport_exercised=false", cfg.Redis.Enabled, cfg.AntiAbuse.Enabled)
	return f
}

func auditFixGRZip(t *testing.T, overrides map[string]string) []byte {
	t.Helper()
	files := map[string]string{
		"manifest.json":                   `{"schema_version":"mcmods-export/v1","exporter_version":"audit-synthetic-legacy-v6","status":"complete","minecraft_version":"1.20.1","loader":"forge","package_format":"zip","configuration":{"namespaces":["example"],"languages":["en-US"],"profile":"all"},"stages":[],"errors":[],"error_count":0}`,
		"compatibility/capabilities.json": `{"schema_version":"mcmods-capabilities/v1","minecraft_version":"1.20.1","loader":"forge","format_contract":"mcmods-export/v1","capabilities":[{"id":"registry","status":"available","source":"runtime"}]}`,
		"recipes/recipes.json":            `{"schema_version":"mcmods-recipes/v2","count":0,"recipes":[]}`,
		"recipes/jei/categories.json":     `{"schema_version":"mcmods-jei-categories/v6","categories":[],"recipes":[]}`,
		"registries/items.json":           `{"registry":"items","entries":[{"id":"example:audit_item","namespace":"example","names":{"en-US":"Audit item"},"max_stack_size":64}]}`,
	}
	for k, v := range overrides {
		if v == "__OMIT__" {
			delete(files, k)
		} else {
			files[k] = v
		}
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, e := z.Create(k)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(files[k])); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func (f auditFixGRFixture) upload(t *testing.T, raw []byte) string {
	t.Helper()
	digest := sha256.Sum256(raw)
	base := "/api/v1/mods/audit-gr-" + f.modCode + "/export-imports/uploads/"
	b := f.require(t, f.editor, "POST", base+"presign", map[string]any{"originalName": "audit-synthetic-v6.zip", "contentType": "application/zip", "source": "mcmods_exporter", "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:])}, 200)
	ticket := decodeTEST022Data[test037Ticket](t, b)
	request, e := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	for k, v := range ticket.Headers {
		request.Header.Set(k, v)
	}
	response, e := f.provider.Client().Do(request)
	if e != nil {
		t.Fatal("synthetic OSS PUT failed")
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("PUT status=%d", response.StatusCode)
	}
	b = f.require(t, f.editor, "POST", base+"complete", ticket.completion(), 201)
	completed := decodeTEST022Data[map[string]any](t, b)
	id, _ := completed["id"].(string)
	if id == "" {
		t.Fatalf("missing file ID; completion keys=%v", auditFixGRKeys(completed))
	}
	t.Log("GR_FACT upload presign=200 PUT=200 complete=201, signed URL/credentials omitted")
	return id
}
func auditFixGRKeys(m map[string]any) []string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func (f auditFixGRFixture) require(t *testing.T, token, method, path string, body any, want int) []byte {
	t.Helper()
	raw, e := json.Marshal(body)
	if e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequestWithContext(f.ctx, method, f.origin.URL+path, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := f.origin.Client().Do(req)
	if e != nil {
		t.Fatal("local HTTP request failed: ", e)
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != want {
		var problem struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(b, &problem)
		t.Fatalf("%s %s status=%d expected=%d error=%s code=%s (response credentials omitted)", method, path, resp.StatusCode, want, problem.Error, problem.Code)
	}
	t.Logf("GR_HTTP %s %s status=%d", method, path, resp.StatusCode)
	return b
}
func (f auditFixGRFixture) job(t *testing.T, file string) modExportJobResponse {
	t.Helper()
	b := f.require(t, f.editor, "POST", "/api/v1/mods/audit-gr-"+f.modCode+"/export-imports", map[string]any{"ossFileId": file, "targetVersionPublicId": f.versionPublic}, 202)
	return decodeTEST022Data[modExportJobResponse](t, b)
}
func (f auditFixGRFixture) imported(t *testing.T, raw []byte) (string, string) {
	t.Helper()
	file := f.upload(t, raw)
	job := f.job(t, file)
	if e := f.server.importMCModsExportJob(f.ctx, job.ID); e != nil {
		t.Fatalf("worker error=%v", e)
	}
	var revision, status, stage string
	if e := f.db.QueryRow(f.ctx, "select r.id,j.status,j.current_stage from catalog_import_revisions r join catalog_import_jobs j on j.id=r.job_id where j.id=$1", job.ID).Scan(&revision, &status, &stage); e != nil {
		var st, detail string
		_ = f.db.QueryRow(f.ctx, "select status,error_detail::text from catalog_import_jobs where id=$1", job.ID).Scan(&st, &detail)
		t.Fatalf("revision error=%v status=%s detail=%s", e, st, detail)
	}
	if status != "ready" && status != "partial" {
		t.Fatalf("unexpected status=%s", status)
	}
	if stage != "review" {
		t.Fatalf("unexpected stage=%s", stage)
	}
	t.Logf("GR_FACT worker=%s stage=%s revision=%s job=%s", status, stage, revision, job.ID)
	return job.ID, revision
}
func (f auditFixGRFixture) reviewGR(t *testing.T, revision, status string) {
	t.Helper()
	f.require(t, f.reviewer, "PATCH", "/api/v1/admin/export-revisions/"+revision+"/activate", map[string]any{"status": status, "note": "Audit synthetic decision"}, 200)
}
func (f auditFixGRFixture) number(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if e := f.db.QueryRow(f.ctx, query, args...).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestAuditFixGameImportCreatorAndPublishedStateHTTPIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+f.modCode)
	raw := auditFixGRZip(t, nil)
	file := f.upload(t, raw)
	job := f.job(t, file)
	base := "/api/v1/mods/audit-gr-" + f.modCode + "/export-imports/" + job.ID
	for _, operation := range []string{"cancel", "retry"} {
		f.require(t, f.otherEditor, "POST", base+"/"+operation, nil, 409)
	}
	var status string
	var creator int64
	if err := f.db.QueryRow(f.ctx, `select status,created_by from catalog_import_jobs where id=$1`, job.ID).Scan(&status, &creator); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || creator != f.userIDs[f.editor] {
		t.Fatalf("rejected operation mutated job: %s creator %d", status, creator)
	}
	if n := f.number(t, `select count(*) from nats_outbox where aggregate_id=$1`, job.ID); n != 1 {
		t.Fatalf("unauthorized side effects: outbox=%d", n)
	}
	f.require(t, f.editor, "POST", base+"/cancel", nil, 200)
	f.require(t, f.editor, "POST", base+"/retry", nil, 202)
	if err := f.server.importMCModsExportJob(f.ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	var revision string
	if err := f.db.QueryRow(f.ctx, `select id from catalog_import_revisions where job_id=$1`, job.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	// This reviewer has content.review only. Reading and deciding use the same
	// existing global/scoped review policy.
	f.require(t, f.reviewer, "GET", "/api/v1/export-revisions/"+revision, nil, 200)
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision, nil, 403)
	f.reviewGR(t, revision, "approved")
	f.require(t, f.reviewer, "PATCH", "/api/v1/admin/export-revisions/"+revision+"/activate", map[string]any{"status": "rejected"}, 409)
	if n := f.number(t, `select count(*) from catalog_import_revisions where id=$1 and is_active and status='ready'`, revision); n != 1 {
		t.Fatal("rejected transition changed active revision")
	}
	if n := f.number(t, `select count(*) from mod_resource_version_details where version_id=$1 and status='active'`, f.versionID); n != 1 {
		t.Fatal("published projection lost")
	}
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision, nil, 200)
}

func TestAuditFixJEIV7ProducerProjectionWorkerIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	overrides := make(map[string]string)
	for path, name := range map[string]string{"recipes/jei/categories.json": "jei-categories-v7.json", "recipes/jei/templates/example/pressing.json": "jei-templates-v2.json", "recipes/jei/recipes/example/pressing.json": "jei-recipes-v2.json"} {
		raw, err := os.ReadFile("testdata/audit-20261024/" + name)
		if err != nil {
			t.Fatal(err)
		}
		overrides[path] = string(raw)
	}
	// The non-game producer contract sample contains one empty input and an
	// output with count two. No version marker is edited.
	_, revision := f.imported(t, auditFixGRZip(t, overrides))
	if n := f.number(t, `select count(*) from recipe_import_binding_candidates candidate join recipe_import_bindings binding on binding.id=candidate.binding_id join recipe_import_snapshots snapshot on snapshot.id=binding.recipe_snapshot_id where snapshot.revision_id=$1 and candidate.amount=2`, revision); n != 1 {
		t.Fatalf("v7 quantity projection=%d", n)
	}
	f.reviewGR(t, revision, "approved")
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets", nil, 200)
}
