package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

// Opt-in owned fixture for an external real browser. The browser must create
// upload/import requests through HTTP. This fixture never inserts import jobs
// and never invokes an import Worker handler directly. No product PASS follows
// merely from this fixture's exit status; browser and final postconditions are
// separate evidence.
func TestAuditLiveGameJourneyFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_GAME_JOURNEY") != "1" {
		t.Skip("opt-in long-lived owned JetStream/API/Worker fixture")
	}
	directory := os.Getenv("MCMODS_AUDIT_JOURNEY_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("MCMODS_AUDIT_JOURNEY_DIR must be an absolute task-owned output directory")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("refuse to replace existing journey ownership marker:", err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-journey pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	samplePath := os.Getenv("MCMODS_AUDIT_GAME_PACKAGE")
	if samplePath == "" {
		samplePath = "/workspace/audit-fixes-20261024/artifacts/exporter/game-fabric-final/auditfixture-20261024-101102-941.zip"
	}
	sample, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatal("unchanged real-game package is required:", err)
	}
	expectedSampleHash := os.Getenv("MCMODS_AUDIT_GAME_SHA256")
	if expectedSampleHash == "" {
		expectedSampleHash = "059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb"
	}
	sampleProducer := os.Getenv("MCMODS_AUDIT_EXPORTER_COMMIT")
	if sampleProducer == "" {
		sampleProducer = "806214802c1e3447a9895fa5311e73f3be6edfcc"
	}
	sampleDigest := sha256.Sum256(sample)
	if hex.EncodeToString(sampleDigest[:]) != expectedSampleHash {
		t.Fatal("real-game package hash differs from the reviewed named sample")
	}
	frontendOrigin := os.Getenv("MCMODS_AUDIT_FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "http://127.0.0.1:3311"
	}
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	f := newAuditFixGRFixture(t, 45*time.Minute)
	journeyCtx, cancelJourney := context.WithCancel(f.ctx)
	defer cancelJourney()
	// Reuse the helper's owned, fully migrated schema with the supported default
	// API/Worker pool size. The original single-connection helper pool remains
	// owned by its cleanup and is never repurposed as another database.
	dbConfig := f.db.Config()
	dbConfig.MaxConns = 12
	dbConfig.MinConns = 1
	db, err := pgxpool.NewWithConfig(f.ctx, dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err = db.Exec(f.ctx, `update mod_identifiers set identifier='auditfixture' where mod_id=$1 and identifier='example'`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(f.ctx, `update mod_content_versions set label='1.0.0',minecraft_versions=ARRAY['1.20.1'],loaders=ARRAY['fabric'] where id=$1`, f.versionID); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.reviewer], "project.review")
	// Only the synthetic OSS service gets this exact browser-origin response
	// wrapper. Production API CORS, upload scanning and visibility are unchanged.
	originalOSSHandler := f.provider.Config.Handler
	f.provider.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == frontendOrigin {
			w.Header().Set("Access-Control-Allow-Origin", frontendOrigin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Expose-Headers", "ETag,Content-Length,x-oss-meta-sha256")
		}
		originalOSSHandler.ServeHTTP(w, r)
	})
	broker, err := natsserver.NewServer(&natsserver.Options{ServerName: "mcmods-owned-game-journey", Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: filepath.Join(directory, "jetstream-store"), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go broker.Start()
	if !broker.ReadyForConnections(10 * time.Second) {
		broker.Shutdown()
		t.Fatal("owned official JetStream server not ready")
	}
	t.Cleanup(func() { broker.Shutdown(); broker.WaitForShutdown() })
	queueConfig := config.NATSConfig{Enabled: true, URL: broker.ClientURL(), SubjectPrefix: "mcmods-audit-journey", OutboxEnabled: true, Tasks: f.server.cfg.NATS.Tasks, JetStream: config.JetStreamConfig{Enabled: true, Stream: "MCMODS_AUDIT_GAME_JOURNEY", MaxDeliver: 5, AckWait: 30 * time.Second, PublishTimeout: 5 * time.Second}}
	q := queue.New(journeyCtx, queueConfig)
	t.Cleanup(q.Close)
	q.SetDeadLetterSink(queue.NewPostgresDeadLetterSink(db))
	if status := q.Status(); !status.Connected || !status.JetStream {
		t.Fatal("real JetStream queue failed readiness")
	}
	cfg := f.server.cfg
	cfg.FrontendOrigin = frontendOrigin
	cfg.NATS = queueConfig
	f.origin.Close()
	unreadBarrierEnabled := os.Getenv("MCMODS_AUDIT_UNREAD_BARRIER") == "1"
	armPath := filepath.Join(directory, "unread-arm")
	startedPath := filepath.Join(directory, "unread-started.json")
	releasePath := filepath.Join(directory, "unread-release")
	var actorA, actorB string
	if err = db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&actorA); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.otherEditor]).Scan(&actorB); err != nil {
		t.Fatal(err)
	}
	if unreadBarrierEnabled {
		if _, err = db.Exec(f.ctx, `insert into notifications(recipient_id,kind,title,body) select $1,'system','Synthetic identity boundary','Task-owned unread boundary fixture' from generate_series(1,3)`, f.userIDs[f.editor]); err != nil {
			t.Fatal(err)
		}
	}
	api := NewServer(journeyCtx, cfg, db, q, nil, nil, nil)
	var apiHandler http.Handler = api
	if unreadBarrierEnabled {
		apiHandler = auditJourneyUnreadBarrier(api, f.editor, actorA, armPath, startedPath, releasePath)
	}
	origin := httptest.NewServer(apiHandler)
	t.Cleanup(func() {
		origin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := api.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		api.cache.Close()
	})
	worker := NewModExportWorker(cfg, db, q)
	if err = worker.Start(); err != nil {
		t.Fatal(err)
	}
	if err = NewNotificationWorker(db, q, api.cache, cfg.SMTP, cfg.SettingsEncryptionKey).Start(); err != nil {
		t.Fatal(err)
	}
	if err = NewProjectUpdateNotificationWorker(db, q, api.cache).Start(journeyCtx); err != nil {
		t.Fatal(err)
	}
	NewOSSRehomeWorker(cfg, db).Start(journeyCtx)
	dispatcher := queue.NewOutboxDispatcher(db, q, true)
	dispatcher.Start(journeyCtx)
	observer, err := nats.Connect(broker.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	js, err := observer.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.ConsumerInfo(queueConfig.JetStream.Stream, "mcmods-export-import-workers"); err != nil {
		t.Fatal("actual import consumer is not registered:", err)
	}
	authPath := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authPath, map[string]any{"creatorToken": f.editor, "reviewerToken": f.reviewer, "otherEditorToken": f.otherEditor})
	revisions := map[string]any{}
	for _, name := range []string{"mcmods-cn-backend", "mcmods-cn-frontend", "mcmods_exporter"} {
		revisions[name] = auditJourneyRevision(filepath.Join("/workspace", name))
	}
	runtimePath := filepath.Join(directory, "runtime.json")
	stopPath := filepath.Join(directory, "STOP")
	runtime := map[string]any{"schemaVersion": 1, "pid": os.Getpid(), "apiOrigin": origin.URL, "ossOrigin": f.provider.URL, "frontendOrigin": frontendOrigin, "authPath": authPath, "stopPath": stopPath, "projectSiteId": "audit-gr-" + f.modCode, "projectPublicId": f.modCode, "targetVersionPublicId": f.versionPublic, "namespace": "auditfixture", "minecraftVersion": "1.20.1", "loader": "fabric", "modVersion": "1.0.0", "postgresPoolMax": 12, "natsURL": broker.ClientURL(), "natsStream": queueConfig.JetStream.Stream, "workerConsumer": "mcmods-export-import-workers", "workerRegistration": "actual NewModExportWorker.Start subscription", "repoRevisions": revisions, "sampleSha256": expectedSampleHash, "sampleProducerCommit": sampleProducer, "samplePath": samplePath, "fixtureScope": "real API/PostgreSQL, official file-backed JetStream, actual business Worker, controlled synthetic OSS; external browser must upload unchanged real game package", "acceptance": "EXTERNAL_BROWSER_AND_POSTCONDITIONS_REQUIRED", "unreadBarrier": map[string]any{"enabled": unreadBarrierEnabled, "armPath": armPath, "startedPath": startedPath, "releasePath": releasePath, "actorAPublicId": actorA, "actorBPublicId": actorB, "initialUnreadA": 3, "initialUnreadB": 0}}
	updateRuntime := func(state string) {
		runtime["state"] = state
		runtime["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		status := q.Status()
		runtime["queueConnected"] = status.Connected
		runtime["jetStreamReady"] = status.JetStream
		runtime["outboxMetrics"] = dispatcher.Metrics()
		if info, e := js.ConsumerInfo(queueConfig.JetStream.Stream, "mcmods-export-import-workers"); e == nil {
			runtime["workerConsumerState"] = map[string]any{"delivered": info.Delivered.Consumer, "acknowledged": info.AckFloor.Consumer, "pending": info.NumPending, "ackPending": info.NumAckPending, "redelivered": info.NumRedelivered}
		}
		snapshotCtx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
		defer cancel()
		var jobs, revisionsRaw, outbox []byte
		if e := db.QueryRow(snapshotCtx, `select coalesce((select jsonb_agg(jsonb_build_object('id',id,'status',status,'progress',progress,'stage',current_stage,'errorCode',error_code,'leasePresent',run_token<>'')) from catalog_import_jobs where mod_id=$1),'[]'::jsonb),coalesce((select jsonb_agg(jsonb_build_object('id',id,'jobId',job_id,'status',status,'active',is_active,'namespace',source_namespace)) from catalog_import_revisions where mod_id=$1),'[]'::jsonb),coalesce((select jsonb_object_agg(status,n) from (select status,count(*) n from nats_outbox group by status) grouped),'{}'::jsonb)`, f.modID).Scan(&jobs, &revisionsRaw, &outbox); e == nil {
			runtime["jobs"] = json.RawMessage(jobs)
			runtime["importRevisions"] = json.RawMessage(revisionsRaw)
			runtime["outbox"] = json.RawMessage(outbox)
			runtime["databaseSnapshotPending"] = false
		} else {
			runtime["databaseSnapshotPending"] = true
		}
		f.objectMu.Lock()
		runtime["assetObjectCount"] = len(f.objects)
		var assetBytes int
		for _, object := range f.objects {
			assetBytes += len(object.data)
		}
		runtime["assetBytes"] = assetBytes
		f.objectMu.Unlock()
		auditJourneyWriteJSON(t, runtimePath, runtime)
	}
	updateRuntime("READY")
	t.Logf("owned journey fixture READY runtime=%s; authentication material is in a separate 0600 file and is not logged", runtimePath)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			updateRuntime("EXPIRED")
			t.Fatal("owned journey fixture expired before STOP")
		case <-ticker.C:
			if _, e := os.Stat(stopPath); e == nil {
				updateRuntime("STOPPED")
				return
			} else if !os.IsNotExist(e) {
				t.Fatal(e)
			}
			updateRuntime("READY")
		}
	}
}

func auditJourneyWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	temporary := path + ".tmp"
	if err = os.WriteFile(temporary, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
}

func auditJourneyRevision(directory string) map[string]any {
	head, err := exec.Command("git", "-C", directory, "rev-parse", "HEAD").Output()
	if err != nil {
		return map[string]any{"error": "git revision unavailable"}
	}
	diff, _ := exec.Command("git", "-C", directory, "diff", "--binary", "HEAD").Output()
	state, _ := exec.Command("git", "-C", directory, "status", "--porcelain").Output()
	digest := sha256.Sum256(diff)
	return map[string]any{"commit": strings.TrimSpace(string(head)), "dirty": len(state) > 0, "trackedDiffSha256": hex.EncodeToString(digest[:])}
}

// The delay occurs after the real server has authenticated and read its actual
// database/cache snapshot. Wire headers/status/body are preserved exactly. The
// opt-in barrier only applies after an explicit owned ARM file and never changes
// a production handler or creates a substitute API response.
func auditJourneyUnreadBarrier(next http.Handler, actorToken, actorPublicID, armPath, startedPath, releasePath string) http.Handler {
	var captured atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, authErr := authTokenFromRequest(r)
		_, armErr := os.Stat(armPath)
		if r.Method != "GET" || r.URL.Path != "/api/v1/me/unread-summary" || authErr != nil || token != actorToken || armErr != nil || !captured.CompareAndSwap(false, true) {
			next.ServeHTTP(w, r)
			return
		}
		recorder := httptest.NewRecorder()
		next.ServeHTTP(recorder, r)
		wire := append([]byte(nil), recorder.Body.Bytes()...)
		decoded := wire
		if recorder.Header().Get("Content-Encoding") == "gzip" {
			reader, err := gzip.NewReader(bytes.NewReader(wire))
			if err == nil {
				body, readErr := io.ReadAll(io.LimitReader(reader, 65537))
				reader.Close()
				if readErr == nil && len(body) <= 65536 {
					decoded = body
				}
			}
		}
		wireHash := sha256.Sum256(wire)
		decodedHash := sha256.Sum256(decoded)
		var observed struct {
			Data map[string]int64 `json:"data"`
		}
		_ = json.Unmarshal(decoded, &observed)
		evidence := map[string]any{"actorPublicId": actorPublicID, "status": recorder.Code, "bodySha256": hex.EncodeToString(decodedHash[:]), "wireBodySha256": hex.EncodeToString(wireHash[:]), "contentEncoding": recorder.Header().Get("Content-Encoding"), "actualCounts": observed.Data, "capturedAt": time.Now().UTC().Format(time.RFC3339Nano), "actualHandlerAndDatabase": true, "released": false}
		persist := func() error {
			raw, err := json.MarshalIndent(evidence, "", "  ")
			if err != nil {
				return err
			}
			temporary := startedPath + ".tmp"
			if err = os.WriteFile(temporary, raw, 0600); err != nil {
				return err
			}
			return os.Rename(temporary, startedPath)
		}
		if err := persist(); err != nil {
			http.Error(w, "owned barrier evidence write failed", 503)
			return
		}
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.NewTimer(120 * time.Second)
		defer deadline.Stop()
		waiting := true
		for waiting {
			select {
			case <-r.Context().Done():
				evidence["clientCanceled"] = true
				_ = persist()
				return
			case <-deadline.C:
				evidence["barrierExpired"] = true
				_ = persist()
				http.Error(w, "owned barrier release expired", 503)
				return
			case <-ticker.C:
				if _, err := os.Stat(releasePath); err == nil {
					waiting = false
				}
			}
		}
		evidence["released"] = true
		evidence["releasedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		if err := persist(); err != nil {
			http.Error(w, "owned barrier evidence write failed", 503)
			return
		}
		for name, values := range recorder.Header() {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(wire)
	})
}
