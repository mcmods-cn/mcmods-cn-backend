package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

type auditTP008NegativeFixture struct {
	test013Fixture
	server    *Server
	storage   *auditTP008OwnedOSS
	tokens    map[string]string
	ids       map[string]int64
	publicIDs map[string]string
	author    string
	nonce     string
	directory string
	report    map[string]any
}

// This is a separate short HTTP/PostgreSQL phase. It neither replays the eleven
// native imports nor labels these manual fixtures as Worker/browser acceptance.
func newAuditTP008NegativeFixture(t *testing.T) *auditTP008NegativeFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	t.Cleanup(cancel)
	f := &auditTP008NegativeFixture{nonce: randomCatalogPublicID(), tokens: map[string]string{}, ids: map[string]int64{}, publicIDs: map[string]string{}, directory: os.Getenv("MCMODS_AUDIT_TP008_NEGATIVE_DIR")}
	if !filepath.IsAbs(f.directory) {
		t.Fatal("fresh absolute TP008 negative evidence directory required")
	}
	if err := os.MkdirAll(f.directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(f.directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-tp008-negative pid=%d nonce=%s\n", os.Getpid(), f.nonce)
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	revision := auditJourneyRevision(cwd)
	if revision["dirty"] != false || revision["error"] != nil || os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT") == "" || revision["commit"] != os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT") {
		t.Fatal("exact clean backend pin required")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executed, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, err = io.Copy(digest, executed)
	executed.Close()
	if err != nil {
		t.Fatal(err)
	}
	f.report = map[string]any{"issue": "TP-008", "status": "RUNNING", "startedAt": time.Now().UTC(), "layer": "ACTUAL_HTTP_POSTGRESQL_NORMAL_OSS_PROTOCOL_AND_MANUAL_SCAN_API_NEGATIVE_ONLY", "backend": revision, "cwd": cwd, "argv": os.Args, "runningExecutableSha256": hex.EncodeToString(digest.Sum(nil)), "pid": os.Getpid(), "calls": []map[string]any{}, "cases": []map[string]any{}, "limits": []string{"No browser, metadata Worker, queue delivery, official provider/cloud or antivirus-engine execution in this separate phase.", "Business snapshots hash every complete row of the declared tables; auth/access/security attempt telemetry, Redis rate counters and PostgreSQL nontransactional sequences are not business publication rows.", "Gallery denial concerns foreign uploaders and already-bound other-project files; no blanket rejection of same-owner unbound provisional sharing is asserted."}}
	t.Cleanup(func() {
		f.report["finishedAt"] = time.Now().UTC()
		f.report["status"] = "PASS"
		if t.Failed() {
			f.report["status"] = "FAIL"
		}
		f.write(t)
	})
	db, cleanupDB := newAuditTP008OwnedDatabase(t, ctx)
	t.Cleanup(cleanupDB)
	cfg := config.Load()
	redisHost, _, err := net.SplitHostPort(cfg.Redis.Addr)
	if err != nil || redisHost != "127.0.0.1" || !cfg.Redis.Enabled || !cfg.AntiAbuse.Enabled {
		t.Fatal("unchanged enabled anti-abuse and explicitly enabled owned loopback Redis required")
	}
	cfg.JWTSecret = "tp008-negative-owned-signing-secret-at-least-32-bytes"
	cfg.SettingsEncryptionKey = "tp008-negative-owned-settings-secret-at-least-32-bytes"
	cfg.FrontendOrigin = "http://127.0.0.1:3551"
	cfg.Redis.Namespace += "_tp008_negative_" + f.nonce
	cfg.NATS, cfg.SMTP = config.NATSConfig{}, config.SMTPConfig{}
	var seed [4]byte
	if _, err = rand.Read(seed[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `select setval(pg_get_serial_sequence('users','id'),$1,false)`, int64(binary.BigEndian.Uint32(seed[:]))+3000000000); err != nil {
		t.Fatal(err)
	}
	roles := map[string][]string{
		"owner":    {"project.create.plugin", "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256"},
		"foreign":  {"project.create.plugin"},
		"reviewer": {"project.review"},
		"scan":     {"oss.write"},
		"governor": {"permission.write"},
		"revoked":  {},
	}
	for _, kind := range simpleProjectTypeValues() {
		roles["revoked"] = append(roles["revoked"], "project.create."+kind)
	}
	for _, role := range []string{"owner", "foreign", "reviewer", "scan", "governor", "revoked"} {
		id, public, token := createTEST044User(t, ctx, db, cfg, "tp008-negative-"+f.nonce+"-"+role, "UTC")
		f.ids[role], f.publicIDs[role], f.tokens[role] = id, public, token
		grantTEST044Permissions(t, ctx, db, id, roles[role]...)
		if _, err = db.Exec(ctx, `update users set created_at=clock_timestamp()-interval '30 days',email_verified=true where id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	f.modCode = randomCatalogPublicID()
	if _, err = db.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values($1,$2,'TP008 negative selectable parent','approved',$3)`, f.modCode, "tp008_negative_parent_"+f.nonce, f.ids["owner"]); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status) values('author',$1,lower($1),'approved') returning public_id`, "TP008 negative author "+f.nonce).Scan(&f.author); err != nil {
		t.Fatal(err)
	}
	f.storage = newAuditTP008OwnedOSS(t)
	sealed, err := (&Server{cfg: cfg, db: db}).sealSystemSetting(f.storage.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, sealed); err != nil {
		t.Fatal(err)
	}
	q := queue.New(ctx, cfg.NATS)
	t.Cleanup(q.Close)
	f.server = NewServer(ctx, cfg, db, q, nil, nil, nil)
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := f.server.Shutdown(stopCtx); err != nil {
			t.Error(err)
		}
		if err := f.server.cache.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := f.server.cache.Ping(ctx); err != nil {
		t.Fatal("owned Redis not ready")
	}
	f.origin = httptest.NewServer(f.server)
	t.Cleanup(func() { f.origin.CloseClientConnections(); f.origin.Close() })
	f.test013Fixture = test013Fixture{ctx: ctx, db: db, origin: f.origin, modCode: f.modCode, editor: f.tokens["owner"], reviewer: f.tokens["reviewer"], userIDs: map[string]int64{f.tokens["owner"]: f.ids["owner"], f.tokens["reviewer"]: f.ids["reviewer"]}}
	f.report["roles"], f.report["publicIdentities"], f.report["antiAbuseEnabled"], f.report["redisEnabled"] = roles, f.publicIDs, cfg.AntiAbuse.Enabled, f.server.cache.Enabled()
	f.report["ratePlan"] = "One owner positive create and one owner/one foreign denied create; six submits by the same predeclared revoked actor, original >=61s pause after three. No policy multiplier, actor/IP switch, retry or permission bypass."
	f.report["apiOrigin"], f.report["ownedDatabase"] = f.origin.URL, db.Config().ConnConfig.Database
	f.write(t)
	return f
}

func (f *auditTP008NegativeFixture) write(t *testing.T) {
	t.Helper()
	raw, err := json.MarshalIndent(f.report, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(f.directory, "result.json"), append(raw, '\n'), 0600)
	}
	if err != nil {
		t.Error("cannot persist TP008 negative evidence")
	}
}

func (f *auditTP008NegativeFixture) request(t *testing.T, role, method, path string, payload any, want int) []byte {
	t.Helper()
	status, _, raw, err := auditTP008HTTP(f.ctx, f.origin.URL, f.tokens[role], method, path, payload)
	var failure struct{ Error, Code string }
	_ = json.Unmarshal(raw, &failure)
	f.report["calls"] = append(f.report["calls"].([]map[string]any), map[string]any{"role": role, "method": method, "path": path, "status": status, "expectedStatus": want, "responseSha256": sha256Hex(raw), "error": failure.Error, "code": failure.Code, "at": time.Now().UTC()})
	f.write(t)
	if err != nil || status != want {
		t.Fatalf("%s %s role=%s status=%d want=%d transportError=%v code=%s message=%s", method, path, role, status, want, err, failure.Code, failure.Error)
	}
	return raw
}

func auditTP008NegativeData[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var result struct{ Data T }
	if json.Unmarshal(raw, &result) != nil {
		t.Fatal("normal API returned invalid data envelope")
	}
	return result.Data
}

func (f *auditTP008NegativeFixture) draft(t *testing.T, kind, label string) simpleProjectSnapshot {
	t.Helper()
	draft := test014Snapshot(f.test013Fixture, kind, "tp008_neg_"+f.nonce+"_"+label, "TP008 manual negative "+label, "1.20.1")
	draft.Authors = []modAuthorPayload{{CreatorID: f.author, Kind: "author", Name: "TP008 negative author " + f.nonce}}
	if err := normalizeAndValidateSimpleProject(&draft); err != nil {
		t.Fatalf("declared manual %s request is not valid before authorization: %v", kind, err)
	}
	return draft
}

func (f *auditTP008NegativeFixture) facts(t *testing.T) map[string]any {
	t.Helper()
	tx, err := f.db.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	result := map[string]any{}
	for _, table := range []string{"simple_projects", "simple_project_localizations", "simple_project_links", "simple_project_parent_refs", "simple_project_gallery_images", "content_creator_bindings", "creators", "creator_links", "creator_team_members", "creator_claims", "creator_claim_attachments", "content_revisions", "content_change_items", "change_requests", "review_events", "audit_events", "permission_audit_logs", "public_routes", "nats_outbox", "activity_event_outbox", "project_update_events", "project_update_notification_tasks", "user_activity_events", "notifications", "unresolved_references", "search_index_queue", "oss_files", "oss_upload_byte_bindings", "oss_user_upload_quota_reservations", "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_multipart_sessions", "oss_object_deletion_outbox", "oss_rehome_jobs", "oss_upload_logs", "oss_scan_logs", "oss_download_stats", "report_evidence", "user_permissions", "user_role_bindings", "project_editor_assignments", "user_currency_balances", "currency_transactions", "user_inventory"} {
		var raw string
		var count int64
		if err := tx.QueryRow(f.ctx, `select count(*),coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&count, &raw); err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		result[table] = map[string]any{"rows": count, "completeRowsSha256": sha256Hex([]byte(raw))}
	}
	var raw string
	if err := tx.QueryRow(f.ctx, `select jsonb_agg(jsonb_build_object('id',id,'publicId',public_id,'status',status,'authVersion',auth_version,'permissionVersion',permission_version) order by id)::text from users`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	result["usersSecurityIdentity"] = sha256Hex([]byte(raw))
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	result["ownedAssetBytes"] = f.storage.snapshot()["objects"]
	return result
}

func (f *auditTP008NegativeFixture) deny(t *testing.T, name, role, kind, message string, draft simpleProjectSnapshot, want int) {
	t.Helper()
	before := f.facts(t)
	row := map[string]any{"name": name, "role": role, "projectType": kind, "before": before, "status": "RUNNING"}
	f.report["cases"] = append(f.report["cases"].([]map[string]any), row)
	f.write(t)
	raw := f.request(t, role, http.MethodPost, "/api/v1/content-projects/"+kind, draft, want)
	var failure struct{ Error, Code string }
	if json.Unmarshal(raw, &failure) != nil || failure.Error != message || failure.Code != fmt.Sprintf("HTTP_%d", want) {
		t.Errorf("denial was not the exact expected business boundary: code=%s message=%s", failure.Code, failure.Error)
	}
	after := f.facts(t)
	row["after"], row["actualStatus"], row["exactMessage"], row["completeBusinessRowsAndAssetsUnchanged"] = after, want, failure.Error, reflect.DeepEqual(before, after)
	if !reflect.DeepEqual(before, after) {
		for key, value := range before {
			if !reflect.DeepEqual(value, after[key]) {
				t.Errorf("denied request changed business fact %s", key)
			}
		}
	}
	row["status"] = "PASS"
	if t.Failed() {
		row["status"] = "FAIL"
	}
	f.write(t)
}

func auditTP008NegativePNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			picture.SetNRGBA(x, y, color.NRGBA{R: uint8(7*x + 3), G: uint8(5*y + 9), B: 131, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
