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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	// Each fixture owns its database. Give its rate limits and projections the
	// same boundary so another fixture on loopback cannot spend its IP budget.
	cfg.Redis.Namespace += "-audit-import-" + randomCatalogPublicID()
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

// auditFixFlushBarrier selects a reachable schedule at the actual recipe
// transaction boundary. Its observations use the separate fixture pool; it
// never borrows another connection from the pool under test.
type auditFixWorkerContextKey struct{}
type auditFixFlushBarrier struct {
	observer *pgxpool.Pool
	ready    chan string
	release  chan struct{}
	mu       sync.Mutex
	seen     map[string]bool
}

func (b *auditFixFlushBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (b *auditFixFlushBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	job, _ := ctx.Value(auditFixWorkerContextKey{}).(string)
	if job == "" || strings.ToLower(data.CommandTag.String()) != "begin" {
		return
	}
	var progress int
	if b.observer.QueryRow(ctx, `select progress from catalog_import_jobs where id=$1`, job).Scan(&progress) != nil || progress < 36 || progress > 64 {
		return
	}
	b.mu.Lock()
	if b.seen[job] {
		b.mu.Unlock()
		return
	}
	b.seen[job] = true
	b.mu.Unlock()
	b.ready <- job
	select {
	case <-b.release:
	case <-ctx.Done():
	}
}

func TestAuditFixGRR01ConcurrentWorkersProgressAtSupportedPoolSizesIntegration(t *testing.T) {
	for _, size := range []int32{2, 12} {
		t.Run(fmt.Sprintf("pool%d", size), func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			other := f
			other.editor = f.otherEditor
			other.modID = f.otherModID
			other.modCode = f.otherModCode
			if _, err := f.db.Exec(f.ctx, `insert into mod_identifiers(mod_id,identifier,is_primary) values($1,'auditother',true)`, other.modID); err != nil {
				t.Fatal(err)
			}
			if err := f.db.QueryRow(f.ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status,created_by,updated_by) values($1,'Concurrent 1.20.1',array['1.20.1'],array['forge'],'active',$2,$2) returning id,public_id`, other.modID, f.userIDs[other.editor]).Scan(&other.versionID, &other.versionPublic); err != nil {
				t.Fatal(err)
			}
			overrides := map[string]string{}
			for path, name := range map[string]string{"recipes/jei/categories.json": "jei-categories-v7.json", "recipes/jei/templates/example/pressing.json": "jei-templates-v2.json", "recipes/jei/recipes/example/pressing.json": "jei-recipes-v2.json"} {
				raw, err := os.ReadFile("testdata/audit-20261024/" + name)
				if err != nil {
					t.Fatal(err)
				}
				overrides[path] = string(raw)
			}
			firstRaw := auditFixGRZip(t, overrides)
			secondFiles := map[string]string{}
			for name, value := range overrides {
				secondFiles[strings.ReplaceAll(name, "example", "auditother")] = strings.ReplaceAll(value, "example", "auditother")
			}
			secondFiles["manifest.json"] = `{"schema_version":"mcmods-export/v1","exporter_version":"audit-synthetic-current-v7","status":"complete","minecraft_version":"1.20.1","loader":"forge","package_format":"zip","configuration":{"namespaces":["auditother"],"profile":"all"},"stages":[],"errors":[],"error_count":0}`
			secondFiles["registries/items.json"] = `{"registry":"items","entries":[{"id":"auditother:audit_item","namespace":"auditother","names":{"en-US":"Other item"},"max_stack_size":64}]}`
			secondRaw := auditFixGRZip(t, secondFiles)
			firstJob := f.job(t, f.upload(t, firstRaw))
			secondJob := other.job(t, other.upload(t, secondRaw))
			barrier := &auditFixFlushBarrier{observer: f.db, ready: make(chan string, 2), release: make(chan struct{}), seen: map[string]bool{}}
			poolConfig := f.db.Config()
			poolConfig.MaxConns = size
			poolConfig.ConnConfig.Tracer = barrier
			workers, err := pgxpool.NewWithConfig(f.ctx, poolConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer workers.Close()
			// The service graph uses its original pool for auth/cache; only the actual
			// business Worker database pool is varied for this resource-dependency test.
			f.server.db = workers
			ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
			defer cancel()
			results := make(chan error, 2)
			for _, job := range []string{firstJob.ID, secondJob.ID} {
				go func(id string) {
					results <- f.server.importMCModsExportJob(context.WithValue(ctx, auditFixWorkerContextKey{}, id), id)
				}(job)
			}
			for i := 0; i < 2; i++ {
				select {
				case <-barrier.ready:
				case <-ctx.Done():
					close(barrier.release)
					t.Fatal("workers did not reach actual recipe transaction barrier")
				}
			}
			if workers.Stat().AcquiredConns() != 2 {
				close(barrier.release)
				t.Fatalf("barrier not holding both transaction connections: %d", workers.Stat().AcquiredConns())
			}
			close(barrier.release)
			for i := 0; i < 2; i++ {
				if err := <-results; err != nil {
					t.Fatalf("concurrent Worker: %v", err)
				}
			}
			if n := f.number(t, `select count(*) from catalog_import_jobs where id=any($1::text[]) and status='ready' and current_stage='review'`, []string{firstJob.ID, secondJob.ID}); n != 2 {
				t.Fatalf("ready/review jobs=%d", n)
			}
			if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=any($1::text[]) and status='staging'`, []string{firstJob.ID, secondJob.ID}); n != 0 {
				t.Fatalf("staging leak=%d", n)
			}
			t.Logf("actual two Workers completed with pool=%d; actual transaction barrier, no sleep; queue transport not exercised", size)
		})
	}
}

func TestAuditFixExecutionPolicyCancelsInvalidQueuedRequirementsIntegration(t *testing.T) {
	for _, scenario := range []string{"banned", "edit_revoked", "automatic_revoked", "archived", "legacy_unknown"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			if scenario == "automatic_revoked" {
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.no-review."+f.modCode)
			}
			job := f.job(t, f.upload(t, auditFixGRZip(t, nil)))
			switch scenario {
			case "banned":
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "account.banned")
			case "edit_revoked", "automatic_revoked":
				code := "project.edit." + f.modCode
				if scenario == "automatic_revoked" {
					code = "project.no-review." + f.modCode
				}
				if _, err := f.db.Exec(f.ctx, `update user_permissions grant_row set allow=false from permissions permission where grant_row.user_id=$1 and permission.id=grant_row.permission_id and permission.code=$2`, f.userIDs[f.editor], code); err != nil {
					t.Fatal(err)
				}
			case "archived":
				if _, err := f.db.Exec(f.ctx, `update mod_content_versions set status='archived' where id=$1`, f.versionID); err != nil {
					t.Fatal(err)
				}
			case "legacy_unknown":
				if _, err := f.db.Exec(f.ctx, `update catalog_import_jobs set requested_publication_mode='legacy_unknown' where id=$1`, job.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.server.importMCModsExportJob(f.ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			var status, code, lease string
			if err := f.db.QueryRow(f.ctx, `select status,error_code,run_token from catalog_import_jobs where id=$1`, job.ID).Scan(&status, &code, &lease); err != nil {
				t.Fatal(err)
			}
			if status != "cancelled" || !strings.HasSuffix(code, "_reupload_required") || lease != "" {
				t.Fatalf("policy state %s/%s leasePresent=%v", status, code, lease != "")
			}
			if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1`, job.ID); n != 0 {
				t.Fatalf("policy cancellation left revisions=%d", n)
			}
			if n := f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='execution_cancelled'`, job.ID); n != 1 {
				t.Fatalf("cancellation audit=%d", n)
			}
			if n := f.number(t, `select count(*) from nats_outbox where aggregate_id=$1`, job.ID); n != 1 {
				t.Fatalf("cancellation emitted publication/retry outbox=%d", n)
			}
			if retried, err := f.server.resetModExportJobForRetry(f.ctx, job.ID, f.modID, f.userIDs[f.editor]); err != nil || retried {
				t.Fatalf("policy terminal task retried=%v/%v", retried, err)
			}
			// A delayed delivery cannot claim the terminal task or create another audit.
			if err := f.server.importMCModsExportJob(f.ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			if n := f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='execution_cancelled'`, job.ID); n != 1 {
				t.Fatalf("late delivery duplicated audit=%d", n)
			}
		})
	}
}

func TestAuditFixCONT002ManualAndMixedRecipeCatalogHTTPIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	insertEntity := func(identity catalogIdentity, kind string) int64 {
		t.Helper()
		var id int64
		if err := tx.QueryRow(f.ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status) values($1,$2,$3,'active') returning id`, identity.ID, identity.PublicID, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	typeIdentity := recipeTypeIdentity("example:pressing")
	typeID := insertEntity(typeIdentity, "recipe_type")
	resource := resourceIdentity("minecraft.item", "example:manual_resource")
	resourceID := insertEntity(resource, "resource")
	template := catalogIdentity{ID: "audit:manual:template", PublicID: randomCatalogPublicID()}
	templateID := insertEntity(template, "recipe_template")
	recipe := recipeIdentity("", "example:pressing", "example:manual", true)
	recipeID := insertEntity(recipe, "recipe")
	statements := []struct {
		sql  string
		args []any
	}{
		{`insert into recipe_types(entity_id,canonical_id) values($1,'example:pressing')`, []any{typeID}},
		{`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path) values($1,'minecraft.item','example:manual_resource','example','manual_resource')`, []any{resourceID}},
		{`insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,identity_source) values($1,$2,'example:manual','audit-manual-fingerprint','manual')`, []any{recipeID, typeID}},
		{`insert into recipe_layout_templates(entity_id,recipe_type_id,template_key,canvas_width,canvas_height,image_scale) values($1,$2,'audit-manual-layout',100,50,1)`, []any{templateID, typeID}},
		{`insert into recipe_template_slots(identity_key,template_id,slot_key,role,ordinal,x,y,width,height) values('audit:manual:empty',$1,'empty','input',0,1,1,16,16),('audit:manual:output',$1,'output','output',1,30,1,16,16)`, []any{templateID}},
		{`insert into recipe_definitions(recipe_id,template_id,definition) values($1,$2,'{"layout_kind":"not_applicable"}')`, []any{recipeID, templateID}},
		{`insert into recipe_bindings(identity_key,recipe_id,template_slot_id,ordinal) select 'audit:manual:binding',$1,id,1 from recipe_template_slots where template_id=$2 and slot_key='output'`, []any{recipeID, templateID}},
		{`insert into recipe_binding_candidates(identity_key,binding_id,candidate_index,resource_id,amount,probability) select 'audit:manual:candidate',id,0,$1,3,.5 from recipe_bindings where recipe_id=$2`, []any{resourceID, recipeID}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(f.ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	assertCatalog := func(want int) {
		t.Helper()
		raw := f.require(t, "", "GET", "/api/v1/recipe-types/"+typeIdentity.PublicID+"/catalog?limit=1", nil, 200)
		data := decodeTEST022Data[map[string]any](t, raw)
		if data["total"] != float64(want) {
			t.Fatalf("catalog total=%v want=%d", data["total"], want)
		}
		recipes, _ := data["recipes"].([]any)
		if len(recipes) != 1 {
			t.Fatalf("paged recipes=%d", len(recipes))
		}
		if want == 1 {
			value := recipes[0].(map[string]any)
			layout := value["layout"].(map[string]any)
			slots := layout["slots"].([]any)
			if len(slots) != 2 || slots[0].(map[string]any)["ingredient_present"] != false {
				t.Fatalf("empty slot lost: %#v", slots)
			}
			candidates := slots[1].(map[string]any)["alternatives"].([]any)
			candidate := candidates[0].(map[string]any)
			if candidate["amount"] != float64(3) || candidate["probability"] != .5 {
				t.Fatalf("quantity/probability changed: %#v", candidate)
			}
		}
		f.require(t, "", "GET", "/api/v1/catalog/recipes/"+recipe.PublicID+"/render", nil, 200)
	}
	assertCatalog(1)
	overrides := map[string]string{}
	for path, name := range map[string]string{"recipes/jei/categories.json": "jei-categories-v7.json", "recipes/jei/templates/example/pressing.json": "jei-templates-v2.json", "recipes/jei/recipes/example/pressing.json": "jei-recipes-v2.json"} {
		raw, err := os.ReadFile("testdata/audit-20261024/" + name)
		if err != nil {
			t.Fatal(err)
		}
		overrides[path] = string(raw)
	}
	_, revision := f.imported(t, auditFixGRZip(t, overrides))
	f.reviewGR(t, revision, "approved")
	assertCatalog(2)
	raw := f.require(t, "", "GET", "/api/v1/recipe-types/"+typeIdentity.PublicID+"/catalog?limit=1&offset=1", nil, 200)
	data := decodeTEST022Data[map[string]any](t, raw)
	if len(data["recipes"].([]any)) != 1 {
		t.Fatal("mixed second page missing")
	}
	// Remove only this synthetic manual definition to exercise imported-only.
	if _, err := f.db.Exec(f.ctx, `delete from recipe_definitions where recipe_id=$1`, recipeID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update catalog_dataset_state set version=version+1 where singleton`); err != nil {
		t.Fatal(err)
	}
	raw = f.require(t, "", "GET", "/api/v1/recipe-types/"+typeIdentity.PublicID+"/catalog", nil, 200)
	data = decodeTEST022Data[map[string]any](t, raw)
	if data["total"] != float64(1) {
		t.Fatalf("import-only catalog=%#v", data)
	}
}

func TestAuditFixActualGamePackageHTTPWorkerIntegration(t *testing.T) {
	path := os.Getenv("MCMODS_AUDIT_ACTUAL_GAME_ZIP")
	if path == "" {
		path = "testdata/audit-20261024/auditfixture-minecraft1201-fabric-jei1520-v7.zip"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	t.Logf("actual original Minecraft package sha256=%s bytes=%d", hex.EncodeToString(digest[:]), len(raw))
	f := newAuditFixGRFixture(t)
	if _, err := f.db.Exec(f.ctx, `update mod_identifiers set identifier='auditfixture' where mod_id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update mod_content_versions set label='1.0.0',loaders=array['fabric'] where id=$1`, f.versionID); err != nil {
		t.Fatal(err)
	}
	_, revision := f.imported(t, raw)
	f.require(t, f.reviewer, "GET", "/api/v1/export-revisions/"+revision, nil, 200)
	var pngPath string
	if err := f.db.QueryRow(f.ctx, `select asset_path from catalog_import_media where revision_id=$1 and media_kind='png' order by asset_path limit 1`, revision).Scan(&pngPath); err != nil {
		t.Fatal(err)
	}
	client := *f.origin.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	asset := f.origin.URL + "/api/v1/export-revisions/" + revision + "/assets/content?path=" + url.QueryEscape(pngPath)
	checkPolicy := func(token string, status int, policy string) {
		t.Helper()
		req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, asset, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal("could not read local image response")
		}
		res.Body.Close()
		if got := res.Header.Get("Cross-Origin-Resource-Policy"); res.StatusCode != status || got != policy {
			t.Fatalf("image status/policy=%d/%q want %d/%q", res.StatusCode, got, status, policy)
		}
		if status == http.StatusTemporaryRedirect && res.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal("signed redirect must retain its private cache policy")
		}
	}
	checkPolicy("", http.StatusForbidden, "same-site")
	checkPolicy(f.reviewer, http.StatusTemporaryRedirect, "same-site")
	f.reviewGR(t, revision, "approved")
	checkPolicy("", http.StatusTemporaryRedirect, "cross-origin")
	if n := f.number(t, `select count(*) from recipe_import_snapshots where revision_id=$1`, revision); n != 2 {
		t.Fatalf("actual game recipe snapshots=%d", n)
	}
	if n := f.number(t, `select count(*) from catalog_import_media where revision_id=$1`, revision); n < 1 {
		t.Fatalf("actual PNG media=%d", n)
	}
	var typePublicID string
	if err := f.db.QueryRow(f.ctx, `select entity.public_id from recipe_type_import_snapshots snapshot join catalog_entities entity on entity.id=snapshot.recipe_type_id where revision_id=$1 limit 1`, revision).Scan(&typePublicID); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", "GET", "/api/v1/recipe-types/"+typePublicID+"/catalog", nil, 200)
	t.Log("real game bytes -> HTTP upload -> PostgreSQL -> directly invoked business Worker -> independent review -> anonymous catalog: PASS; browser/NATS not exercised by this test")
}
