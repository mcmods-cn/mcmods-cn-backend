package httpapi

// New final-combination fixture only. The older42b fixture and its evidence are
// retained unchanged. No jobs are inserted and no import/deletion handler is
// invoked directly: normal HTTP, real JetStream subscriptions and real periodic
// workers own every operation after the predeclared synthetic target setup.
import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5/pgxpool"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/progression"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/queue"
)

func TestAuditFinalGameJourneyFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_FINAL_GAME_JOURNEY") != "1" {
		t.Skip("opt-in final fixed-combination real queue/native fixture; PREPARED/SKIP is not business PASS")
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
	checkouts := auditLiveSourceCheckouts(backendCheckout, "")
	revisions := checkouts.pinnedRevisions(t)
	directory := os.Getenv("MCMODS_AUDIT_FINAL_GAME_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("MCMODS_AUDIT_FINAL_GAME_DIR must be an absolute task-owned output directory")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("final journey requires a never-used output directory")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("refuse to replace existing journey ownership marker:", err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-final-game-journey pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	samplePath := os.Getenv("MCMODS_AUDIT_GAME_PACKAGE")
	if samplePath == "" {
		samplePath = filepath.Join(packageDirectory, "testdata", "audit-20261024", "auditfixture-minecraft1201-fabric-jei1520-v7.zip")
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
	if len(sample) != 18290 || expectedSampleHash != "059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb" || sampleProducer != "806214802c1e3447a9895fa5311e73f3be6edfcc" || hex.EncodeToString(sampleDigest[:]) != expectedSampleHash {
		t.Fatal("real-game package hash differs from the reviewed named sample")
	}
	frontendOrigin := os.Getenv("MCMODS_AUDIT_FRONTEND_ORIGIN")
	site, err := url.Parse(frontendOrigin)
	if err != nil || site.Scheme != "https" || site.Hostname() != "127.0.0.1" || site.User != nil || site.Host != "127.0.0.1:"+site.Port() || frontendOrigin != "https://"+site.Host {
		t.Fatal("set the exact HTTPS loopback origin of the deployed frontend")
	}
	port, err := strconv.Atoi(site.Port())
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("the HTTPS loopback deployment requires an explicit valid port")
	}
	trustedBundlePath := os.Getenv("SSL_CERT_FILE")
	trustedBundleSHA := os.Getenv("MCMODS_AUDIT_TLS_CA_BUNDLE_SHA256")
	expectedTLSLeaf := os.Getenv("MCMODS_AUDIT_EXPECT_TLS_LEAF_SHA256")
	trustedBundle, err := os.ReadFile(trustedBundlePath)
	if !filepath.IsAbs(trustedBundlePath) || err != nil || len(trustedBundleSHA) != 64 || normalizeSHA256(trustedBundleSHA) != trustedBundleSHA || len(expectedTLSLeaf) != 64 || normalizeSHA256(expectedTLSLeaf) != expectedTLSLeaf {
		t.Fatal("explicit inherited-plus-owned CA bundle and exact certificate fingerprints required")
	}
	bundleDigest := sha256.Sum256(trustedBundle)
	if hex.EncodeToString(bundleDigest[:]) != trustedBundleSHA {
		t.Fatal("actual inherited-plus-owned CA bundle differs from the declared bytes")
	}
	manifestPath := os.Getenv("MCMODS_AUDIT_FRONTEND_BUILD_MANIFEST")
	manifestSHA := os.Getenv("MCMODS_AUDIT_EXPECT_FRONTEND_BUILD_MANIFEST_SHA256")
	manifestBytes, err := os.ReadFile(manifestPath)
	manifestDigest := sha256.Sum256(manifestBytes)
	var build struct {
		Source, Deploy, SourceCommit, BuildID, EmbeddedAPIOrigin, EmbeddedSiteOrigin string
		Manifest                                                                     []struct {
			Path   string
			Bytes  int64
			SHA256 string
		}
	}
	if !filepath.IsAbs(manifestPath) || err != nil || len(manifestSHA) != 64 || hex.EncodeToString(manifestDigest[:]) != manifestSHA || json.Unmarshal(manifestBytes, &build) != nil || build.Source != checkouts.frontend || build.SourceCommit != revisions["mcmods-cn-frontend"].(map[string]any)["commit"] || strings.TrimSpace(build.BuildID) == "" || (build.EmbeddedAPIOrigin != frontendOrigin && build.EmbeddedAPIOrigin != frontendOrigin+"/service") || build.EmbeddedSiteOrigin != frontendOrigin || !filepath.IsAbs(build.Deploy) {
		t.Fatal("the declared source, site and API must match the pinned deployment manifest")
	}
	buildID, err := os.ReadFile(filepath.Join(build.Deploy, ".next", "BUILD_ID"))
	if err != nil || strings.TrimSpace(string(buildID)) != build.BuildID {
		t.Fatal("actual standalone production BUILD_ID differs from the declared artifact")
	}
	var serverSHA string
	for _, entry := range build.Manifest {
		if entry.Path == "server.js" {
			serverSHA = entry.SHA256
		}
	}
	serverBytes, err := os.ReadFile(filepath.Join(build.Deploy, "server.js"))
	serverDigest := sha256.Sum256(serverBytes)
	if err != nil || serverSHA == "" || hex.EncodeToString(serverDigest[:]) != serverSHA {
		t.Fatal("actual standalone server bytes differ from the declared production manifest")
	}
	productionBuild := map[string]any{"manifestPath": manifestPath, "manifestSHA256": manifestSHA, "sourceCommit": build.SourceCommit, "deploymentPath": build.Deploy, "buildId": build.BuildID, "serverSHA256": serverSHA, "embeddedAPIOrigin": build.EmbeddedAPIOrigin, "embeddedSiteOrigin": build.EmbeddedSiteOrigin}
	tlsState := map[string]any{"trustedCABundlePath": trustedBundlePath, "trustedCABundleSHA256": trustedBundleSHA, "leafCertificateDERSHA256": expectedTLSLeaf, "verified": false, "insecureSkipVerify": false}
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	// Serialize only the owned schema installation/drop, not the external
	// browser's observation window. Later cleanup callbacks close the live
	// API/Worker before this callback reacquires the lock for the helper's drop.
	unlockDDL := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlockDDL() })
	// The real frontend explicitly requests a60-minute upload capability. Keep
	// that product contract and allow its actual wall-clock retirement witness;
	// this is only the new owned fixture lifetime, not a Worker/lease timeout.
	f := newAuditFixGRFixture(t, 90*time.Minute)
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
	dbConfig.ConnConfig.RuntimeParams["application_name"] = "mcmods-audit-final-game-api"
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
	// A distinct ordinary reader is qualified for the real catalogue selector
	// before this fixture exposes its API. These existing read capabilities do
	// not grant publication, private-source access or review bypass.
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.otherEditor], "global_resource.list", "global_resource.view")
	// Only the synthetic OSS service gets this exact browser-origin response
	// wrapper. Production API CORS, upload scanning and visibility are unchanged.
	f.provider.Config.Handler = auditFinalGameOSSProtocol(frontendOrigin, f.objects, f.objectMu, f.provider.Config.Handler)
	// SDK capabilities are signed for the real TLS gateway from the start. It
	// forwards the exact bucket path, original Host and bytes to this owned
	// storage; no API response or signed URL is rewritten.
	storageConfig := f.server.ossConfigFromSettings(f.ctx)
	storageConfig.Endpoint, storageConfig.PublicEndpoint = frontendOrigin, frontendOrigin
	sealedStorage, err := f.server.sealSystemSetting(storageConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealedStorage); err != nil {
		t.Fatal(err)
	}
	broker, err := natsserver.NewServer(&natsserver.Options{ServerName: "mcmods-owned-final-game-journey", Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: filepath.Join(directory, "jetstream-store"), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go broker.Start()
	if !broker.ReadyForConnections(10 * time.Second) {
		broker.Shutdown()
		t.Fatal("owned official JetStream server not ready")
	}
	t.Cleanup(func() { broker.Shutdown(); broker.WaitForShutdown() })
	queueConfig := config.NATSConfig{Enabled: true, URL: broker.ClientURL(), SubjectPrefix: "mcmods-audit-final-game-journey", OutboxEnabled: true, Tasks: f.server.cfg.NATS.Tasks, JetStream: config.JetStreamConfig{Enabled: true, Stream: "MCMODS_AUDIT_FINAL_GAME_JOURNEY", MaxDeliver: 5, AckWait: 30 * time.Second, PublishTimeout: 5 * time.Second}}
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
	var actorA, actorB, reviewerID, schema string
	for token, output := range map[string]*string{f.editor: &actorA, f.otherEditor: &actorB, f.reviewer: &reviewerID} {
		if err = db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[token]).Scan(output); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.QueryRow(f.ctx, `select current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	var rawCapabilitySchemaReady bool
	if err = db.QueryRow(f.ctx, `select
		to_regclass(current_schema()||'.oss_upload_capabilities') is not null
		and exists(select 1 from information_schema.columns where table_schema=current_schema()
		and table_name='oss_upload_capabilities' and column_name='max_expires_at')
		and exists(select 1 from information_schema.columns where table_schema=current_schema()
		and table_name='oss_object_deletion_outbox' and column_name='raw_upload_binding_id')
		and exists(select 1 from information_schema.columns where table_schema=current_schema()
		and table_name='oss_object_deletion_outbox' and column_name='raw_hold_reason')`).Scan(&rawCapabilitySchemaReady); err != nil || !rawCapabilitySchemaReady {
		t.Fatal("final raw-expiry schema contract is not installed; do not start an old-source business journey", err)
	}
	cache := querycache.New(cfg.Redis)
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	activityConfig := f.db.Config()
	activityConfig.MaxConns, activityConfig.MinConns = int32(cfg.Activity.DBMaxConns), int32(cfg.Activity.DBMinConns)
	activityConfig.ConnConfig.RuntimeParams["application_name"] = "mcmods-audit-final-game-activity"
	activityDB, err := pgxpool.NewWithConfig(f.ctx, activityConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(activityDB.Close)
	monitor := activity.NewMonitor(activityDB, progression.NewService(db, cache), activity.Options{
		BatchSize: cfg.Activity.BatchSize, QueueCapacity: cfg.Activity.QueueCapacity,
		FlushInterval: cfg.Activity.FlushInterval, RetryMinDelay: cfg.Activity.RetryMinDelay,
		RetryMaxDelay: cfg.Activity.RetryMaxDelay, WriteTimeout: cfg.Activity.WriteTimeout,
		DurableEnqueueTimeout: cfg.Activity.DurableEnqueueTimeout,
	})
	t.Cleanup(func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := monitor.Close(closeContext); err != nil {
			t.Error(err)
		}
	})
	api := NewServer(journeyCtx, cfg, db, q, cache, monitor, nil)
	records, err := os.OpenFile(filepath.Join(directory, "http-attempts.ndjson"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { records.Close() })
	var recordMu sync.Mutex
	identities := map[string]string{f.editor: actorA, f.otherEditor: actorB, f.reviewer: reviewerID}
	apiHandler := auditFinalGameHTTPRecorder(api, records, &recordMu, identities)
	origin := httptest.NewServer(apiHandler)
	t.Cleanup(func() {
		origin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := api.Shutdown(ctx); err != nil {
			t.Error(err)
		}
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
	NewOSSDeletionWorker(cfg, db).Start(journeyCtx)
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
	if err = os.Mkdir(filepath.Join(directory, ".private"), 0700); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(directory, ".private", "auth.json")
	auditJourneyWriteJSON(t, authPath, map[string]any{"creatorToken": f.editor, "reviewerToken": f.reviewer, "otherEditorToken": f.otherEditor, "catalogReaderToken": f.otherEditor})
	runtimePath := filepath.Join(directory, "runtime.json")
	stopPath := filepath.Join(directory, "STOP")
	tlsReadyPath := filepath.Join(directory, "TLS_READY")
	baseline, err := auditFinalGameFacts(f.ctx, db, f.modID, f.userIDs[f.editor])
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Jobs != 0 || baseline.Revisions != 0 || baseline.Recipes != 0 || baseline.ActiveTasks != 0 {
		t.Fatal("new final fixture must start with no import facts or configured reward tasks")
	}
	auditJourneyWriteJSON(t, filepath.Join(directory, "database-before-game.json"), baseline)
	executable, err := auditFinalGameExecutable()
	if err != nil {
		t.Fatal(err)
	}
	settingsSHA, err := auditFinalGameSettingsSHA(f.ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var databaseOID uint32
	var databaseName, databaseOwner, connectedRole string
	if err = db.QueryRow(f.ctx, `select oid,datname,pg_get_userbyid(datdba),current_user from pg_database where datname=current_database()`).
		Scan(&databaseOID, &databaseName, &databaseOwner, &connectedRole); err != nil {
		t.Fatal("could not identify the actual fixture database", err)
	}
	readyAt := time.Now().UTC()
	serveDeadline, hasDeadline := f.ctx.Deadline()
	if !hasDeadline {
		t.Fatal("final fixture requires its actual bounded context deadline")
	}
	runtime := map[string]any{
		"schemaVersion": 2, "owner": "mcmods-final-game-journey", "pid": os.Getpid(), "ownedSchema": schema,
		"databaseBinding":    map[string]any{"name": databaseName, "oid": databaseOID, "owner": databaseOwner, "role": connectedRole, "host": dbConfig.ConnConfig.Host, "port": dbConfig.ConnConfig.Port},
		"sourceCheckoutPath": backendCheckout, "compilerSourceCheckoutPath": backendCheckout, "testPackageWorkingDirectory": packageDirectory,
		"apiOrigin": origin.URL, "internalOSSOrigin": f.provider.URL, "ossOrigin": frontendOrigin, "frontendOrigin": frontendOrigin, "authPath": authPath, "stopPath": stopPath, "tlsReadyPath": tlsReadyPath,
		"ossBucketPathPrefix": "/" + storageConfig.Bucket + "/", "tls": tlsState, "frontendProductionBuild": productionBuild,
		"projectSiteId": "audit-gr-" + f.modCode, "projectPublicId": f.modCode, "targetVersionPublicId": f.versionPublic,
		"creatorPublicId": actorA, "reviewerPublicId": reviewerID, "namespace": "auditfixture", "minecraftVersion": "1.20.1", "loader": "fabric", "modVersion": "1.0.0",
		"postgresPoolMax": 12, "activityPoolMax": cfg.Activity.DBMaxConns, "activityPoolMin": cfg.Activity.DBMinConns,
		"natsURL": broker.ClientURL(), "natsServerID": broker.ID(), "natsStream": queueConfig.JetStream.Stream, "workerConsumer": "mcmods-export-import-workers",
		"workerRegistration": "actual NewModExportWorker.Start subscription", "repoRevisions": revisions,
		"sampleSha256": expectedSampleHash, "sampleBytes": len(sample), "sampleProducerCommit": sampleProducer, "samplePath": samplePath,
		"createdAt": readyAt, "serveDeadline": serveDeadline.UTC(), "compiledExecutable": executable, "initialSettingsSHA256": settingsSHA,
		"fixtureScope":          "actual HTTP/PostgreSQL and official file-backed JetStream registered business Worker, actual notifications and OSS deletion, actual Monitor+Progression with unchanged default policy; controlled synthetic OSS, external unchanged-game-package browser required",
		"ownedStorageProtocol":  "Only this named18KB single-PUT sample: observed successful SDKV4 signedPUT keys permit exact-prefix MaxUploads32 nontruncated empty multipart listings. Missing objects return standard404/NoSuchKey XML. No multipart creation or real-cloud signature/ACL guarantee is claimed by this controlled storage.",
		"acceptance":            "EXTERNAL_NATIVE_AND_INDEPENDENT_READONLY_TERMINAL_REQUIRED",
		"activityScope":         map[string]any{"actualMonitor": true, "actualProgression": true, "productionOptions": cfg.Activity, "activeRewardTasks": baseline.ActiveTasks, "noSyntheticRewardPolicy": true, "expectedNoTaskRewards": true},
		"catalogReader":         map[string]any{"publicId": actorB, "assignedCatalogReadPermissions": []string{"global_resource.list", "global_resource.view"}, "helperExistingPermissions": []string{"project.edit." + f.otherModCode, "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256"}, "helperOtherProjectPublicId": f.otherModCode, "assignedBeforeAPIExposure": true},
		"sourceLifecycle":       "Current package verifiedSHA stays as evidence; consumed source is tombstoned. Core120s verifies actual sealed/stored deletion and typed raw intent; a separate real maximum-capability-expiry+normal15s-poll phase, predeclared61min ceiling for the real frontend60min grant, must observe raw physical404 before full seal. No product TTL, age or deletion schedule is changed.",
		"rawCapabilityContract": map[string]any{"table": "oss_upload_capabilities", "keyColumns": []string{"bucket", "endpoint", "object_key"}, "expiryColumn": "max_expires_at", "rawIntentBindingColumn": "raw_upload_binding_id", "rawHoldReasonColumn": "raw_hold_reason", "coreSeconds": 120, "rawPhysicalCeilingSeconds": 3660, "frontendDeclaredUploadExpiresMinutes": 60, "sourceSchemaMustBeFrozenBeforeBusiness": true},
	}
	rawMultipartListChecked := false
	updateRuntime := func(state string) {
		runtime["state"] = state
		runtime["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		status := q.Status()
		runtime["queueConnected"] = status.Connected
		runtime["jetStreamReady"] = status.JetStream
		runtime["outboxMetrics"] = dispatcher.Metrics()
		metricsContext, cancelMetrics := context.WithTimeout(f.ctx, 2*time.Second)
		runtime["activityMetrics"] = monitor.Snapshot(metricsContext)
		cancelMetrics()
		if facts, factsErr := auditFinalGameFacts(f.ctx, db, f.modID, f.userIDs[f.editor]); factsErr == nil {
			runtime["terminalFacts"] = facts
			delete(runtime, "terminalFactsError")
			if !rawMultipartListChecked && state == "READY" && facts.ReadyJobs == 1 && facts.SourceBindings == 1 {
				// A real SDK read on the actual observed browser-uploaded key
				// checks the CName root listing route before the natural hour
				// wait. It never claims/deletes a job or changes object bytes.
				readCtx, cancelRead := context.WithTimeout(f.ctx, 10*time.Second)
				var rawKey string
				var live bool
				readErr := db.QueryRow(readCtx, `select binding.upload_object_key,clock_timestamp()<cap.max_expires_at
				 from catalog_import_jobs job join catalog_import_packages package on package.id=job.package_id
				 join oss_upload_byte_bindings binding on binding.file_id=package.archive_file_id and binding.status='committed'
				 join oss_upload_capabilities cap on cap.bucket=binding.bucket and cap.endpoint=binding.endpoint and cap.object_key=binding.upload_object_key
				 where job.mod_id=$1 and job.status='ready'`, f.modID).Scan(&rawKey, &live)
				var actual *aliyunoss.ListMultipartUploadsResult
				if readErr == nil && live {
					var client *aliyunoss.Client
					var activeStorage ossConfigPayload
					client, activeStorage, readErr = api.ossClient(readCtx)
					if readErr == nil && activeStorage.Endpoint == frontendOrigin && activeStorage.PublicEndpoint == frontendOrigin {
						actual, readErr = client.ListMultipartUploads(readCtx, &aliyunoss.ListMultipartUploadsRequest{Bucket: aliyunoss.Ptr(activeStorage.Bucket), Prefix: aliyunoss.Ptr(rawKey), KeyMarker: aliyunoss.Ptr(""), UploadIdMarker: aliyunoss.Ptr(""), MaxUploads: 32})
					}
				}
				cancelRead()
				if readErr != nil || !live || actual == nil || actual.IsTruncated || len(actual.Uploads) != 0 {
					runtime["rawMultipartListPreflight"] = map[string]any{"status": "FAIL", "actualSDK": true, "readOnly": true, "secretOrRawURLLogged": false}
					auditJourneyWriteJSON(t, runtimePath, runtime)
					t.Fatal("actual owned SDK/TLS/exact-key multipart route preflight failed; no cleanup/expiry PASS")
				}
				rawKeyDigest := sha256.Sum256([]byte(rawKey))
				runtime["rawMultipartListPreflight"] = map[string]any{"status": "PASS", "actualSDK": true, "readOnly": true, "rawKeySHA256": hex.EncodeToString(rawKeyDigest[:]), "prefixExact": true, "maxUploads": 32, "encodingType": "url", "emptyMarkers": true, "performedBeforeExpiry": true, "tlsVerifiedNormally": true, "at": time.Now().UTC()}
				rawMultipartListChecked = true
			}
		} else {
			runtime["terminalFactsError"] = "read-only final fact query failed"
		}
		consumers := []map[string]any{}
		if infos := js.ConsumersInfo(queueConfig.JetStream.Stream); infos != nil {
			for info := range infos {
				consumers = append(consumers, map[string]any{"name": info.Name, "delivered": info.Delivered.Consumer, "acknowledged": info.AckFloor.Consumer, "pending": info.NumPending, "ackPending": info.NumAckPending, "redelivered": info.NumRedelivered})
			}
		}
		runtime["allConsumerStates"] = consumers
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
	updateRuntime("BOOTSTRAP_WAIT_TLS_PROXY")
	bootstrapCtx, cancelBootstrap := context.WithTimeout(f.ctx, 5*time.Minute)
	defer cancelBootstrap()
	bootstrapTick := time.NewTicker(150 * time.Millisecond)
	defer bootstrapTick.Stop()
	for {
		if _, err = os.Stat(tlsReadyPath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case <-bootstrapCtx.Done():
			t.Fatal("owned verified TLS gateway bootstrap exceeded its predeclared five-minute budget")
		case <-bootstrapTick.C:
		}
	}
	probe, err := http.NewRequestWithContext(bootstrapCtx, http.MethodGet, build.EmbeddedAPIOrigin+"/api/v1/modpacks/z9z9z9z9z", nil)
	if err != nil {
		t.Fatal(err)
	}
	verifiedClient := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := verifiedClient.Do(probe)
	if err != nil {
		t.Fatal("owned HTTPS gateway normal certificate verification failed")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 || len(response.TLS.PeerCertificates) == 0 {
		t.Fatal("owned HTTPS gateway must return the real missing-object404 with an actually verified certificate chain")
	}
	leafDigest := sha256.Sum256(response.TLS.PeerCertificates[0].Raw)
	if hex.EncodeToString(leafDigest[:]) != expectedTLSLeaf {
		t.Fatal("actual HTTPS leaf differs from the exact predeclared task-owned certificate")
	}
	configProbe, err := http.NewRequestWithContext(bootstrapCtx, http.MethodGet, frontendOrigin+"/authentication-write-config", nil)
	if err != nil {
		t.Fatal(err)
	}
	configResponse, err := verifiedClient.Do(configProbe)
	if err != nil {
		t.Fatal("cannot read public frontend API configuration through the verified deployment")
	}
	var publicConfig struct {
		APIBaseURL string `json:"apiBaseURL"`
	}
	configErr := json.NewDecoder(io.LimitReader(configResponse.Body, 16384)).Decode(&publicConfig)
	_ = configResponse.Body.Close()
	if configResponse.StatusCode != http.StatusOK || configErr != nil || strings.TrimSuffix(publicConfig.APIBaseURL, "/") != build.EmbeddedAPIOrigin {
		t.Fatal("public frontend API configuration differs from its pinned deployment manifest")
	}
	tlsState["verified"] = true
	tlsState["verifiedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	auditJourneyWriteJSON(t, filepath.Join(directory, "pre-business-source-executable-seal.json"), map[string]any{"repoRevisions": revisions, "compiledExecutable": executable, "sampleSha256": expectedSampleHash, "sampleBytes": len(sample), "readyAt": time.Now().UTC(), "settingsSHA256": settingsSHA, "tls": tlsState, "frontendProductionBuild": productionBuild})
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
				seal, readErr := os.ReadFile(filepath.Join(directory, "terminal-acceptance.json"))
				var accepted struct {
					Status        string `json:"status"`
					SampleSHA256  string `json:"sampleSHA256"`
					BackendCommit string `json:"backendCommit"`
				}
				if readErr != nil || json.Unmarshal(seal, &accepted) != nil || accepted.Status != "PASS_FINAL_FIXED_COMBINATION_RAW059B_NATIVE_QUEUE_HTTP_PG" || accepted.SampleSHA256 != expectedSampleHash || accepted.BackendCommit != revisions["mcmods-cn-backend"].(map[string]any)["commit"] {
					t.Fatal("normal owned STOP without matching independent terminal seal remains INCOMPLETE")
				}
				facts, factsErr := auditFinalGameFacts(f.ctx, db, f.modID, f.userIDs[f.editor])
				if factsErr != nil || !facts.complete() {
					t.Fatalf("independent terminal seal did not match actual final business facts: %+v err=%v", facts, factsErr)
				}
				currentSettings, settingsErr := auditFinalGameSettingsSHA(f.ctx, db)
				if settingsErr != nil || currentSettings != settingsSHA {
					t.Fatal("normal policy/settings changed during final journey")
				}
				auditJourneyWriteJSON(t, filepath.Join(directory, "fixture-terminal-facts.json"), facts)
				return
			} else if !os.IsNotExist(e) {
				t.Fatal(e)
			}
			updateRuntime("READY")
		}
	}
}

type auditFinalGameFactSet struct {
	Jobs                     int64 `json:"jobs"`
	ReadyJobs                int64 `json:"readyJobs"`
	Revisions                int64 `json:"revisions"`
	ActiveRevisions          int64 `json:"activeRevisions"`
	Recipes                  int64 `json:"recipes"`
	VerifiedPackages         int64 `json:"verifiedPackages"`
	DeletedSourceFiles       int64 `json:"deletedSourceFiles"`
	SourceBindings           int64 `json:"sourceBindings"`
	SourceCleanupKeys        int64 `json:"sourceCleanupKeys"`
	CompletedSourceDeletions int64 `json:"completedSourceDeletions"`
	ActiveMedia              int64 `json:"activeMedia"`
	PendingActivity          int64 `json:"pendingActivity"`
	CreatorUploadMod         int64 `json:"creatorUploadMod"`
	CreatorCreateMod         int64 `json:"creatorCreateMod"`
	Notifications            int64 `json:"notifications"`
	PublishedNATS            int64 `json:"publishedNATS"`
	UnpublishedNATS          int64 `json:"unpublishedNATS"`
	ActiveTasks              int64 `json:"activeTasks"`
	TaskProgress             int64 `json:"taskProgress"`
	ExperienceRows           int64 `json:"experienceRows"`
	ExperienceTransactions   int64 `json:"experienceTransactions"`
	CurrencyTransactions     int64 `json:"currencyTransactions"`
}

func (facts auditFinalGameFactSet) complete() bool {
	return facts.Jobs == 1 && facts.ReadyJobs == 1 && facts.Revisions == 1 && facts.ActiveRevisions == 1 && facts.Recipes == 2 &&
		facts.VerifiedPackages == 1 && facts.DeletedSourceFiles == 1 && facts.SourceBindings == 1 &&
		facts.SourceCleanupKeys == 2 && facts.CompletedSourceDeletions == facts.SourceCleanupKeys && facts.ActiveMedia == 3 &&
		facts.PendingActivity == 0 && facts.CreatorUploadMod == 3 && facts.CreatorCreateMod == 1 && facts.Notifications == 1 &&
		facts.PublishedNATS == 2 && facts.UnpublishedNATS == 0 && facts.ActiveTasks == 0 && facts.TaskProgress == 0 &&
		facts.ExperienceRows == 0 && facts.ExperienceTransactions == 0 && facts.CurrencyTransactions == 0
}

// One statement observes one PostgreSQL snapshot. The independently executed
// observer also reads the actual rows in a read-only repeatable-read transaction.
// No raw object key, authorization data or document is written to this summary.
func auditFinalGameFacts(ctx context.Context, db *pgxpool.Pool, modID, actorID int64) (auditFinalGameFactSet, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var raw []byte
	err := db.QueryRow(ctx, `with sources as (
 select package.*,file.status file_status,file.source_hash_verified,file.sha256 file_sha,file.stored_sha256,file.size_bytes,
 binding.id binding_id,binding.bucket,binding.endpoint,binding.upload_object_key,binding.sealed_source_key,binding.stored_object_key,
 binding.source_verified_sha256,binding.stored_verified_sha256,binding.source_verified_size_bytes,binding.stored_verified_size_bytes
 from catalog_import_jobs job join catalog_import_packages package on package.id=job.package_id
 join oss_files file on file.id=package.archive_file_id
 left join oss_upload_byte_bindings binding on binding.file_id=file.id and binding.status='committed'
 where job.mod_id=$1
), cleanup_keys as (
 select distinct sources.bucket,sources.endpoint,candidate.object_key from sources
 cross join lateral (values(upload_object_key),(sealed_source_key),(stored_object_key)) candidate(object_key)
 where candidate.object_key<>''
)
 select jsonb_build_object(
 'jobs',(select count(*) from catalog_import_jobs where mod_id=$1),
 'readyJobs',(select count(*) from catalog_import_jobs where mod_id=$1 and status='ready' and progress=100 and attempt_count=1 and run_token=''),
 'revisions',(select count(*) from catalog_import_revisions where mod_id=$1),
 'activeRevisions',(select count(*) from catalog_import_revisions where mod_id=$1 and status='ready' and is_active),
 'recipes',(select count(*) from recipe_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id where revision.mod_id=$1),
 'verifiedPackages',(select count(*) from sources where content_verified_at is not null and source_hash_verified and sha256=$3 and file_sha=$3 and stored_sha256=$3 and source_verified_sha256=$3 and stored_verified_sha256=$3 and size_bytes=18290 and source_verified_size_bytes=18290 and stored_verified_size_bytes=18290),
 'deletedSourceFiles',(select count(*) from sources where file_status='deleted'),
 'sourceBindings',(select count(*) from sources where binding_id is not null),
 'sourceCleanupKeys',(select count(*) from cleanup_keys),
 'completedSourceDeletions',(select count(*) from cleanup_keys key where exists(select 1 from oss_object_deletion_outbox deletion where deletion.bucket=key.bucket and deletion.endpoint=key.endpoint and deletion.object_key=key.object_key and deletion.status='completed' and deletion.deleted_at is not null)),
 'activeMedia',(select count(*) from catalog_import_media media join catalog_import_revisions revision on revision.id=media.revision_id join oss_files file on file.id=media.oss_file_id where revision.mod_id=$1 and file.status='active'),
 'pendingActivity',(select count(*) from activity_event_outbox),
 'creatorUploadMod',(select count(*) from user_activity_events where user_id=$2 and action_id=$4 and object_type_id=$5),
 'creatorCreateMod',(select count(*) from user_activity_events where user_id=$2 and action_id=$6 and object_type_id=$5),
 'notifications',(select count(*) from notifications notification where notification.recipient_id=$2 and notification.template_key='mod_import_success' and notification.source_event_id in(select event_id from nats_outbox where event_type='notification.direct')),
 'publishedNATS',(select count(*) from nats_outbox where status='published'),
 'unpublishedNATS',(select count(*) from nats_outbox where status<>'published'),
 'activeTasks',(select count(*) from task_definitions where status='active'),
 'taskProgress',(select count(*) from user_task_progress),
 'experienceRows',(select count(*) from user_experience),
 'experienceTransactions',(select count(*) from experience_transactions),
 'currencyTransactions',(select count(*) from currency_transactions))`, modID, actorID,
		"059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb", activity.ActionUpload, activity.ObjectMod, activity.ActionCreate).Scan(&raw)
	if err != nil {
		return auditFinalGameFactSet{}, err
	}
	var facts auditFinalGameFactSet
	err = json.Unmarshal(raw, &facts)
	return facts, err
}

func auditFinalGameSettingsSHA(ctx context.Context, db *pgxpool.Pool) (string, error) {
	var raw []byte
	err := db.QueryRow(ctx, `select coalesce(jsonb_agg(jsonb_build_array(key,value) order by key),'[]'::jsonb) from system_settings`).Scan(&raw)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func auditFinalGameExecutable() (map[string]any, error) {
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	digest := sha256.New()
	count, err := io.Copy(digest, file)
	if err != nil {
		return nil, err
	}
	return map[string]any{"pid": os.Getpid(), "bytes": count, "sha256": hex.EncodeToString(digest.Sum(nil)), "capturedBeforeExternalBusiness": true}, nil
}

type auditFinalGameResponse struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (response *auditFinalGameResponse) WriteHeader(status int) {
	if response.status == 0 {
		response.status = status
	}
	response.ResponseWriter.WriteHeader(status)
}

func (response *auditFinalGameResponse) Write(raw []byte) (int, error) {
	if response.status == 0 {
		response.status = http.StatusOK
	}
	if response.body.Len()+len(raw) <= 64<<10 {
		_, _ = response.body.Write(raw)
	}
	return response.ResponseWriter.Write(raw)
}

// This owned recorder forwards the original HTTP request/response. It records
// only mutating API paths and bounded response error metadata, never tokens,
// proof answers, raw JSON bodies, object keys or signed query parameters.
func auditFinalGameHTTPRecorder(next http.Handler, file *os.File, mu *sync.Mutex, identities map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}
		actor := identities[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if actor == "" {
			if cookie, err := r.Cookie("mcmods_session"); err == nil {
				actor = identities[cookie.Value]
			}
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 128<<10))
		if err != nil || len(body) == 128<<10 {
			http.Error(w, "owned final fixture request limit", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		response := &auditFinalGameResponse{ResponseWriter: w}
		next.ServeHTTP(response, r)
		wire := response.body.Bytes()
		if response.Header().Get("Content-Encoding") == "gzip" {
			if reader, err := gzip.NewReader(bytes.NewReader(wire)); err == nil {
				wire, _ = io.ReadAll(io.LimitReader(reader, 64<<10))
				reader.Close()
			}
		}
		var result struct {
			Code  string `json:"code"`
			Error string `json:"error"`
			Data  struct {
				ExpiresAt string `json:"expiresAt"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wire, &result)
		bodySHA := sha256.Sum256(body)
		row := map[string]any{"at": time.Now().UTC(), "actorPublicId": actor, "method": r.Method, "path": r.URL.Path, "status": response.status, "bodySHA256": hex.EncodeToString(bodySHA[:]), "requestId": r.Header.Get("X-Request-ID"), "code": result.Code}
		if result.Data.ExpiresAt != "" {
			row["advertisedExpiresAt"] = result.Data.ExpiresAt
		}
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(file).Encode(row)
	})
}

// Complete only the new named single-PUT fixture's missing OSS protocol.
// Unknown prefixes never receive a generic empty-list success. No database
// connection or business Worker call is made inside the storage service.
func auditFinalGameOSSProtocol(frontend string, objects map[string]test036Object, objectMu *sync.Mutex, next http.Handler) http.Handler {
	var observedMu sync.Mutex
	observedRaw := make(map[string]bool)
	problem := func(w http.ResponseWriter, status int, code string) {
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("x-oss-request-id", "mcmods-owned-final-protocol")
		w.WriteHeader(status)
		fmt.Fprintf(w, "<Error><Code>%s</Code><Message>Owned fixture request boundary</Message></Error>", code)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == frontend {
			w.Header().Set("Access-Control-Allow-Origin", frontend)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Expose-Headers", "ETag,Content-Length,x-oss-meta-sha256")
		}
		query := r.URL.Query()
		if r.Method == http.MethodGet && query.Has("uploads") {
			for name, values := range query {
				if len(values) != 1 || (name != "uploads" && name != "prefix" && name != "max-uploads" && name != "key-marker" && name != "upload-id-marker" && name != "encoding-type") {
					problem(w, http.StatusBadRequest, "InvalidArgument")
					return
				}
			}
			prefix := query.Get("prefix")
			observedMu.Lock()
			known := observedRaw[prefix]
			observedMu.Unlock()
			pathOK := r.URL.Path == "/" || r.URL.Path == "/audit-owned" || r.URL.Path == "/audit-owned/"
			if !pathOK || !known || query.Get("max-uploads") != "32" || query.Get("key-marker") != "" || query.Get("upload-id-marker") != "" || query.Get("encoding-type") != "url" {
				problem(w, http.StatusBadRequest, "InvalidArgument")
				return
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "OSS4-HMAC-SHA256 ") {
				problem(w, http.StatusForbidden, "AccessDenied")
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, "<ListMultipartUploadsResult><Bucket>audit-owned</Bucket><EncodingType>url</EncodingType><MaxUploads>32</MaxUploads><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>")
			return
		}
		key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), "audit-owned/")
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			objectMu.Lock()
			_, exists := objects[key]
			objectMu.Unlock()
			if !exists {
				problem(w, http.StatusNotFound, "NoSuchKey")
				return
			}
		}
		if r.Method == http.MethodPut && query.Get("x-oss-signature-version") == "OSS4-HMAC-SHA256" && query.Get("x-oss-signature") != "" && key != "" && !isOSSSealedObjectKey(key) {
			recorded := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(recorded, r)
			if recorded.status == 0 || recorded.status == http.StatusOK {
				observedMu.Lock()
				observedRaw[key] = true
				observedMu.Unlock()
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Protocol guard only, with no database/import job/Worker/game acceptance. The
// positive key is one real SDK V4 signed PUT, never an overwrite or metadata edit.
func TestAuditFinalGameOwnedStorageProtocolHasExactObservedMultipartAndNoSuchKey(t *testing.T) {
	objects := make(map[string]test036Object)
	var objectMu sync.Mutex
	storage := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), "audit-owned/")
		objectMu.Lock()
		defer objectMu.Unlock()
		switch r.Method {
		case http.MethodPut:
			raw, err := io.ReadAll(io.LimitReader(r.Body, 1024))
			if err != nil || len(raw) >= 1024 {
				t.Error("bounded owned fixture input required")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			objects[key] = test036Object{data: raw}
			// Match the original helper's implicit HTTP200 empty response.
		case http.MethodGet:
			w.Write(objects[key].data)
		case http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	server := httptest.NewServer(auditFinalGameOSSProtocol("http://127.0.0.1:3561", objects, &objectMu, storage))
	defer server.Close()
	cfg := ossConfigPayload{Bucket: "audit-owned", Region: "cn-test", Endpoint: server.URL, UseCName: true, AccessKeyID: "owned-final-key", AccessKeySecret: "synthetic-owned-final-secret"}
	client := newOSSClient(cfg, server.URL, true)
	const key = "mcmods/owned-final/source.zip"
	checkError := func(err error, status int, code string) {
		t.Helper()
		var problem *aliyunoss.ServiceError
		if !errors.As(err, &problem) || problem.StatusCode != status || problem.Code != code {
			t.Fatalf("exact owned OSS protocol status/code missing: got %T", err)
		}
	}
	_, err := client.GetObject(context.Background(), &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key)})
	checkError(err, http.StatusNotFound, "NoSuchKey")
	_, err = client.ListMultipartUploads(context.Background(), &aliyunoss.ListMultipartUploadsRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Prefix: aliyunoss.Ptr(key), MaxUploads: 32})
	checkError(err, http.StatusBadRequest, "InvalidArgument")
	signed, err := client.Presign(context.Background(), &aliyunoss.PutObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key), ForbidOverwrite: aliyunoss.Ptr("true")}, aliyunoss.PresignExpires(time.Minute))
	if err != nil {
		t.Fatal("owned SDK signature generation failed")
	}
	request, err := http.NewRequest(signed.Method, signed.URL, strings.NewReader("one owned source"))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range signed.SignedHeaders {
		request.Header.Set(name, value)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal("owned signed PUT failed")
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("owned signed PUT did not succeed")
	}
	listed, err := client.ListMultipartUploads(context.Background(), &aliyunoss.ListMultipartUploadsRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Prefix: aliyunoss.Ptr(key), MaxUploads: 32})
	if err != nil || listed.IsTruncated || len(listed.Uploads) != 0 {
		t.Fatal("exact observed single-PUT key did not return bounded empty multipart listing")
	}
	_, err = client.ListMultipartUploads(context.Background(), &aliyunoss.ListMultipartUploadsRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Prefix: aliyunoss.Ptr(key + ".unknown"), MaxUploads: 32})
	checkError(err, http.StatusBadRequest, "InvalidArgument")
	_, err = client.ListMultipartUploads(context.Background(), &aliyunoss.ListMultipartUploadsRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Prefix: aliyunoss.Ptr(key), MaxUploads: 33})
	checkError(err, http.StatusBadRequest, "InvalidArgument")
	_, err = client.ListMultipartUploads(context.Background(), &aliyunoss.ListMultipartUploadsRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Prefix: aliyunoss.Ptr(key), MaxUploads: 32, EncodingType: aliyunoss.Ptr("not-url")})
	checkError(err, http.StatusBadRequest, "InvalidArgument")
	if _, err = client.DeleteObject(context.Background(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key)}); err != nil {
		t.Fatal("owned source deletion failed")
	}
	_, err = client.GetObject(context.Background(), &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key)})
	checkError(err, http.StatusNotFound, "NoSuchKey")
}
