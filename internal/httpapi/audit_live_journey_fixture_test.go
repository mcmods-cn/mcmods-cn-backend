package httpapi

import (
	"archive/zip"
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
	packageDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := exec.Command("git", "-C", packageDirectory, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal("cannot identify actual compiled checkout:", err)
	}
	backendCheckout := strings.TrimSpace(string(checkout))
	frontendCheckout := os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
	if frontendCheckout == "" {
		frontendCheckout = filepath.Join(filepath.Dir(backendCheckout), "mcmods-cn-frontend")
	}
	exporterCheckout := os.Getenv("MCMODS_AUDIT_EXPORTER_REPO")
	if exporterCheckout == "" {
		exporterCheckout = filepath.Join(filepath.Dir(backendCheckout), "mcmods_exporter")
	}
	revisions := map[string]any{}
	for name, path := range map[string]string{"mcmods-cn-backend": backendCheckout, "mcmods-cn-frontend": frontendCheckout, "mcmods_exporter": exporterCheckout} {
		revision := auditJourneyRevision(path)
		if revision["error"] != nil || revision["dirty"] != false {
			t.Fatalf("formal live fixture requires clean identified checkout: %s", name)
		}
		revisions[name] = revision
	}
	if expected := os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT"); expected != "" && revisions["mcmods-cn-backend"].(map[string]any)["commit"] != expected {
		t.Fatal("actual backend checkout HEAD differs from pinned full commit")
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
	sample := loadJourneyGameSample(t, packageDirectory, exporterCheckout)
	frontendOrigin := os.Getenv("MCMODS_AUDIT_FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "http://127.0.0.1:3311"
	}
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	// Serialize only the owned schema installation/drop, not the external
	// browser's observation window. Later cleanup callbacks close the live
	// API/Worker before this callback reacquires the lock for the helper's drop.
	unlockDDL := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlockDDL() })
	f := newAuditFixGRFixture(t, 45*time.Minute)
	unlockDDL()
	t.Cleanup(func() { unlockDDL = auditGlobalCatalogPGLock(t) })
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
	if _, err = db.Exec(f.ctx, `update mod_identifiers set identifier=$2 where mod_id=$1 and identifier='example'`, f.modID, sample.namespace); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(f.ctx, `update mod_content_versions set label=$2,minecraft_versions=ARRAY[$3]::text[],loaders=ARRAY[$4]::text[] where id=$1`, f.versionID, sample.modVersion, sample.minecraft, sample.loader); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.reviewer], "project.review")
	// A distinct ordinary reader is qualified for the real catalogue selector
	// before this fixture exposes its API. These existing read capabilities do
	// not grant publication, private-source access or review bypass.
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.otherEditor], "global_resource.list", "global_resource.view")
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
	seededUnreadA := 0
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
		seededUnreadA = 3
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
	dispatcher := queue.NewOutboxDispatcher(db, q)
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
	auditJourneyWriteJSON(t, authPath, map[string]any{"creatorToken": f.editor, "reviewerToken": f.reviewer, "otherEditorToken": f.otherEditor, "catalogReaderToken": f.otherEditor})
	runtimePath := filepath.Join(directory, "runtime.json")
	stopPath := filepath.Join(directory, "STOP")
	runtime := map[string]any{"schemaVersion": 1, "pid": os.Getpid(), "sourceCheckoutPath": backendCheckout, "compilerSourceCheckoutPath": backendCheckout, "testPackageWorkingDirectory": packageDirectory, "apiOrigin": origin.URL, "ossOrigin": f.provider.URL, "frontendOrigin": frontendOrigin, "authPath": authPath, "stopPath": stopPath, "projectSiteId": "audit-gr-" + f.modCode, "projectPublicId": f.modCode, "targetVersionPublicId": f.versionPublic, "namespace": sample.namespace, "minecraftVersion": sample.minecraft, "loader": sample.loader, "modVersion": sample.modVersion, "postgresPoolMax": 12, "natsURL": broker.ClientURL(), "natsStream": queueConfig.JetStream.Stream, "workerConsumer": "mcmods-export-import-workers", "workerRegistration": "actual NewModExportWorker.Start subscription", "repoRevisions": revisions, "sampleSha256": sample.digest, "sampleProducerCommit": sample.producer, "samplePath": sample.path, "fixtureScope": "real API/PostgreSQL, official file-backed JetStream, actual business Worker, controlled synthetic OSS; external browser must upload unchanged real game package", "acceptance": "EXTERNAL_BROWSER_AND_POSTCONDITIONS_REQUIRED", "catalogReader": map[string]any{"publicId": actorB, "permissions": []string{"global_resource.list", "global_resource.view"}, "assignedBeforeAPIExposure": true, "scope": "Distinct ordinary authorized selector reader; no admin, publication or review-bypass capability"}, "unreadBarrier": map[string]any{"enabled": unreadBarrierEnabled, "armPath": armPath, "startedPath": startedPath, "releasePath": releasePath, "actorAPublicId": actorA, "actorBPublicId": actorB, "initialUnreadA": seededUnreadA, "initialUnreadB": 0}}
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

type journeyGameSample struct {
	path, digest, producer                   string
	namespace, minecraft, loader, modVersion string
}

func loadJourneyGameSample(t *testing.T, packageDirectory, exporter string) journeyGameSample {
	t.Helper()
	const originalHash = "059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb"
	const originalProducer = "806214802c1e3447a9895fa5311e73f3be6edfcc"
	sample := journeyGameSample{
		path:     os.Getenv("MCMODS_AUDIT_GAME_PACKAGE"),
		digest:   os.Getenv("MCMODS_AUDIT_GAME_SHA256"),
		producer: os.Getenv("MCMODS_AUDIT_EXPORTER_COMMIT"),
	}
	if sample.path == "" {
		sample.path = filepath.Join(packageDirectory, "testdata", "audit-20261024", "auditfixture-minecraft1201-fabric-jei1520-v7.zip")
	}
	if sample.digest == "" {
		sample.digest = originalHash
	}
	if sample.digest == originalHash {
		if sample.producer != "" && sample.producer != originalProducer {
			t.Fatal("original game package has a different recorded producer")
		}
		sample.producer = originalProducer
	} else {
		if !auditChallengeHex(sample.digest, 64) || !auditChallengeHex(sample.producer, 40) {
			t.Fatal("provide the recorded SHA256 and full exporter source commit for a new game package")
		}
		if err := exec.Command("git", "-C", exporter, "cat-file", "-e", sample.producer+"^{commit}").Run(); err != nil {
			t.Fatal("recorded game package producer is absent from the exporter checkout")
		}
	}
	if !filepath.IsAbs(sample.path) {
		t.Fatal("game package path must be absolute")
	}
	// The synthetic provider accepts single PUT bodies up to 4 MiB.
	f, err := os.Open(sample.path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		t.Fatal("could not read the game package within the fixture upload limit")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != sample.digest {
		t.Fatal("real-game package hash differs from the recorded producer output")
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files, err := validateExportZIP(archive.File)
	if err != nil {
		t.Fatal(err)
	}
	var manifest modExportManifest
	if err := json.Unmarshal(auditRecipeScopeRead(t, files, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if err := validateModExportManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Configuration.Namespaces) != 1 || !validModIdentifier(manifest.Configuration.Namespaces[0]) || manifest.ErrorCount != 0 {
		t.Fatal("a complete single-namespace game fixture is required")
	}
	sample.namespace, sample.minecraft, sample.loader = manifest.Configuration.Namespaces[0], manifest.MinecraftVersion, normalizeExportLoader(manifest.Loader)
	var mods struct {
		Entries []struct{ ID, Version string }
	}
	if err := json.Unmarshal(auditRecipeScopeRead(t, files, "registries/mods.json"), &mods); err != nil {
		t.Fatal(err)
	}
	for _, mod := range mods.Entries {
		if mod.ID == sample.namespace {
			if sample.modVersion != "" || strings.TrimSpace(mod.Version) == "" {
				t.Fatal("expected exactly one version for the game fixture namespace")
			}
			sample.modVersion = mod.Version
		}
	}
	if sample.modVersion == "" || sample.minecraft != "1.20.1" || (sample.loader != "fabric" && sample.loader != "forge") {
		t.Fatal("missing scoped mod version or unsupported game fixture environment")
	}
	return sample
}

// Exercise the default recorded sample independently of the long-lived fixture.
func TestJourneyGameSampleUsesRecordedSource(t *testing.T) {
	for _, key := range []string{"MCMODS_AUDIT_GAME_PACKAGE", "MCMODS_AUDIT_GAME_SHA256", "MCMODS_AUDIT_EXPORTER_COMMIT"} {
		t.Setenv(key, "")
	}
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sample := loadJourneyGameSample(t, directory, "")
	if sample.namespace != "auditfixture" || sample.minecraft != "1.20.1" || sample.loader != "fabric" || sample.modVersion != "1.0.0" {
		t.Fatalf("sample scope changed: %+v", sample)
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
	diff, err := exec.Command("git", "-C", directory, "diff", "--binary", "HEAD").Output()
	if err != nil {
		return map[string]any{"error": "git source diff unavailable"}
	}
	state, err := exec.Command("git", "-C", directory, "status", "--porcelain").Output()
	if err != nil {
		return map[string]any{"error": "git source status unavailable"}
	}
	digest := sha256.Sum256(diff)
	return map[string]any{"checkoutPath": directory, "commit": strings.TrimSpace(string(head)), "dirty": len(state) > 0, "trackedDiffSha256": hex.EncodeToString(digest[:])}
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
		evidence := map[string]any{"actorPublicId": actorPublicID, "status": recorder.Code, "bodySha256": hex.EncodeToString(decodedHash[:]), "wireBodySha256": hex.EncodeToString(wireHash[:]), "contentEncoding": recorder.Header().Get("Content-Encoding"), "actualCounts": observed.Data, "capturedAt": time.Now().UTC().Format(time.RFC3339Nano), "actualServerHandler": true, "source": "owned PostgreSQL-backed API with normal cache", "released": false}
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
